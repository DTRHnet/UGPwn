package main

import (
	"os"
	"strings"
	"testing"
)

// TestWorkerWiresDocumentedOptions guards the contract's "enforced vs recorded"
// table at source level.
//
// The worker is JavaScript living inside a Go module with no Node test runner, so
// rather than executing Chromium this pins the wiring: every helper the contract
// claims to run must actually have a call site, and every option the contract
// calls "recorded" must reach the sidecar report.
//
// It exists because these helpers were once defined without being called, which
// silently made SECURITY.md, README.md and web/CONTRACT.md untrue.
func TestWorkerWiresDocumentedOptions(t *testing.T) {
	raw, err := os.ReadFile("download-worker.mjs")
	if err != nil {
		t.Fatalf("read worker: %v", err)
	}
	src := string(raw)

	cases := []struct{ behaviour, needle string }{
		{"verify is applied to the payload before it is saved", "verifyPayload(bodyBuffer"},
		{"a failed verification aborts the save", "payload failed verification"},
		{"the verification outcome reaches the sidecar", "verification,"},
		{"delay is enforced before navigation", "await sleep(pauseMs)"},
		{"the startup log labels which options are enforced", "enforced:"},
		{"a disabled verification is reported", "--verify 0"},
	}
	for _, tc := range cases {
		if !strings.Contains(src, tc.needle) {
			t.Fatalf("%s: download-worker.mjs is missing %q", tc.behaviour, tc.needle)
		}
	}

	// versions/include only mean anything if they land inside the report object.
	start := strings.Index(src, "const report = {")
	if start < 0 {
		t.Fatal("download-worker.mjs has no sidecar report object")
	}
	window := src[start:]
	if len(window) > 900 {
		window = window[:900]
	}
	for _, field := range []string{"versions,", "include,", "format:", "verify: verifyEnabled"} {
		if !strings.Contains(window, field) {
			t.Fatalf("sidecar report is missing %q; recorded options must be written", field)
		}
	}
}
