package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner stands in for the Puppeteer worker so lifecycle tests never spawn
// node, Chromium, or network traffic.
type fakeRunner struct {
	blocking bool
	outcome  jobOutcome
	err      error
}

func (f *fakeRunner) run(ctx context.Context, job *Job) (jobOutcome, error) {
	if !f.blocking {
		return f.outcome, f.err
	}
	<-ctx.Done()
	return jobOutcome{}, ctx.Err()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func doJSON(t *testing.T, s *dashboardState, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("X-Echoheist-Token", s.token)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func jsonInto(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

// TestEveryAPIRouteRequiresToken walks the whole contract surface: nothing under
// /api is reachable without the run token, and no CORS grant is ever emitted.
func TestEveryAPIRouteRequiresToken(t *testing.T) {
	s := newTestDashboard()
	h := s.routes()

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/state"},
		{http.MethodGet, "/api/jobs"},
		{http.MethodPost, "/api/jobs"},
		{http.MethodDelete, "/api/jobs/job-1"},
		{http.MethodPost, "/api/jobs/job-1/retry"},
		{http.MethodPost, "/api/jobs/clear"},
		{http.MethodGet, "/api/history"},
		{http.MethodGet, "/api/favorites"},
		{http.MethodPost, "/api/favorites"},
		{http.MethodDelete, "/api/favorites/1"},
		{http.MethodGet, "/api/sources"},
		{http.MethodGet, "/api/logs"},
		{http.MethodGet, "/api/logs/job-1.log"},
		{http.MethodGet, "/api/artists"},
		{http.MethodGet, "/api/artists/1"},
		{http.MethodGet, "/api/search?q=a"},
		{http.MethodPost, "/api/fetch/artists"},
		{http.MethodPost, "/api/fetch/tabs"},
		{http.MethodGet, "/api/config"},
		{http.MethodPut, "/api/config"},
		{http.MethodGet, "/api/health"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		req.Header.Set("Origin", "https://evil.test")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without token = %d, want 401", tc.method, tc.path, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("%s %s leaked Access-Control-Allow-Origin %q", tc.method, tc.path, got)
		}
	}

	// The query-string form of the token is accepted too.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health?token=test-token", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/health?token= = %d, want 200", rec.Code)
	}
}

// TestJobLifecycleQueueCancelRetry covers queue → run → cancel → retry, including
// cancelling a job that has not started yet.
func TestJobLifecycleQueueCancelRetry(t *testing.T) {
	s := newTestDashboard()
	runner := &fakeRunner{blocking: true}
	s.runner = runner.run
	s.startDispatcher()

	tab := Tab{ID: 501, Song: "Song", Artist: "Artist", URL: "https://tabs.ultimate-guitar.com/tab/x/song-501"}
	opts := defaultOptions()

	first := s.newJob(tab, opts)
	s.enqueue(first)
	queued := s.newJob(tab, opts)
	s.enqueue(queued)

	waitFor(t, "first job running", func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.jobs[first.ID].Status == StatusRunning
	})

	s.mu.RLock()
	queuedStatus := s.jobs[queued.ID].Status
	s.mu.RUnlock()
	if queuedStatus != StatusQueued {
		t.Fatalf("second job status = %q, want queued", queuedStatus)
	}

	if got := s.cancelJob(queued.ID); got != cancelStarted {
		t.Fatalf("canceling a queued job = %v, want cancelStarted", got)
	}
	s.mu.RLock()
	got := s.jobs[queued.ID]
	queuedEnded := got.EndedAt != nil
	queuedStatus = got.Status
	s.mu.RUnlock()
	if queuedStatus != StatusCanceled || !queuedEnded {
		t.Fatalf("queued cancel left status=%q ended=%v", queuedStatus, queuedEnded)
	}

	if got := s.cancelJob(first.ID); got != cancelStarted {
		t.Fatalf("canceling a running job = %v, want cancelStarted", got)
	}
	waitFor(t, "running job canceled", func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		job := s.jobs[first.ID]
		return job.EndedAt != nil && job.Status == StatusCanceled
	})

	next, err := s.retryJob(first.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if next.ID == first.ID {
		t.Fatal("retry reused the original job id")
	}
	if next.Tab.ID != tab.ID || next.Tab.Song != tab.Song {
		t.Fatalf("retry lost the tab: %#v", next.Tab)
	}
	s.cancelJob(next.ID)
}

