package main

import (
	"os"
	"strings"
	"testing"
)

// TestWorkerIsObserveOnly pins the single property this tool now rests on: the
// worker observes a download response and reports metadata, and it cannot hand
// back a file.
//
// The worker is JavaScript living inside a Go module with no Node test runner, so
// rather than executing Chromium this is a source-level contract. It exists for
// the same reason its predecessor did: a documented behaviour that nothing checks
// drifts from the code, which is how SECURITY.md, README.md and web/CONTRACT.md
// once became untrue. README.md, SECURITY.md and the UIs all describe an
// observation-only build; this test fails the moment that stops being true.
func TestWorkerIsObserveOnly(t *testing.T) {
	raw, err := os.ReadFile("download-worker.mjs")
	if err != nil {
		t.Fatalf("read worker: %v", err)
	}
	src := string(raw)

	// Behaviour that is still advertised must keep its call sites.
	for _, tc := range []struct{ behaviour, needle string }{
		{"delay is enforced before navigation", "await sleep(pauseMs)"},
		{"the startup log labels which options are enforced", "enforced:"},
		{"a disabled payload check is reported", "--verify 0"},
		{"the payload check still runs", "verifyPayload(bodyBuffer"},
		{"a failed payload check is reported", "payload failed verification"},
		{"the metadata observation is emitted", `event: "observed"`},
		{"the observe-only boundary is marked in the source", "Observe-only boundary"},
	} {
		if !strings.Contains(src, tc.needle) {
			t.Fatalf("%s: download-worker.mjs is missing %q", tc.behaviour, tc.needle)
		}
	}

	// The removals are the point of this build. Reinstating any of these without
	// changing README.md, SECURITY.md, both UIs and this test is the failure mode
	// the test guards: a payload written to disk behind a read-only façade.
	for _, tc := range []struct{ behaviour, needle string }{
		{"the payload is never persisted", "fs.writeFile("},
		{"no staging file is renamed into place", "fs.rename("},
		{"the download directory is never created", "fs.mkdir("},
		{"the captured request is never replayed from Node", "fetch(downloadRequest.url"},
		{"the worker never announces a saved file", `event: "saved"`},
		{"no sidecar report object exists", "const report = {"},
	} {
		if strings.Contains(src, tc.needle) {
			t.Fatalf("%s: download-worker.mjs still contains %q", tc.behaviour, tc.needle)
		}
	}
}
