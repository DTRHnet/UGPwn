package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// songsModel builds a song-screen model without touching the network or SQLite.
func songsModel(groups []SongGroup) model {
	m := newModel(nil, nil)
	m.view, m.focus = songsScreen, songsPane
	m.groups = groups
	m.expanded = make(map[int]bool)
	m.downloadState = make(map[int]string)
	m.downloadQueue = nil
	m.query = textinput.New()
	return m
}

// TestSongRowsAddressUnfilteredGroups is the regression test for rows being
// numbered by filtered position while every consumer indexed the unfiltered
// group slice.
func TestSongRowsAddressUnfilteredGroups(t *testing.T) {
	m := songsModel([]SongGroup{
		{Song: "Zoo", Tabs: []Tab{{ID: 1, Song: "Zoo"}}},
		{Song: "apple", Tabs: []Tab{{ID: 2, Song: "apple"}, {ID: 3, Song: "apple"}}},
	})
	m.query.SetValue("app")

	rows := m.songRows()
	if len(rows) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(rows))
	}
	if rows[0].label != "apple" {
		t.Fatalf("row label = %q, want apple", rows[0].label)
	}
	if rows[0].group != 1 {
		t.Fatalf("row points at group %d, want unfiltered index 1", rows[0].group)
	}

	m.selected = 0
	if got := m.previewTab(); got.ID != 2 {
		t.Fatalf("previewTab() id = %d, want the displayed song's tab id 2", got.ID)
	}
}

// TestQueueSelectionUsesDisplayedSong locks in that a filtered view queues the
// song the user is looking at, not whichever group shares its index.
func TestQueueSelectionUsesDisplayedSong(t *testing.T) {
	m := songsModel([]SongGroup{
		{Song: "Zoo", Tabs: []Tab{{ID: 1, Song: "Zoo"}}},
		{Song: "apple", Tabs: []Tab{{ID: 2, Song: "apple"}, {ID: 3, Song: "apple"}}},
	})
	m.query.SetValue("app")
	m.expanded[1] = true
	m.selected = 1 // first child of "apple"

	if cmd := m.toggleDownloadSelection(); cmd != nil {
		t.Fatal("queueing a variant should not return a command")
	}
	if len(m.downloadQueue) != 1 {
		t.Fatalf("queued %d tabs, want 1", len(m.downloadQueue))
	}
	if got := m.downloadQueue[0].ID; got != 2 {
		t.Fatalf("queued tab id %d, want 2 (the displayed song)", got)
	}
}

func TestVariantTabRejectsStaleIndexes(t *testing.T) {
	m := songsModel([]SongGroup{{Song: "One", Tabs: []Tab{{ID: 5}}}})
	for _, row := range []songRow{{group: 3, variant: 0}, {group: 0, variant: 7}, {group: -1, variant: 0}} {
		if _, ok := m.variantTab(row); ok {
			t.Fatalf("variantTab(%#v) should have been rejected", row)
		}
	}
	if tab, ok := m.variantTab(songRow{group: 0, variant: 0}); !ok || tab.ID != 5 {
		t.Fatalf("variantTab returned %#v, %v", tab, ok)
	}
}

// TestSpaceKeyQueuesVariant covers the keybinding that was silently dead:
// bubbletea reports KeySpace as " ", so a `case "space":` never matched.
func TestSpaceKeyQueuesVariant(t *testing.T) {
	m := songsModel([]SongGroup{{Song: "One", Tabs: []Tab{{ID: 7, Song: "One"}, {ID: 8, Song: "One"}}}})
	m.expanded[0] = true
	m.selected = 1

	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeySpace})
	next, ok := got.(model)
	if !ok {
		t.Fatalf("handleKey returned %T, want model", got)
	}
	if len(next.downloadQueue) != 1 || next.downloadQueue[0].ID != 7 {
		t.Fatalf("space queued %#v, want tab 7", next.downloadQueue)
	}
}