// TestJobCompletesAndPersists covers the success path plus history persistence.
func TestJobCompletesAndPersists(t *testing.T) {
	cache, err := OpenCacheAt(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	s := newDashboardState(dashboardConfig{DownloadDir: "./tabs", LogDir: "./logs", WorkerPath: "./download-worker.mjs"}, nil, cache, "test-token")
	runner := &fakeRunner{outcome: jobOutcome{saved: true, path: "/tmp/song_v1.gp7", bytes: 4096}}
	s.runner = runner.run
	s.startDispatcher()

	job := s.newJob(Tab{ID: 9, Song: "Nine", Artist: "Band"}, defaultOptions())
	s.enqueue(job)

	waitFor(t, "job completed", func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.jobs[job.ID].Status == StatusCompleted
	})

	s.mu.RLock()
	done := s.jobs[job.ID]
	if done.Path != "/tmp/song_v1.gp7" || done.Bytes != 4096 || done.Progress != 100 {
		t.Fatalf("unexpected completed job: path=%q bytes=%d progress=%d", done.Path, done.Bytes, done.Progress)
	}
	s.mu.RUnlock()

	stored, err := cache.Job(job.ID)
	if err != nil || stored == nil {
		t.Fatalf("job was not persisted: %v", err)
	}
	if stored.Status != StatusCompleted || stored.Tab.ID != 9 {
		t.Fatalf("persisted job mismatch: %#v", stored)
	}
	metrics, err := cache.Metrics()
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if metrics.TotalDownloads != 1 || metrics.Bytes != 4096 || metrics.SuccessRate != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
}

// TestJobsEndpointIncrementalLogs locks in the cursor semantics: a client that
// echoes the cursor back never re-reads a line.
func TestJobsEndpointIncrementalLogs(t *testing.T) {
	s := newTestDashboard()
	// No dispatcher: the job stays queued, which is enough to produce a log line.
	s.enqueue(s.newJob(Tab{ID: 3, Song: "Queued"}, defaultOptions()))

	var first jobsResponse
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/jobs", ""), &first)
	if len(first.Logs) == 0 {
		t.Fatal("expected the queueing to produce a log line")
	}
	if first.Cursor != first.Logs[len(first.Logs)-1].Seq {
		t.Fatalf("cursor %d does not match newest line %d", first.Cursor, first.Logs[len(first.Logs)-1].Seq)
	}
	if len(first.Jobs) != 1 || first.Jobs[0].Status != StatusQueued {
		t.Fatalf("jobs = %#v", first.Jobs)
	}

	var repeat jobsResponse
	jsonInto(t, doJSON(t, s, http.MethodGet, fmt.Sprintf("/api/jobs?since=%d", first.Cursor), ""), &repeat)
	if len(repeat.Logs) != 0 {
		t.Fatalf("cursor replay returned %d lines, want 0", len(repeat.Logs))
	}

	s.logf("", LevelInfo, "later line")
	var tail jobsResponse
	jsonInto(t, doJSON(t, s, http.MethodGet, fmt.Sprintf("/api/jobs?since=%d", first.Cursor), ""), &tail)
	if len(tail.Logs) != 1 || tail.Logs[0].Text != "later line" {
		t.Fatalf("incremental tail = %#v", tail.Logs)
	}
	if tail.Cursor <= first.Cursor {
		t.Fatalf("cursor did not advance: %d -> %d", first.Cursor, tail.Cursor)
	}

	bad := doJSON(t, s, http.MethodGet, "/api/jobs?since=-4", "")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("negative since = %d, want 400", bad.Code)
	}
}

// TestJobsCursorFromPreviousProcessReplays covers a cursor left over from an
// earlier dashboard process: seq restarts at 1, so a cursor ahead of the new head
// must replay from the start instead of leaving the console empty forever.
func TestJobsCursorFromPreviousProcessReplays(t *testing.T) {
	s := newTestDashboard()
	s.logf("", LevelInfo, "hello")

	var first jobsResponse
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/jobs", ""), &first)
	if len(first.Logs) != 1 {
		t.Fatalf("expected 1 line, got %d", len(first.Logs))
	}

	var stale jobsResponse
	jsonInto(t, doJSON(t, s, http.MethodGet, fmt.Sprintf("/api/jobs?since=%d", first.Cursor+500), ""), &stale)
	if len(stale.Logs) != 1 || stale.Logs[0].Text != "hello" {
		t.Fatalf("stale cursor did not replay: %#v", stale.Logs)
	}
	if stale.Cursor != first.Cursor {
		t.Fatalf("cursor changed unexpectedly: %d -> %d", first.Cursor, stale.Cursor)
	}
}

