package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// jobQueueDepth bounds how many jobs may wait for the single worker slot.
	jobQueueDepth = 512
	// workerWaitDelay is how long Wait lingers on inherited pipes after the
	// process is cancelled before giving up on them.
	workerWaitDelay = 5 * time.Second
	// maxWorkerStderrLines bounds how many stderr lines reach the ring per job.
	maxWorkerStderrLines = 200
)

// jobOutcome is what a runner reports after a completed worker run.
type jobOutcome struct {
	path  string
	bytes int64
	saved bool
}

// jobRunner executes one job. The default implementation spawns the Puppeteer
// worker; tests substitute a fake so the lifecycle can be exercised without
// Chromium or the network.
type jobRunner func(ctx context.Context, job *Job) (jobOutcome, error)

// startDispatcher runs the single-slot job consumer. Downloads are sequential on
// purpose: one Chromium at a time keeps the queue readable and the machine sane.
func (s *dashboardState) startDispatcher() {
	s.mu.Lock()
	queue := s.queue
	s.mu.Unlock()
	go func() {
		for job := range queue {
			s.execute(job)
		}
	}()
}

// enqueue registers a job and hands it to the dispatcher.
func (s *dashboardState) enqueue(job *Job) {
	s.mu.Lock()
	s.jobs[job.ID] = job
	s.order = append(s.order, job.ID)
	s.mu.Unlock()

	s.logf(job.ID, LevelInfo, fmt.Sprintf("queued %s — %s", displayOr(job.Tab.Song, "untitled"), displayOr(job.Tab.Artist, "unknown artist")))
	select {
	case s.queue <- job:
	default:
		// Never drop a job silently: a full queue is reported as a failure.
		s.finish(job, StatusFailed, "queue is full; try again shortly", "", 0)
	}
}

// execute runs one job with its options-derived deadline and records the
// terminal status.
func (s *dashboardState) execute(job *Job) {
	s.mu.Lock()
	if job.canceled || job.Status == StatusCanceled {
		s.mu.Unlock()
		return
	}
	job.Status = StatusRunning
	job.StartedAt = time.Now()
	opts := job.Options
	base, cancelBase := context.WithCancel(context.Background())
	job.cancel = cancelBase
	s.mu.Unlock()

	timeout := workerJobTimeout
	if opts.TimeoutSec > 0 {
		timeout = time.Duration(opts.TimeoutSec) * time.Second
	}
	ctx, cancelTimeout := context.WithTimeout(base, timeout)
	defer cancelTimeout()
	defer cancelBase()

	s.logf(job.ID, LevelInfo, fmt.Sprintf("started (%s, retries %d, delay %gs, timeout %s)", jobTag(opts.Format), opts.Retries, opts.DelaySec, timeout))

	outcome, err := s.runner(ctx, job)

	s.mu.RLock()
	canceled := job.canceled
	s.mu.RUnlock()

	switch {
	case canceled:
		s.finish(job, StatusCanceled, "canceled", "", 0)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		s.finish(job, StatusFailed, fmt.Sprintf("timed out after %s", timeout), "", 0)
	case err != nil:
		s.finish(job, StatusFailed, err.Error(), "", 0)
	case outcome.saved:
		s.finish(job, StatusCompleted, "download finished", outcome.path, outcome.bytes)
	default:
		s.finish(job, StatusFailed, "worker did not report a saved file", "", 0)
	}

	s.mu.Lock()
	job.cancel = nil
	s.mu.Unlock()
}

// finish records a terminal status, persists it, and emits a server log line.
func (s *dashboardState) finish(job *Job, status, message, path string, bytes int64) {
	now := time.Now()
	s.mu.Lock()
	job.Status = status
	job.Message = message
	if path != "" {
		job.Path = path
	}
	if bytes > 0 {
		job.Bytes = bytes
	}
	if status == StatusCompleted {
		job.Progress = 100
	}
	job.EndedAt = &now
	persisted := job.clone()
	s.mu.Unlock()

	level := LevelInfo
	switch status {
	case StatusFailed:
		level = LevelError
	case StatusCanceled:
		level = LevelWarn
	}
	s.logf(job.ID, level, fmt.Sprintf("%s — %s", status, displayOr(message, "no detail")))

	if s.cache != nil {
		if err := s.cache.SaveJob(persisted); err != nil {
			s.logf(job.ID, LevelWarn, "could not persist job: "+err.Error())
		}
	}
	s.pruneJobs()
}

// cancelJob cancels a running job or retires a queued one.
func (s *dashboardState) cancelJob(id string) bool {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.mu.Unlock()
		return false
	}
	switch job.Status {
	case StatusQueued:
		now := time.Now()
		job.canceled = true
		job.Status = StatusCanceled
		job.Message = "canceled before it started"
		job.EndedAt = &now
		persisted := job.clone()
		s.mu.Unlock()
		s.logf(id, LevelWarn, "canceled while queued")
		if s.cache != nil {
			_ = s.cache.SaveJob(persisted)
		}
		s.pruneJobs()
		return true
	case StatusRunning:
		job.canceled = true
		cancel := job.cancel
		s.mu.Unlock()
		s.logf(id, LevelWarn, "cancellation requested; terminating the worker process group")
		if cancel != nil {
			cancel()
		}
		return true
	default:
		s.mu.Unlock()
		return false
	}
}

