package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type downloadResult struct {
	Tab     Tab
	Path    string
	Bytes   int64
	Report  string
	Message string
	Err     error
}

type downloadProgress struct {
	session *downloadSession
	event   workerEvent
	line    string
	stream  string
}

type workerEvent struct {
	Event   string `json:"event"`
	JobID   string `json:"jobId"`
	Level   string `json:"level"`
	Text    string `json:"text"`
	Percent int    `json:"percent"`
	Total   int64  `json:"total"`
	Code    string `json:"code"`
	Phase   string `json:"phase"`
	URL     string `json:"url"`
	Path    string `json:"path"`
	Report  string `json:"report"`
	Bytes   int64  `json:"bytes"`
	Message string `json:"message"`
}

type workerOutput struct {
	event  workerEvent
	line   string
	stream string
	done   bool
	err    error
}

type downloadSession struct {
	tab     Tab
	ch      chan workerOutput
	saved   workerEvent
	last    workerEvent
	logFile *os.File
}

func runDownload(ctx context.Context, tab Tab, directory, worker string, logFile *os.File) tea.Cmd {
	return func() tea.Msg {
		tab.URL = canonicalTabRecordURL(tab)
		if tab.URL == "" {
			return downloadResult{Tab: tab, Err: fmt.Errorf("download URL is empty for tab %d", tab.ID)}
		}
		if absoluteWorker, err := filepath.Abs(worker); err == nil {
			worker = absoluteWorker
		}
		if absoluteDirectory, err := filepath.Abs(directory); err == nil {
			directory = absoluteDirectory
		}
		jobID := fmt.Sprintf("tab-%d", tab.ID)
		cmd := exec.CommandContext(ctx, "node", worker, "--url", tab.URL, "--output-dir", directory, "--artist", tab.Artist, "--song", tab.Song, "--job-id", jobID)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return downloadResult{Tab: tab, Err: err}
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return downloadResult{Tab: tab, Err: err}
		}
		session := &downloadSession{tab: tab, ch: make(chan workerOutput, 256), logFile: logFile}
		if logFile != nil {
			_, _ = fmt.Fprintf(logFile, "%s [system] starting worker command=%q url=%s output-dir=%s\n", time.Now().Format(time.RFC3339Nano), cmd.String(), tab.URL, directory)
		}
		go session.collect(cmd, stdout, stderr)
		return readDownloadOutput(session)()
	}
}

func (s *downloadSession) collect(cmd *exec.Cmd, stdout, stderr io.ReadCloser) {
	var wg sync.WaitGroup
	sendProgress := func(output workerOutput) {
		// Network tracing can produce thousands of lines while the Bubble Tea
		// event loop is rendering. The log file already contains the complete
		// stream, so never let UI progress backpressure block the worker's
		// stdout pipe and make the download hang.
		select {
		case s.ch <- output:
		default:
		}
	}
	sendTerminal := func(output workerOutput) {
		// Terminal messages must never be dropped. If the UI queue is full of
		// trace progress, discard queued progress until the result can be
		// delivered. The complete trace remains in the log file.
		for {
			select {
			case s.ch <- output:
				return
			default:
				select {
				case <-s.ch:
				default:
				}
			}
		}
	}
	read := func(stream string, input io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(input)
		// Network tracing can include large console/error payloads. Do not let
		// Scanner's 64 KiB default hide the useful part of a failing run.
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			s.log(stream, line)
			var event workerEvent
			if stream == "stdout" && json.Unmarshal([]byte(line), &event) == nil {
				s.last = event
				if event.Event == "saved" {
					s.saved = event
				}
				// Keep the complete structured event in the pane when it can be
				// consumed immediately; the log file remains lossless.
				sendProgress(workerOutput{event: event, line: line, stream: stream})
			} else if line != "" {
				sendProgress(workerOutput{line: line, stream: stream})
			}
		}
		if err := scanner.Err(); err != nil {
			sendProgress(workerOutput{line: err.Error(), stream: stream})
		}
	}
	if err := cmd.Start(); err != nil {
		s.log("system", fmt.Sprintf("start worker: %v", err))
		sendTerminal(workerOutput{done: true, err: fmt.Errorf("start worker: %w", err)})
		return
	}
	wg.Add(2)
	go read("stdout", stdout)
	go read("stderr", stderr)
	waitErr := cmd.Wait()
	wg.Wait()
	if waitErr != nil {
		s.log("system", fmt.Sprintf("worker exited: %v", waitErr))
		sendTerminal(workerOutput{done: true, err: fmt.Errorf("worker: %w url=%s", waitErr, s.tab.URL)})
	} else if s.saved.Path == "" && s.last.Event != "completed" && s.last.Event != "saved" {
		s.log("system", "worker did not emit a completed or saved event")
		sendTerminal(workerOutput{done: true, err: fmt.Errorf("worker did not complete the download")})
	} else {
		if s.saved.Path == "" {
			s.saved = s.last
		}
		sendTerminal(workerOutput{done: true})
	}
	if s.logFile != nil {
		_ = s.logFile.Sync()
	}
	close(s.ch)
}

func (s *downloadSession) log(stream, line string) {
	if s.logFile == nil || line == "" {
		return
	}
	_, _ = fmt.Fprintf(s.logFile, "%s [%s] %s\n", time.Now().Format(time.RFC3339Nano), stream, line)
}

func readDownloadOutput(s *downloadSession) tea.Cmd {
	return func() tea.Msg {
		output, ok := <-s.ch
		if !ok || output.done {
			if output.err != nil {
				return downloadResult{Tab: s.tab, Err: output.err}
			}
			return downloadResult{Tab: s.tab, Path: filepath.Clean(s.saved.Path), Bytes: s.saved.Bytes, Report: s.saved.Report}
		}
		return downloadProgress{session: s, line: output.line, event: output.event, stream: output.stream}
	}
}

func formatWorkerEvent(event workerEvent) string {
	parts := []string{event.Event}
	if event.Phase != "" {
		parts = append(parts, event.Phase)
	}
	if event.Code != "" {
		parts = append(parts, event.Code)
	}
	if event.Message != "" {
		parts = append(parts, event.Message)
	}
	if event.Path != "" {
		parts = append(parts, event.Path)
	}
	if event.Bytes > 0 {
		parts = append(parts, formatBytes(event.Bytes))
	}
	return strings.Join(parts, " — ")
}