func TestLogRingCursorSemantics(t *testing.T) {
	r := newLogRing(8)
	if r.cursor() != 0 {
		t.Fatalf("empty ring cursor = %d, want 0", r.cursor())
	}
	a := r.add("job-1", LevelInfo, "first")
	b := r.add("", LevelWarn, "second")
	if a.Seq != 1 || b.Seq != 2 {
		t.Fatalf("sequence numbers = %d, %d; want 1, 2", a.Seq, b.Seq)
	}
	if got := r.since(0); len(got) != 2 {
		t.Fatalf("since(0) = %d lines, want 2", len(got))
	}
	if got := r.since(a.Seq); len(got) != 1 || got[0].Seq != b.Seq {
		t.Fatalf("since(%d) = %#v", a.Seq, got)
	}
	if r.cursor() != b.Seq {
		t.Fatalf("cursor = %d, want %d", r.cursor(), b.Seq)
	}

	for i := 0; i < 20; i++ {
		r.add("", LevelInfo, fmt.Sprintf("line-%d", i))
	}
	if r.len() != 8 {
		t.Fatalf("ring kept %d lines, want capacity 8", r.len())
	}
	all := r.since(0)
	if len(all) != 8 {
		t.Fatalf("since(0) after wrap = %d lines, want 8", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].Seq <= all[i-1].Seq {
			t.Fatalf("sequence numbers not monotonic at %d: %d then %d", i, all[i-1].Seq, all[i].Seq)
		}
	}
	if all[len(all)-1].Seq != r.cursor() {
		t.Fatalf("newest buffered line %d != cursor %d", all[len(all)-1].Seq, r.cursor())
	}
	if got := r.since(r.cursor()); len(got) != 0 {
		t.Fatalf("cursor at head returned %d lines, want 0", len(got))
	}

	// Oversized text is truncated rather than dropped.
	long := r.add("job-1", LevelInfo, strings.Repeat("x", maxLogTextLen+50))
	if len(long.Text) > maxLogTextLen+4 {
		t.Fatalf("long line length = %d, want truncation near %d", len(long.Text), maxLogTextLen)
	}
}

func TestHistoryAndFavoritesRoundTrip(t *testing.T) {
	cache, err := OpenCacheAt(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	started := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	ended := started.Add(30 * time.Second)
	opts := defaultOptions()
	opts.Format = "gp5"
	job := &Job{
		ID:        "job-a",
		Tab:       Tab{ID: 77, Song: "Song", Artist: "Band", URL: "https://tabs.ultimate-guitar.com/tab/77"},
		Status:    StatusCompleted,
		Path:      "/tabs/song_v1.gp5",
		Bytes:     2048,
		StartedAt: started,
		EndedAt:   &ended,
		Options:   opts,
	}
	if err := cache.SaveJob(job); err != nil {
		t.Fatalf("save job: %v", err)
	}
	failed := &Job{ID: "job-b", Tab: Tab{ID: 78}, Status: StatusFailed, StartedAt: started.Add(-time.Minute), Message: "boom"}
	if err := cache.SaveJob(failed); err != nil {
		t.Fatalf("save failed job: %v", err)
	}

	history, err := cache.JobHistory(10)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history = %d jobs, want 2", len(history))
	}
	if history[0].Tab.ID != 77 || history[0].Bytes != 2048 || history[0].EndedAt == nil {
		t.Fatalf("newest history row = %#v", history[0])
	}
	if history[0].Options.Format != "gp5" || history[0].Options.TimeoutSec != 30 {
		t.Fatalf("options did not round-trip: %#v", history[0].Options)
	}

	metrics, err := cache.Metrics()
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if metrics.TotalDownloads != 1 || metrics.Bytes != 2048 {
		t.Fatalf("metrics = %#v", metrics)
	}
	if metrics.SuccessRate != 0.5 {
		t.Fatalf("successRate = %v, want 0.5 (1 completed, 1 failed)", metrics.SuccessRate)
	}

	if err := cache.AddFavorite(Tab{ID: 77, Song: "Song"}); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	if err := cache.AddFavorite(Tab{ID: 77, Song: "Song renamed"}); err != nil {
		t.Fatalf("re-add favorite: %v", err)
	}
	favorites, err := cache.Favorites()
	if err != nil {
		t.Fatalf("favorites: %v", err)
	}
	if len(favorites) != 1 {
		t.Fatalf("favorites = %d, want 1 (upsert)", len(favorites))
	}
	if favorites[0].Song != "Song renamed" {
		t.Fatalf("favorite not updated: %#v", favorites[0])
	}
	if err := cache.RemoveFavorite(77); err != nil {
		t.Fatalf("remove favorite: %v", err)
	}
	if favorites, _ = cache.Favorites(); len(favorites) != 0 {
		t.Fatalf("favorites after delete = %d, want 0", len(favorites))
	}

	if err := cache.SaveSetting(settingOptions, `{"format":"gp5"}`); err != nil {
		t.Fatalf("save setting: %v", err)
	}
	stored, err := cache.Setting(settingOptions)
	if err != nil || !strings.Contains(stored, "gp5") {
		t.Fatalf("setting = %q, %v", stored, err)
	}
	missing, err := cache.Setting("never-written")
	if err != nil || missing != "" {
		t.Fatalf("missing setting = %q, %v; want empty and no error", missing, err)
	}
}