// retryJob re-queues the same tab and options under a fresh id. It falls back to
// persisted history so a job can be retried after the live list is cleared.
func (s *dashboardState) retryJob(id string) (*Job, error) {
	s.mu.RLock()
	original := s.jobs[id]
	if original != nil {
		clone := original.clone()
		s.mu.RUnlock()
		next := s.newJob(clone.Tab, clone.Options)
		s.enqueue(next)
		s.saveOptions(next.Options)
		return next, nil
	}
	s.mu.RUnlock()

	if s.cache != nil {
		stored, err := s.cache.Job(id)
		if err == nil && stored != nil {
			next := s.newJob(stored.Tab, stored.Options.normalize())
			s.enqueue(next)
			s.saveOptions(next.Options)
			return next, nil
		}
	}
	return nil, fmt.Errorf("job %q not found", id)
}

// newJob builds a queued job for a tab.
func (s *dashboardState) newJob(tab Tab, opts Options) *Job {
	now := time.Now()
	return &Job{
		ID:        fmt.Sprintf("tab-%d-%d", tab.ID, now.UnixNano()),
		Tab:       tab,
		Status:    StatusQueued,
		Source:    sourceName(),
		Tag:       jobTag(opts.Format),
		StartedAt: now,
		Options:   opts,
	}
}

// clearJobs drops finished jobs from the live list (history is untouched).
func (s *dashboardState) clearJobs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, job := range s.jobs {
		switch job.Status {
		case StatusCompleted, StatusFailed, StatusCanceled:
			delete(s.jobs, id)
			removed++
		}
	}
	if removed > 0 {
		kept := make([]string, 0, len(s.order))
		for _, id := range s.order {
			if _, ok := s.jobs[id]; ok {
				kept = append(kept, id)
			}
		}
		s.order = kept
	}
	return removed
}

// snapshotJobs clones the live job list in queue order.
func (s *dashboardState) snapshotJobs() []*Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Job, 0, len(s.order))
	for _, id := range s.order {
		if job, ok := s.jobs[id]; ok {
			out = append(out, job.clone())
		}
	}
	return out
}

// jobCounts summarises the live list for /api/state.
func (s *dashboardState) jobCounts() Counts {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var c Counts
	for _, job := range s.jobs {
		switch job.Status {
		case StatusQueued:
			c.Queued++
		case StatusRunning:
			c.Running++
		case StatusCompleted:
			c.Completed++
		case StatusFailed:
			c.Failed++
		}
	}
	return c
}

// logf appends a server-level log line to the ring buffer.
func (s *dashboardState) logf(jobID, level, text string) {
	if s.logs == nil {
		return
	}
	s.logs.add(jobID, level, text)
}

// applyJobEvent folds one worker protocol event into the job and returns the log
// line it should produce. Returning ok=false means the event only updates job
// state (progress ticks). Lines are always attributed to the job so the UI can
// group them even if the worker omits jobId.
func applyJobEvent(job *Job, ev workerEvent) (LogLine, bool) {
	if ev.JobID == "" {
		ev.JobID = job.ID
	}
	switch ev.Event {
	case "progress":
		percent := ev.Percent
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		job.Progress = percent
		if ev.Bytes > 0 {
			job.Bytes = ev.Bytes
		}
		return LogLine{}, false
	case "log":
		level := ev.Level
		if level == "" {
			level = LevelInfo
		}
		return LogLine{JobID: ev.JobID, Level: level, Text: ev.Text}, true
	case "canceled":
		return LogLine{JobID: ev.JobID, Level: LevelWarn, Text: "worker confirmed cancellation"}, true
	case "saved":
		job.Path = ev.Path
		if ev.Bytes > 0 {
			job.Bytes = ev.Bytes
		}
		job.Progress = 100
		return LogLine{JobID: ev.JobID, Level: LevelInfo, Text: "saved " + displayOr(ev.Path, "file")}, true
	case "completed":
		job.Progress = 100
		return LogLine{JobID: ev.JobID, Level: LevelInfo, Text: "worker completed"}, true
	case "error":
		return LogLine{JobID: ev.JobID, Level: LevelError, Text: displayOr(ev.Message, displayOr(ev.Code, "worker error"))}, true
	case "navigation-warning":
		return LogLine{JobID: ev.JobID, Level: LevelWarn, Text: describeEvent(ev)}, true
	default:
		return LogLine{JobID: ev.JobID, Level: LevelDebug, Text: describeEvent(ev)}, true
	}
}

