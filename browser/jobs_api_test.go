package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// TestJobActionStatusCodesDistinguishMissingFromFinished pins the difference
// between "no such job" (404) and "this job exists but can no longer be
// cancelled" (409). They used to share a 404, which made a finished job look
// like a mistyped id.
func TestJobActionStatusCodesDistinguishMissingFromFinished(t *testing.T) {
	s := newTestDashboard()
	s.jobs["done-1"] = &Job{ID: "done-1", Status: StatusCompleted, StartedAt: time.Now(), EndedAt: timePtr(time.Now())}
	s.jobs["queued-1"] = &Job{ID: "queued-1", Status: StatusQueued, StartedAt: time.Now()}
	h := s.routes()

	cases := []struct {
		name string
		id   string
		want int
	}{
		{"unknown id", "does-not-exist", http.StatusNotFound},
		{"already finished", "done-1", http.StatusConflict},
		{"still cancelable", "queued-1", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+tc.id, nil)
			req.Header.Set("X-Echoheist-Token", "test-token")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("DELETE /api/jobs/%s = %d, want %d (body %s)", tc.id, rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	// The cancelable job must actually reach a terminal state.
	if got := s.jobs["queued-1"]; got.Status != StatusCanceled {
		t.Fatalf("queued job status = %q, want %q", got.Status, StatusCanceled)
	}
}

// TestCancelJobOutcomeCoversAllThreeCases exercises the enum directly, so the
// handler's status mapping is backed by behaviour rather than only by its switch.
func TestCancelJobOutcomeCoversAllThreeCases(t *testing.T) {
	s := newTestDashboard()

	if got := s.cancelJob("missing"); got != cancelUnknown {
		t.Fatalf("unknown id = %v, want cancelUnknown", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	s.mu.Lock()
	s.jobs["done"] = &Job{ID: "done", Status: StatusCompleted, StartedAt: now, EndedAt: timePtr(now)}
	s.jobs["failed"] = &Job{ID: "failed", Status: StatusFailed, StartedAt: now, EndedAt: timePtr(now)}
	s.jobs["queued"] = &Job{ID: "queued", Status: StatusQueued, StartedAt: now}
	s.jobs["running"] = &Job{ID: "running", Status: StatusRunning, StartedAt: now, cancel: cancel}
	s.mu.Unlock()

	for _, id := range []string{"done", "failed"} {
		if got := s.cancelJob(id); got != cancelAlreadyFinished {
			t.Fatalf("finished job %q = %v, want cancelAlreadyFinished", id, got)
		}
	}

	if got := s.cancelJob("queued"); got != cancelStarted {
		t.Fatalf("queued job = %v, want cancelStarted", got)
	}
	s.mu.RLock()
	queuedStatus := s.jobs["queued"].Status
	s.mu.RUnlock()
	if queuedStatus != StatusCanceled {
		t.Fatalf("queued job status = %q, want %q", queuedStatus, StatusCanceled)
	}
	// Cancelling it a second time is now a conflict, not a second success.
	if got := s.cancelJob("queued"); got != cancelAlreadyFinished {
		t.Fatalf("second cancel = %v, want cancelAlreadyFinished", got)
	}

	if got := s.cancelJob("running"); got != cancelStarted {
		t.Fatalf("running job = %v, want cancelStarted", got)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelling a running job did not cancel its context")
	}
}

// TestJobActionHistoryOnlyIDIsConflict covers a job cleared from the live list
// but still recorded: it is finished, not missing.
func TestJobActionHistoryOnlyIDIsConflict(t *testing.T) {
	cache, err := OpenCacheAt(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	defer cache.Close()

	ended := time.Now()
	stored := &Job{
		ID:        "hist-1",
		Tab:       Tab{ID: 1, Song: "Gone"},
		Status:    StatusCompleted,
		StartedAt: ended.Add(-time.Minute),
		EndedAt:   &ended,
		Options:   defaultOptions(),
	}
	if err := cache.SaveJob(stored); err != nil {
		t.Fatalf("save job: %v", err)
	}

	s := newDashboardState(dashboardConfig{DownloadDir: "./tabs", LogDir: "./logs"}, nil, cache, "test-token")
	rec := doJSON(t, s, http.MethodDelete, "/api/jobs/hist-1", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("history-only id = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}

	// An id in neither place is still a 404.
	if rec := doJSON(t, s, http.MethodDelete, "/api/jobs/never-existed", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id = %d, want 404", rec.Code)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