// TestCacheColdStart makes sure a brand-new database never crashes history or
// favorites loading.
func TestCacheColdStart(t *testing.T) {
	cache, err := OpenCacheAt(filepath.Join(t.TempDir(), "nested", "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	history, err := cache.JobHistory(10)
	if err != nil {
		t.Fatalf("history on cold db: %v", err)
	}
	if history == nil || len(history) != 0 {
		t.Fatalf("history = %#v, want empty non-nil", history)
	}
	favorites, err := cache.Favorites()
	if err != nil || len(favorites) != 0 {
		t.Fatalf("favorites = %#v, %v", favorites, err)
	}
	metrics, err := cache.Metrics()
	if err != nil {
		t.Fatalf("metrics on cold db: %v", err)
	}
	if metrics.TotalDownloads != 0 || metrics.Bytes != 0 || metrics.SuccessRate != 0 {
		t.Fatalf("cold metrics = %#v", metrics)
	}
	if job, err := cache.Job("nope"); err != nil || job != nil {
		t.Fatalf("missing job = %#v, %v", job, err)
	}
}

// TestOpenMemoryCacheKeepsAppUsable covers the degraded storage mode: with no
// writable cache directory the app still needs a working store so browsing,
// search and job bookkeeping keep functioning (just without persistence).
func TestOpenMemoryCacheKeepsAppUsable(t *testing.T) {
	cache, err := OpenMemoryCache()
	if err != nil {
		t.Fatalf("open memory cache: %v", err)
	}
	defer cache.Close()

	if err := cache.SaveSetting(settingOptions, `{"format":"gp5"}`); err != nil {
		t.Fatalf("save setting: %v", err)
	}
	if got, err := cache.Setting(settingOptions); err != nil || !strings.Contains(got, "gp5") {
		t.Fatalf("setting = %q, %v", got, err)
	}

	ended := time.Now()
	job := &Job{ID: "mem-1", Tab: Tab{ID: 3}, Status: StatusCompleted, StartedAt: ended.Add(-time.Minute), EndedAt: &ended, Options: defaultOptions()}
	if err := cache.SaveJob(job); err != nil {
		t.Fatalf("save job: %v", err)
	}
	history, err := cache.JobHistory(5)
	if err != nil || len(history) != 1 || history[0].ID != "mem-1" {
		t.Fatalf("history = %#v, %v", history, err)
	}
	if metrics, err := cache.Metrics(); err != nil || metrics.TotalDownloads != 1 {
		t.Fatalf("metrics = %#v, %v", metrics, err)
	}
	if err := cache.AddFavorite(Tab{ID: 3}); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	if favorites, err := cache.Favorites(); err != nil || len(favorites) != 1 {
		t.Fatalf("favorites = %#v, %v", favorites, err)
	}
}

// TestOpenCacheWithFallbackReturnsUsableCache points the cache directory at an
// unwritable location and asserts the degraded path is taken and still yields a
// working store. Under a root shell a 0500 directory stays writable, so the
// scenario is not reachable there and the test skips rather than lying.
func TestOpenCacheWithFallbackReturnsUsableCache(t *testing.T) {
	readOnly := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(readOnly, 0o500); err != nil {
		t.Fatalf("create read-only dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	t.Setenv("XDG_CACHE_HOME", readOnly)

	if _, err := OpenCache(); err == nil {
		t.Skip("environment permits writing to an unwritable directory (running as root?); degraded path not reachable")
	}

	cache, err := openCacheWithFallback()
	if err != nil {
		t.Fatalf("fallback returned an error instead of a usable cache: %v", err)
	}
	defer cache.Close()

	// The degraded store must behave like the real one for everything the UI does.
	if err := cache.SaveSetting("probe", "1"); err != nil {
		t.Fatalf("returned cache is not usable: %v", err)
	}
	if got, err := cache.Setting("probe"); err != nil || got != "1" {
		t.Fatalf("probe = %q, %v", got, err)
	}
	if artists, err := cache.Artists(); err != nil || len(artists) != 0 {
		t.Fatalf("artists on degraded cache = %#v, %v", artists, err)
	}
	if _, err := os.Stat(filepath.Join(readOnly, "echoheist-guitar-pro", "cache.db")); err == nil {
		t.Fatal("fallback still wrote an on-disk database")
	}
}

func TestWorkerEventParsing(t *testing.T) {
	job := &Job{}

	if _, ok := applyJobEvent(job, workerEvent{Event: "progress", Percent: 42, Bytes: 512}); ok {
		t.Fatal("progress must not emit a log line")
	}
	if job.Progress != 42 || job.Bytes != 512 {
		t.Fatalf("progress not applied: %#v", job)
	}

	line, ok := applyJobEvent(job, workerEvent{Event: "log", JobID: "job-1", Level: LevelWarn, Text: "hello"})
	if !ok || line.Level != LevelWarn || line.Text != "hello" || line.JobID != "job-1" {
		t.Fatalf("log event = %#v, %v", line, ok)
	}

	line, ok = applyJobEvent(job, workerEvent{Event: "saved", JobID: "job-1", Path: "/p.gp5", Bytes: 99})
	if !ok || job.Path != "/p.gp5" || job.Bytes != 99 || job.Progress != 100 {
		t.Fatalf("saved event = %#v / job %#v", line, job)
	}

	line, ok = applyJobEvent(job, workerEvent{Event: "canceled", JobID: "job-1"})
	if !ok || line.Level != LevelWarn {
		t.Fatalf("canceled event = %#v, %v", line, ok)
	}

	line, ok = applyJobEvent(job, workerEvent{Event: "error", Message: "boom"})
	if !ok || line.Level != LevelError || line.Text != "boom" {
		t.Fatalf("error event = %#v, %v", line, ok)
	}

	line, ok = applyJobEvent(job, workerEvent{Event: "trace", Phase: "launch"})
	if !ok || line.Level != LevelDebug {
		t.Fatalf("trace event = %#v, %v", line, ok)
	}

	applyJobEvent(job, workerEvent{Event: "progress", Percent: 250})
	if job.Progress != 100 {
		t.Fatalf("progress clamp = %d, want 100", job.Progress)
	}
	applyJobEvent(job, workerEvent{Event: "progress", Percent: -7})
	if job.Progress != 0 {
		t.Fatalf("negative progress = %d, want 0", job.Progress)
	}
}

func TestOptionsMergeAndNormalize(t *testing.T) {
	base := defaultOptions()

	// Omitting verify must keep the safe default rather than disabling it.
	got := optionsInput{Format: stringPtr("gp5")}.merge(base)
	if !got.Verify {
		t.Fatal("omitted verify disabled payload checking")
	}
	if got.Format != "gp5" || got.TimeoutSec != 30 {
		t.Fatalf("merge = %#v", got)
	}
	// Explicit false is honoured.
	if (optionsInput{Verify: boolPtr(false)}).merge(base).Verify {
		t.Fatal("explicit verify=false was ignored")
	}
	// Out-of-range and unknown values are repaired, not trusted.
	got = optionsInput{
		Format:     stringPtr("bogus"),
		TimeoutSec: intPtr(-5),
		Retries:    intPtr(-3),
		DelaySec:   floatPtr(-1),
	}.merge(base)
	if got.Format != "gp7" || got.TimeoutSec != 30 || got.Retries != 0 || got.DelaySec != 0 {
		t.Fatalf("normalize = %#v", got)
	}
	if (optionsInput{TimeoutSec: intPtr(99999)}).merge(base).TimeoutSec != 3600 {
		t.Fatal("timeout was not capped")
	}
	if opts := (Options{Versions: []string{" a ", "", "a", "b"}}).normalize(); len(opts.Versions) != 2 {
		t.Fatalf("versions not deduped/trimmed: %#v", opts.Versions)
	}
}

func TestStaticFrontendServing(t *testing.T) {
	s := newTestDashboard()
	h := s.routes()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	page := rec.Body.String()
	if !strings.Contains(page, "test-token") {
		t.Fatal("served page does not carry the run token")
	}
	if strings.Contains(page, tokenPlaceholder) {
		t.Fatal("token placeholder was not substituted")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}

	entries, err := fs.ReadDir(dashboardDist, "assets")
	if err != nil || len(entries) == 0 {
		t.Fatalf("embedded assets missing: %v", err)
	}
	// Asset filenames are content-hashed by Vite, so pick by extension.
	for _, want := range []struct {
		suffix      string
		contentType string
	}{
		{".js", "javascript"},
		{".css", "text/css"},
	} {
		name := ""
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), want.suffix) {
				name = entry.Name()
				break
			}
		}
		if name == "" {
			t.Fatalf("no %s asset in the embedded build", want.suffix)
		}
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/assets/"+name, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", name, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, want.contentType) {
			t.Fatalf("%s content type = %q, want %q", name, ct, want.contentType)
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("%s body was empty", name)
		}
	}

	// Unknown client routes fall back to the shell so routing works.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/settings", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "test-token") {
		t.Fatalf("SPA fallback = %d, body %q", rec.Code, rec.Body.String())
	}

	// A missing compiled asset is a real 404 so broken bundles are visible.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard/assets/missing.js", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset = %d, want 404", rec.Code)
	}

	// Traversal is refused by the handler itself, bypassing mux path cleaning.
	traversal := httptest.NewRequest(http.MethodGet, "/dashboard/x", nil)
	traversal.URL.Path = "/dashboard/../../etc/passwd"
	rec = httptest.NewRecorder()
	s.asset(rec, traversal)
	if rec.Code == http.StatusOK {
		t.Fatalf("traversal served %d with body %q", rec.Code, rec.Body.String())
	}
}

