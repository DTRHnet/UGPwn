package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Job status values, matching JobStatus in web/CONTRACT.md.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
)

// Log levels, matching LogLine.level in web/CONTRACT.md.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
	LevelDebug = "debug"
)

// Options mirrors the contract's Options type.
//
// Enforced vs recorded semantics (see web/CONTRACT.md "What is actually
// enforced vs recorded"):
//   - TimeoutSec, Retries, DelaySec, Verify are enforced by the worker.
//   - Format only biases the saved extension; there is no transcoding.
//   - Versions and Include are recorded in the sidecar report only.
type Options struct {
	Format     string   `json:"format"`
	Versions   []string `json:"versions"`
	Include    []string `json:"include"`
	TimeoutSec int      `json:"timeoutSec"`
	Retries    int      `json:"retries"`
	DelaySec   float64  `json:"delaySec"`
	Verify     bool     `json:"verify"`
}

// Job is the contract's Job type. The unexported fields carry scheduler state
// and are never serialized.
type Job struct {
	ID        string     `json:"id"`
	Tab       Tab        `json:"tab"`
	Status    string     `json:"status"`
	Message   string     `json:"message,omitempty"`
	Path      string     `json:"path,omitempty"`
	Bytes     int64      `json:"bytes"`
	Progress  int        `json:"progress"`
	Source    string     `json:"source"`
	Tag       string     `json:"tag"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`

	Options  Options            `json:"-"`
	cancel   context.CancelFunc `json:"-"`
	canceled bool               `json:"-"`
}

// clone copies a job for serialization without sharing the EndedAt pointer.
func (j *Job) clone() *Job {
	if j == nil {
		return nil
	}
	out := *j
	if j.EndedAt != nil {
		t := *j.EndedAt
		out.EndedAt = &t
	}
	return &out
}

// LogLine is one ring-buffer entry. Seq is monotonic across the process.
type LogLine struct {
	Seq   int64     `json:"seq"`
	JobID string    `json:"jobId"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
}

// Source is a selectable tab source. Only Ultimate Guitar is implemented; the
// rest carry a reason so the UI can disable them with a tooltip instead of
// silently doing nothing.
type Source struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

// Metrics feed the footer. Bytes is an absolute total, never a rate: the
// frontend must not invent bandwidth numbers.
type Metrics struct {
	TotalDownloads int     `json:"totalDownloads"`
	SuccessRate    float64 `json:"successRate"`
	Bytes          int64   `json:"bytes"`
}

// Counts summarises the live job list.
type Counts struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// State is the GET /api/state payload.
type State struct {
	Version string          `json:"version"`
	Config  dashboardConfig `json:"config"`
	Jobs    []*Job          `json:"jobs"`
	Counts  Counts          `json:"counts"`
	Metrics Metrics         `json:"metrics"`
	Sources []Source        `json:"sources"`
	Options Options         `json:"options"`
}

// defaultOptions returns the server-side defaults exposed by /api/state so the
// UI can reset to them.
func defaultOptions() Options {
	return Options{
		Format:     "gp7",
		Versions:   []string{"all"},
		Include:    []string{"tabs"},
		TimeoutSec: 30,
		Retries:    5,
		DelaySec:   1.5,
		Verify:     true,
	}
}

// defaultSources lists every source the UI may show. Unimplemented sources stay
// visible but disabled, with the reason the UI renders as a tooltip.
func defaultSources() []Source {
	const outOfScope = "Not implemented — outside the scope of this research tool"
	return []Source{
		{ID: "ultimate-guitar", Name: "Ultimate Guitar", Enabled: true},
		{ID: "songsterr", Name: "Songsterr", Enabled: false, Reason: outOfScope},
		{ID: "guitar-pro-tabs", Name: "Guitar Pro Tabs", Enabled: false, Reason: outOfScope},
		{ID: "mysongbook", Name: "MySongBook", Enabled: false, Reason: outOfScope},
		{ID: "musescore", Name: "Musescore", Enabled: false, Reason: outOfScope},
	}
}

var allowedFormats = map[string]bool{"gp7": true, "gp6": true, "gp5": true, "gpx": true, "gp": true}

// normalize repairs out-of-range or unknown option values instead of rejecting
// the request: the UI can only ever send contract values, but a stray value must
// not be able to disable a safety check or hang the worker.
func (o Options) normalize() Options {
	out := o
	if !allowedFormats[strings.ToLower(strings.TrimSpace(out.Format))] {
		out.Format = defaultOptions().Format
	} else {
		out.Format = strings.ToLower(strings.TrimSpace(out.Format))
	}
	out.Versions = normalizeList(out.Versions, 32, 64)
	out.Include = normalizeList(out.Include, 32, 64)
	if out.TimeoutSec <= 0 {
		out.TimeoutSec = defaultOptions().TimeoutSec
	}
	if out.TimeoutSec > 3600 {
		out.TimeoutSec = 3600
	}
	if out.Retries < 0 {
		out.Retries = 0
	}
	if out.Retries > 20 {
		out.Retries = 20
	}
	if out.DelaySec < 0 {
		out.DelaySec = 0
	}
	if out.DelaySec > 60 {
		out.DelaySec = 60
	}
	return out
}

func normalizeList(in []string, maxItems, maxLen int) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		item := strings.TrimSpace(raw)
		if item == "" || len(item) > maxLen || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

// optionsInput decodes a partial Options so that an omitted field keeps the
// server default (notably `verify`, whose zero value would silently turn
// payload checking off).
type optionsInput struct {
	Format     *string  `json:"format"`
	Versions   []string `json:"versions"`
	Include    []string `json:"include"`
	TimeoutSec *int     `json:"timeoutSec"`
	Retries    *int     `json:"retries"`
	DelaySec   *float64 `json:"delaySec"`
	Verify     *bool    `json:"verify"`
}

// merge overlays the supplied fields onto base and normalizes the result.
func (in optionsInput) merge(base Options) Options {
	out := base
	if in.Format != nil {
		out.Format = *in.Format
	}
	if in.Versions != nil {
		out.Versions = in.Versions
	}
	if in.Include != nil {
		out.Include = in.Include
	}
	if in.TimeoutSec != nil {
		out.TimeoutSec = *in.TimeoutSec
	}
	if in.Retries != nil {
		out.Retries = *in.Retries
	}
	if in.DelaySec != nil {
		out.DelaySec = *in.DelaySec
	}
	if in.Verify != nil {
		out.Verify = *in.Verify
	}
	return out.normalize()
}

// jobTag is the short label the queue table shows (GP7, GP6, …).
func jobTag(format string) string {
	if format == "" {
		return "GP"
	}
	return strings.ToUpper(format)
}

// sourceName is the display name recorded on a job.
func sourceName() string { return "Ultimate Guitar" }

// describeEvent renders a worker event as a single human-readable log line.
func describeEvent(ev workerEvent) string {
	parts := []string{ev.Event}
	for _, extra := range []string{ev.Phase, ev.Code, ev.Message} {
		if extra != "" {
			parts = append(parts, extra)
		}
	}
	if ev.Path != "" {
		parts = append(parts, ev.Path)
	}
	if ev.Bytes > 0 {
		parts = append(parts, formatBytes(ev.Bytes))
	}
	if ev.Percent > 0 {
		parts = append(parts, fmt.Sprintf("%d%%", ev.Percent))
	}
	return strings.Join(parts, " — ")
}