// runWorker is the default runner: it spawns the Node worker, streams its stdout
// into the ring buffer, and terminates the whole process group on cancellation.
func (s *dashboardState) runWorker(ctx context.Context, job *Job) (jobOutcome, error) {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		return jobOutcome{}, fmt.Errorf("create log directory: %w", err)
	}
	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		return jobOutcome{}, fmt.Errorf("create download directory: %w", err)
	}
	url := canonicalTabRecordURL(job.Tab)
	if url == "" {
		return jobOutcome{}, fmt.Errorf("download URL is empty for tab %d", job.Tab.ID)
	}

	logFile, logErr := os.Create(filepath.Join(cfg.LogDir, job.ID+".log"))
	if logErr != nil {
		s.logf(job.ID, LevelWarn, "could not open job log file: "+logErr.Error())
	}
	if logFile != nil {
		defer logFile.Close()
	}

	cmd := exec.CommandContext(ctx, "node", workerArgs(cfg.WorkerPath, cfg, job, url)...)
	// Cancel the whole group, not just node: a cancelled job must not leave
	// Chromium children holding the download open. SIGTERM first so the worker
	// can emit its canceled event and close the browser; WaitDelay force-kills
	// the group if it does not exit in time.
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return terminateProcessGroup(cmd) }
	cmd.WaitDelay = workerWaitDelay

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return jobOutcome{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return jobOutcome{}, err
	}

	s.logf(job.ID, LevelDebug, "worker command: "+cmd.String())
	if logFile != nil {
		_, _ = fmt.Fprintf(logFile, "%s [system] %s\n", time.Now().Format(time.RFC3339Nano), cmd.String())
	}
	if err := cmd.Start(); err != nil {
		return jobOutcome{}, fmt.Errorf("start worker: %w", err)
	}

	var outcome jobOutcome
	var workerError string
	var completed bool
	var stderrLines int

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			s.writeLogFile(logFile, "stdout", line)
			var ev workerEvent
			if json.Unmarshal([]byte(line), &ev) != nil {
				if strings.TrimSpace(line) != "" {
					s.logf(job.ID, LevelInfo, line)
				}
				continue
			}
			s.mu.Lock()
			logLine, ok := applyJobEvent(job, ev)
			s.mu.Unlock()
			if ok {
				s.logf(logLine.JobID, logLine.Level, logLine.Text)
			}
			switch ev.Event {
			case "saved":
				outcome.saved = true
				outcome.path = ev.Path
				outcome.bytes = ev.Bytes
			case "completed":
				completed = true
			case "error":
				if ev.Message != "" {
					workerError = ev.Message
				}
			}
		}
		if err := scanner.Err(); err != nil {
			s.logf(job.ID, LevelWarn, "stdout stream: "+err.Error())
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			s.writeLogFile(logFile, "stderr", line)
			if strings.TrimSpace(line) == "" {
				continue
			}
			if stderrLines < maxWorkerStderrLines {
				stderrLines++
				s.logf(job.ID, LevelWarn, line)
			}
		}
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	if logFile != nil {
		_, _ = fmt.Fprintf(logFile, "[system] worker-exit=%v\n", waitErr)
	}

	s.mu.RLock()
	canceled := job.canceled
	s.mu.RUnlock()
	if canceled {
		return outcome, context.Canceled
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return outcome, ctxErr
	}
	if waitErr != nil {
		if workerError != "" {
			return outcome, errors.New(workerError)
		}
		return outcome, fmt.Errorf("worker exited: %w", waitErr)
	}
	if !outcome.saved && !completed {
		if workerError != "" {
			return outcome, errors.New(workerError)
		}
		return outcome, errors.New("worker did not report a saved file")
	}
	return outcome, nil
}

// workerArgs assembles the worker command line from the job options.
func workerArgs(worker string, cfg dashboardConfig, job *Job, url string) []string {
	opts := job.Options
	args := []string{
		"--url", url,
		"--output-dir", cfg.DownloadDir,
		"--artist", job.Tab.Artist,
		"--song", job.Tab.Song,
		"--job-id", job.ID,
	}
	if opts.Format != "" {
		args = append(args, "--format", opts.Format)
	}
	if len(opts.Versions) > 0 {
		args = append(args, "--versions", strings.Join(opts.Versions, ","))
	}
	if len(opts.Include) > 0 {
		args = append(args, "--include", strings.Join(opts.Include, ","))
	}
	if opts.TimeoutSec > 0 {
		args = append(args, "--timeout-sec", strconv.Itoa(opts.TimeoutSec))
	}
	args = append(args,
		"--retries", strconv.Itoa(opts.Retries),
		"--delay-sec", strconv.FormatFloat(opts.DelaySec, 'f', -1, 64),
		"--verify", boolFlag(opts.Verify),
	)
	return args
}

func boolFlag(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func (s *dashboardState) writeLogFile(f *os.File, stream, line string) {
	if f == nil || line == "" {
		return
	}
	_, _ = fmt.Fprintf(f, "%s [%s] %s\n", time.Now().Format(time.RFC3339Nano), stream, line)
}

func displayOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