func TestLogsListAndTail(t *testing.T) {
	dir := t.TempDir()
	s := newTestDashboard()
	s.mu.Lock()
	s.config.LogDir = dir
	s.mu.Unlock()

	var builder strings.Builder
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&builder, "line-%03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "job-1.log"), []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	var list struct {
		Files []logFileInfo `json:"files"`
	}
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/logs", ""), &list)
	if len(list.Files) != 1 || list.Files[0].Name != "job-1.log" {
		t.Fatalf("log list = %#v", list.Files)
	}
	if list.Files[0].Size == 0 || list.Files[0].Modified == "" {
		t.Fatalf("log metadata = %#v", list.Files[0])
	}

	var tail struct {
		Name  string   `json:"name"`
		Lines []string `json:"lines"`
	}
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/logs/job-1.log", ""), &tail)
	if tail.Name != "job-1.log" {
		t.Fatalf("tail name = %q", tail.Name)
	}
	if len(tail.Lines) != logTailLines {
		t.Fatalf("tail returned %d lines, want %d", len(tail.Lines), logTailLines)
	}
	if tail.Lines[len(tail.Lines)-1] != "line-599" {
		t.Fatalf("tail last line = %q, want line-599", tail.Lines[len(tail.Lines)-1])
	}

	if rec := doJSON(t, s, http.MethodGet, "/api/logs/nope.log", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("missing log = %d, want 404", rec.Code)
	}

	// Traversal and absolute paths are refused by the handler directly.
	for _, raw := range []string{"../../etc/passwd", "/etc/passwd", "sub/dir.log"} {
		req := httptest.NewRequest(http.MethodGet, "/api/logs/x", nil)
		req.URL.Path = "/api/logs/" + raw
		rec := httptest.NewRecorder()
		s.logTail(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("log name %q = %d, want 400", raw, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "root:") {
			t.Fatalf("log name %q leaked file contents", raw)
		}
	}
}