func TestVKeyQueuesVariant(t *testing.T) {
	m := songsModel([]SongGroup{{Song: "One", Tabs: []Tab{{ID: 9, Song: "One"}, {ID: 10, Song: "One"}}}})
	m.expanded[0] = true
	m.selected = 1

	got, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	next, ok := got.(model)
	if !ok {
		t.Fatalf("handleKey returned %T, want model", got)
	}
	if len(next.downloadQueue) != 1 || next.downloadQueue[0].ID != 9 {
		t.Fatalf("v queued %#v, want tab 9", next.downloadQueue)
	}
}

func newTestDashboard() *dashboardState {
	return &dashboardState{
		config:   dashboardConfig{WorkerPath: "/opt/echoheist/download-worker.mjs", DownloadDir: "./tabs", LogDir: "./logs"},
		jobs:     make(map[string]*dashboardJob),
		token:    "test-token",
		jobLimit: maxDashboardJobs,
	}
}

// TestDashboardAPIRequiresToken proves the drive-by chain is closed: without the
// run token nothing under /api is reachable, and no CORS grant is emitted.
func TestDashboardAPIRequiresToken(t *testing.T) {
	h := newTestDashboard().routes()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated /api/health = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-Echoheist-Token", "test-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated /api/health = %d, want 200", rec.Code)
	}

	pre := httptest.NewRequest(http.MethodOptions, "/api/config", nil)
	pre.Header.Set("Origin", "https://evil.test")
	pre.Header.Set("Access-Control-Request-Method", "PUT")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, pre)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, dashboard must stay same-origin", got)
	}
}

// TestDashboardConfigRejectsWorkerPathMutation locks in that the executed script
// cannot be repointed over HTTP.
func TestDashboardConfigRejectsWorkerPathMutation(t *testing.T) {
	s := newTestDashboard()
	body := strings.NewReader(`{"workerPath":"/tmp/evil.mjs","downloadDir":"./out"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/config", body)
	req.Header.Set("X-Echoheist-Token", "test-token")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config = %d, want 200", rec.Code)
	}
	if s.config.WorkerPath != "/opt/echoheist/download-worker.mjs" {
		t.Fatalf("worker path mutated to %q", s.config.WorkerPath)
	}
	if s.config.DownloadDir != "out" {
		t.Fatalf("download dir = %q, want cleaned \"out\"", s.config.DownloadDir)
	}
}

func TestDashboardConfigValidatesDirectories(t *testing.T) {
	s := newTestDashboard()
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"downloadDir":"   "}`))
	req.Header.Set("X-Echoheist-Token", "test-token")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("blank download dir = %d, want 400", rec.Code)
	}
}

func TestDashboardFrontendSubstitutesToken(t *testing.T) {
	s := newTestDashboard()
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	page := rec.Body.String()
	if !strings.Contains(page, "test-token") {
		t.Fatal("served dashboard page does not carry the run token")
	}
	if strings.Contains(page, tokenPlaceholder) {
		t.Fatal("token placeholder was not substituted")
	}
}

func TestDashboardPrunesFinishedJobs(t *testing.T) {
	s := newTestDashboard()
	s.jobLimit = 2
	base := time.Now()
	for i := 0; i < 5; i++ {
		ended := base.Add(time.Duration(i) * time.Minute)
		s.jobs[fmt.Sprintf("job-%d", i)] = &dashboardJob{ID: fmt.Sprintf("job-%d", i), Status: "completed", EndedAt: &ended}
	}
	s.pruneJobs()
	if len(s.jobs) > s.jobLimit {
		t.Fatalf("jobs = %d after prune, want at most %d", len(s.jobs), s.jobLimit)
	}
	for _, id := range []string{"job-0", "job-1", "job-2"} {
		if _, ok := s.jobs[id]; ok {
			t.Fatalf("oldest job %q survived pruning", id)
		}
	}
	if _, ok := s.jobs["job-4"]; !ok {
		t.Fatal("newest job was pruned")
	}
}
