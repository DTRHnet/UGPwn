package main

import (
	"net/http"
	"net/http/httptest"
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

func timePtr(t time.Time) *time.Time { return &t }