func TestStateEndpointAndClear(t *testing.T) {
	s := newTestDashboard()

	var state State
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/state", ""), &state)
	if state.Version == "" {
		t.Fatal("state.version is empty")
	}
	if len(state.Sources) != 5 {
		t.Fatalf("sources = %d, want 5", len(state.Sources))
	}
	for _, src := range state.Sources {
		if !src.Enabled && src.Reason == "" {
			t.Fatalf("disabled source %q has no reason", src.ID)
		}
	}
	if state.Options.TimeoutSec != 30 || !state.Options.Verify || state.Options.Format != "gp7" {
		t.Fatalf("default options = %#v", state.Options)
	}
	if state.Jobs == nil {
		t.Fatal("state.jobs must serialize as [] not null")
	}
	if state.Config.WorkerPath != "/opt/echoheist/download-worker.mjs" {
		t.Fatalf("config.workerPath = %q", state.Config.WorkerPath)
	}

	done := s.newJob(Tab{ID: 1, Song: "Done"}, defaultOptions())
	s.enqueue(done)
	s.finish(done, StatusCompleted, "ok", "/p.gp7", 10)
	live := s.newJob(Tab{ID: 2, Song: "Live"}, defaultOptions())
	s.enqueue(live)

	var cleared struct {
		Removed int `json:"removed"`
	}
	jsonInto(t, doJSON(t, s, http.MethodPost, "/api/jobs/clear", ""), &cleared)
	if cleared.Removed != 1 {
		t.Fatalf("cleared %d jobs, want 1", cleared.Removed)
	}
	if jobs := s.snapshotJobs(); len(jobs) != 1 || jobs[0].Tab.ID != 2 {
		t.Fatalf("remaining jobs = %#v", jobs)
	}

	var sources struct {
		Items []Source `json:"items"`
	}
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/sources", ""), &sources)
	if len(sources.Items) != 5 {
		t.Fatalf("/api/sources = %d items", len(sources.Items))
	}

	if rec := doJSON(t, s, http.MethodPost, "/api/jobs/nope/retry", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("retry of unknown job = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, s, http.MethodDelete, "/api/jobs/nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete of unknown job = %d, want 404", rec.Code)
	}
	if rec := doJSON(t, s, http.MethodPost, "/api/jobs", `{"tabs":[]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty tabs = %d, want 400", rec.Code)
	}
}

// TestPostJobsUsesOptions verifies the accepted shape and that the chosen options
// become the server-side default.
func TestPostJobsUsesOptions(t *testing.T) {
	s := newTestDashboard()
	body := `{"tabs":[{"id":11,"song":"S","artist":"A"}],"options":{"format":"gp5","timeoutSec":12,"verify":false}}`
	var accepted struct {
		IDs []string `json:"ids"`
	}
	rec := doJSON(t, s, http.MethodPost, "/api/jobs", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /api/jobs = %d, want 202 (%s)", rec.Code, rec.Body.String())
	}
	jsonInto(t, rec, &accepted)
	if len(accepted.IDs) != 1 {
		t.Fatalf("ids = %#v", accepted.IDs)
	}

	s.mu.RLock()
	opts := s.options
	s.mu.RUnlock()
	if opts.Format != "gp5" || opts.TimeoutSec != 12 || opts.Verify {
		t.Fatalf("options not adopted: %#v", opts)
	}

	s.mu.RLock()
	job := s.jobs[accepted.IDs[0]]
	s.mu.RUnlock()
	if job == nil || job.Tag != "GP5" || job.Source != "Ultimate Guitar" {
		t.Fatalf("job = %#v", job)
	}
}

func TestWorkerArgsCarryOptions(t *testing.T) {
	opts := defaultOptions()
	opts.Format = "gpx"
	opts.Versions = []string{"official", "gp-user"}
	opts.Include = []string{"tabs", "bass"}
	opts.TimeoutSec = 45
	opts.Retries = 2
	opts.DelaySec = 2.5
	opts.Verify = false

	job := &Job{ID: "job-1", Tab: Tab{Artist: "A", Song: "S"}, Options: opts}
	args := workerArgs("/w.mjs", dashboardConfig{DownloadDir: "/out"}, job, "https://tabs.ultimate-guitar.com/tab/1")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--format gpx",
		"--versions official,gp-user",
		"--include tabs,bass",
		"--timeout-sec 45",
		"--retries 2",
		"--delay-sec 2.5",
		"--verify 0",
		"--job-id job-1",
		"--output-dir /out",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("worker args missing %q: %s", want, joined)
		}
	}
}

func TestTailFileEdgeCases(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := tailFile(empty, 10)
	if err != nil || len(lines) != 0 {
		t.Fatalf("empty file = %#v, %v", lines, err)
	}

	short := filepath.Join(dir, "short.log")
	if err := os.WriteFile(short, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err = tailFile(short, 2)
	if err != nil || len(lines) != 2 || lines[0] != "b" || lines[1] != "c" {
		t.Fatalf("short file = %#v, %v", lines, err)
	}

	if _, err := tailFile(filepath.Join(dir, "missing.log"), 5); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file error = %v, want fs.ErrNotExist", err)
	}
}

// TestFavoritesAndHistoryEndpoints exercises the handlers against a real
// temporary database, including the empty case.
func TestFavoritesAndHistoryEndpoints(t *testing.T) {
	cache, err := OpenCacheAt(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	s := newDashboardState(dashboardConfig{DownloadDir: "./tabs", LogDir: "./logs"}, nil, cache, "test-token")

	var empty struct {
		Items []Tab `json:"items"`
	}
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/favorites", ""), &empty)
	if empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty favorites = %#v, want []", empty.Items)
	}

	body := `{"tab":{"id":4242,"song":"Song","artist":"Band"}}`
	if rec := doJSON(t, s, http.MethodPost, "/api/favorites", body); rec.Code != http.StatusOK {
		t.Fatalf("POST /api/favorites = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var listed struct {
		Items []Tab `json:"items"`
	}
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/favorites", ""), &listed)
	if len(listed.Items) != 1 || listed.Items[0].ID != 4242 {
		t.Fatalf("favorites = %#v", listed.Items)
	}
	if rec := doJSON(t, s, http.MethodDelete, "/api/favorites/4242", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/favorites/4242 = %d, want 200", rec.Code)
	}
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/favorites", ""), &listed)
	if len(listed.Items) != 0 {
		t.Fatalf("favorites after delete = %#v", listed.Items)
	}
	if rec := doJSON(t, s, http.MethodDelete, "/api/favorites/not-a-number", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE bad favorite id = %d, want 400", rec.Code)
	}

	// History starts empty and returns persisted jobs newest first.
	var history struct {
		Items []Job `json:"items"`
	}
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/history?limit=5", ""), &history)
	if history.Items == nil || len(history.Items) != 0 {
		t.Fatalf("empty history = %#v, want []", history.Items)
	}
	ended := time.Now()
	stored := &Job{ID: "hist-1", Tab: Tab{ID: 5, Song: "Five"}, Status: StatusCompleted, Bytes: 100, StartedAt: time.Now().Add(-time.Minute), EndedAt: &ended, Options: defaultOptions()}
	if err := cache.SaveJob(stored); err != nil {
		t.Fatalf("save job: %v", err)
	}
	jsonInto(t, doJSON(t, s, http.MethodGet, "/api/history", ""), &history)
	if len(history.Items) != 1 || history.Items[0].ID != "hist-1" {
		t.Fatalf("history = %#v", history.Items)
	}
	if rec := doJSON(t, s, http.MethodGet, "/api/history?limit=nope", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad limit = %d, want 400", rec.Code)
	}

	// Retry works from persisted history after the live list is cleared.
	next, err := s.retryJob("hist-1")
	if err != nil {
		t.Fatalf("retry from history: %v", err)
	}
	if next.Tab.ID != 5 {
		t.Fatalf("retried tab = %#v", next.Tab)
	}
	s.cancelJob(next.ID)
}

// TestLogsEndpointWithoutDirectory must not 500 when the log dir does not exist.
func TestLogsEndpointWithoutDirectory(t *testing.T) {
	s := newTestDashboard()
	s.mu.Lock()
	s.config.LogDir = filepath.Join(t.TempDir(), "absent")
	s.mu.Unlock()

	rec := doJSON(t, s, http.MethodGet, "/api/logs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("missing log dir = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var list struct {
		Files []logFileInfo `json:"files"`
	}
	jsonInto(t, rec, &list)
	if list.Files == nil || len(list.Files) != 0 {
		t.Fatalf("files = %#v, want []", list.Files)
	}
}

func stringPtr(v string) *string  { return &v }
func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
func boolPtr(v bool) *bool        { return &v }
