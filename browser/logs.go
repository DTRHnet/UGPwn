package main

import (
	"strings"
	"sync"
	"time"
)

const (
	// maxLogLines bounds the in-memory ring so a chatty worker cannot grow the
	// process without limit; the oldest lines are dropped first.
	maxLogLines = 5000
	// maxLogTextLen caps one line so a single enormous payload cannot dominate
	// the ring or the JSON response.
	maxLogTextLen = 4000
)

// logRing is a bounded, monotonically-numbered log buffer. The frontend keeps a
// cursor and asks for lines with a greater Seq, so numbering must never repeat
// or go backwards.
type logRing struct {
	mu    sync.Mutex
	buf   []LogLine
	start int
	next  int64
}

func newLogRing(capacity int) *logRing {
	if capacity <= 0 {
		capacity = maxLogLines
	}
	return &logRing{buf: make([]LogLine, 0, capacity), next: 1}
}

// add appends one line and returns it with its assigned sequence number.
func (r *logRing) add(jobID, level, text string) LogLine {
	text = truncateText(text, maxLogTextLen)
	if level == "" {
		level = LevelInfo
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	line := LogLine{Seq: r.next, JobID: jobID, Level: level, Text: text, At: time.Now()}
	r.next++
	if len(r.buf) < cap(r.buf) {
		r.buf = append(r.buf, line)
		return line
	}
	// Buffer is full: overwrite the oldest slot and advance the window.
	r.buf[r.start] = line
	r.start = (r.start + 1) % len(r.buf)
	return line
}

// since returns every buffered line with Seq > seq, oldest first.
func (r *logRing) since(seq int64) []LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]LogLine, 0, len(r.buf))
	for i := 0; i < len(r.buf); i++ {
		line := r.buf[(r.start+i)%len(r.buf)]
		if line.Seq > seq {
			out = append(out, line)
		}
	}
	return out
}

// cursor is the sequence number of the newest line, or 0 when empty.
func (r *logRing) cursor() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.next - 1
}

// len reports how many lines are buffered (used by tests and /api/health).
func (r *logRing) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buf)
}

func truncateText(text string, limit int) string {
	text = strings.TrimRight(text, "\r\n")
	if len(text) <= limit {
		return text
	}
	// Trim on a rune boundary so the JSON stays valid UTF-8.
	cut := limit
	for cut > 0 && !utf8Start(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// utf8Start reports whether b starts a UTF-8 rune (i.e. is not a continuation
// byte).
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
