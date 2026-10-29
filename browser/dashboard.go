package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed dashboard/*
var dashboardFiles embed.FS

type dashboardConfig struct {
	DownloadDir string `json:"downloadDir"`
	WorkerPath  string `json:"workerPath"`
	LogDir      string `json:"logDir"`
}

type dashboardJob struct {
	ID        string        `json:"id"`
	Tab       Tab           `json:"tab"`
	Status    string        `json:"status"`
	Message   string        `json:"message,omitempty"`
	Path      string        `json:"path,omitempty"`
	Bytes     int64         `json:"bytes,omitempty"`
	StartedAt time.Time     `json:"startedAt"`
	EndedAt   *time.Time    `json:"endedAt,omitempty"`
	Events    []workerEvent `json:"events,omitempty"`
}

type dashboardState struct {
	mu       sync.RWMutex
	config   dashboardConfig
	fetcher  *Fetcher
	cache    *Cache
	jobs     map[string]*dashboardJob
	token    string
	jobLimit int
}

const (
	// maxDashboardJobs bounds the job map for long-running sessions.
	maxDashboardJobs = 200
	// workerJobTimeout stops a wedged Chromium from holding a job forever.
	workerJobTimeout = 20 * time.Minute
	// tokenPlaceholder is substituted into the embedded page at serve time.
	tokenPlaceholder = "{{ECHOHEIST_TOKEN}}"
)

func runDashboard(addr string, fetcher *Fetcher, cache *Cache, config dashboardConfig) error {
	token, err := newDashboardToken()
	if err != nil {
		return err
	}
	s := &dashboardState{
		config:   config,
		fetcher:  fetcher,
		cache:    cache,
		jobs:     make(map[string]*dashboardJob),
		token:    token,
		jobLimit: maxDashboardJobs,
	}
	if config.WorkerPath != "" {
		if _, err := os.Stat(config.WorkerPath); err != nil {
			fmt.Printf("warning: download worker not found at %s: %v\n", config.WorkerPath, err)
		}
	}
	server := &http.Server{Addr: addr, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Printf("Echoheist dashboard %s running at http://%s\n", version, addr)
	fmt.Printf("Every /api request requires this run's token; open the URL above in a browser to pick it up.\n")
	return server.ListenAndServe()
}

// newDashboardToken returns the per-run secret that gates /api. Without it the
// dashboard was a drive-by target: any page the user visited could PUT
// /api/config with an attacker-chosen worker path and then POST /api/downloads
// to have that script executed locally.
func newDashboardToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate dashboard token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// routes wires the HTTP surface. No CORS headers are emitted (the dashboard is
// same-origin only) and every /api endpoint requires the run token.
func (s *dashboardState) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.frontend)
	mux.HandleFunc("/dashboard/", s.asset)
	guard := s.requireToken
	mux.HandleFunc("/api/health", guard(s.health))
	mux.HandleFunc("/api/config", guard(s.configHandler))
	mux.HandleFunc("/api/artists", guard(s.artists))
	mux.HandleFunc("/api/artists/", guard(s.tabs))
	mux.HandleFunc("/api/search", guard(s.search))
	mux.HandleFunc("/api/fetch/artists", guard(s.fetchArtists))
	mux.HandleFunc("/api/fetch/tabs", guard(s.fetchTabs))
	mux.HandleFunc("/api/downloads", guard(s.downloads))
	mux.HandleFunc("/api/downloads/", guard(s.downloadJob))
	return mux
}

func (s *dashboardState) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		supplied := r.Header.Get("X-Echoheist-Token")
		if supplied == "" {
			supplied = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(supplied), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, fmt.Errorf("missing or invalid dashboard token"))
			return
		}
		next(w, r)
	}
}

func (s *dashboardState) frontend(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := dashboardFiles.ReadFile("dashboard/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	page := strings.ReplaceAll(string(b), tokenPlaceholder, s.token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(page))
}

func (s *dashboardState) asset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	b, err := dashboardFiles.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(name, ".js") {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func decodeJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

func (s *dashboardState) health(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	jobs := len(s.jobs)
	s.mu.RUnlock()
	writeJSON(w, 200, map[string]any{"ok": true, "time": time.Now(), "jobs": jobs})
}
func (s *dashboardState) configHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == http.MethodPut {
		// The worker path is intentionally not settable over HTTP: it names the
		// script executed for every download, so allowing it to change at runtime
		// turns this endpoint into code execution. Set it with --worker instead.
		var in struct {
			DownloadDir string `json:"downloadDir"`
			LogDir      string `json:"logDir"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, 400, err)
			return
		}
		if in.DownloadDir != "" {
			dir, err := validateDirectory("downloadDir", in.DownloadDir)
			if err != nil {
				writeError(w, 400, err)
				return
			}
			s.config.DownloadDir = dir
		}
		if in.LogDir != "" {
			dir, err := validateDirectory("logDir", in.LogDir)
			if err != nil {
				writeError(w, 400, err)
				return
			}
			s.config.LogDir = dir
		}
	}
	writeJSON(w, 200, s.config)
}

// validateDirectory rejects unusable destination paths up front instead of
// letting the worker create whatever it was handed.
func validateDirectory(field, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s must not be empty", field)
	}
	if strings.ContainsRune(trimmed, 0) {
		return "", fmt.Errorf("%s contains a NUL byte", field)
	}
	return filepath.Clean(trimmed), nil
}

// pruneJobs bounds the job map so a long session cannot grow it without limit.
func (s *dashboardState) pruneJobs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) <= s.jobLimit {
		return
	}
	finished := make([]*dashboardJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		if job.EndedAt != nil {
			finished = append(finished, job)
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].EndedAt.Before(*finished[j].EndedAt) })
	for i := 0; i < len(finished) && len(s.jobs) > s.jobLimit; i++ {
		delete(s.jobs, finished[i].ID)
	}
}

func (s *dashboardState) artists(w http.ResponseWriter, r *http.Request) {
	letter := r.URL.Query().Get("letter")
	if letter != "" {
		xs, source, err := s.fetcher.Artists(r.Context(), letter)
		if err != nil {
			writeError(w, 502, err)
			return
		}
		_ = s.cache.SaveArtists(xs)
		writeJSON(w, 200, map[string]any{"items": xs, "source": source})
		return
	}
	xs, err := s.cache.Artists()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": xs, "source": "cache"})
}

func (s *dashboardState) tabs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/artists/"))
	if err != nil || id <= 0 {
		writeError(w, 400, fmt.Errorf("invalid artist id"))
		return
	}
	artists, _ := s.cache.Artists()
	var artist Artist
	for _, a := range artists {
		if a.ID == id {
			artist = a
			break
		}
	}
	if artist.ID == 0 {
		writeError(w, 404, fmt.Errorf("artist not found in cache; fetch the artist list first"))
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		xs, source, e := s.fetcher.Tabs(r.Context(), artist)
		if e != nil {
			writeError(w, 502, e)
			return
		}
		_ = s.cache.SaveTabs(xs)
		writeJSON(w, 200, map[string]any{"items": xs, "source": source})
		return
	}
	xs, e := s.cache.Tabs(id)
	if e != nil {
		writeError(w, 500, e)
		return
	}
	writeJSON(w, 200, map[string]any{"items": xs, "source": "cache"})
}

func (s *dashboardState) search(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	letter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("letter")))
	artists, _ := s.cache.Artists()
	result := make([]map[string]any, 0)
	artistMatches := make([]Artist, 0)
	for _, a := range artists {
		name := strings.ToLower(strings.TrimSpace(a.Name))
		if letter != "" {
			if letter == "0-9" {
				if name == "" || name[0] < '0' || name[0] > '9' {
					continue
				}
			} else if !strings.HasPrefix(name, letter) {
				continue
			}
		}
		if q == "" || strings.Contains(name, q) {
			artistMatches = append(artistMatches, a)
		}
		tabs, _ := s.cache.Tabs(a.ID)
		for _, t := range tabs {
			hay := strings.ToLower(strings.Join([]string{t.Song, t.Artist, t.Type, t.Author, t.Difficulty, t.Tuning}, " "))
			if q == "" || strings.Contains(hay, q) {
				result = append(result, map[string]any{"artist": a, "tab": t})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"items": result, "artists": artistMatches, "count": len(result), "artistCount": len(artistMatches), "source": "local-cache", "letter": letter})
}

func (s *dashboardState) fetchArtists(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Letter string `json:"letter"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Letter == "" {
		writeError(w, 400, fmt.Errorf("letter is required"))
		return
	}
	xs, source, err := s.fetcher.Artists(r.Context(), in.Letter)
	if err != nil {
		writeError(w, 502, err)
		return
	}
	if err = s.cache.SaveArtists(xs); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": xs, "source": source})
}
func (s *dashboardState) fetchTabs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ArtistID int `json:"artistId"`
	}
	if err := decodeJSON(r, &in); err != nil || in.ArtistID <= 0 {
		writeError(w, 400, fmt.Errorf("artistId is required"))
		return
	}
	rr := r.Clone(r.Context())
	rr.URL.RawQuery = "refresh=1"
	rr.URL.Path = "/api/artists/" + strconv.Itoa(in.ArtistID)
	s.tabs(w, rr)
}

func (s *dashboardState) downloads(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var in struct {
			Tabs []Tab `json:"tabs"`
		}
		if err := decodeJSON(r, &in); err != nil || len(in.Tabs) == 0 {
			writeError(w, 400, fmt.Errorf("tabs are required"))
			return
		}
		ids := make([]string, 0, len(in.Tabs))
		for _, tab := range in.Tabs {
			id := fmt.Sprintf("tab-%d-%d", tab.ID, time.Now().UnixNano())
			job := &dashboardJob{ID: id, Tab: tab, Status: "queued", StartedAt: time.Now()}
			s.mu.Lock()
			s.jobs[id] = job
			s.mu.Unlock()
			ids = append(ids, id)
			go s.runJob(job)
		}
		s.pruneJobs()
		writeJSON(w, 202, map[string]any{"ids": ids})
		return
	}
	s.mu.RLock()
	out := make([]*dashboardJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		clone := *job
		out = append(out, &clone)
	}
	s.mu.RUnlock()
	writeJSON(w, 200, map[string]any{"items": out})
}

func (s *dashboardState) downloadJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/downloads/")
	s.mu.RLock()
	job, ok := s.jobs[id]
	if ok {
		clone := *job
		job = &clone
	}
	s.mu.RUnlock()
	if !ok {
		writeError(w, 404, fmt.Errorf("job not found"))
		return
	}
	writeJSON(w, 200, job)
}

func (s *dashboardState) runJob(job *dashboardJob) {
	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()
	_ = os.MkdirAll(cfg.LogDir, 0755)
	_ = os.MkdirAll(cfg.DownloadDir, 0755)
	logPath := filepath.Join(cfg.LogDir, job.ID+".log")
	logFile, _ := os.Create(logPath)
	if logFile != nil {
		defer logFile.Close()
	}
	url := canonicalTabRecordURL(job.Tab)
	// A wedged Chromium must not pin a job forever; the deadline also terminates
	// the node child through CommandContext.
	ctx, cancel := context.WithTimeout(context.Background(), workerJobTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", cfg.WorkerPath, "--url", url, "--output-dir", cfg.DownloadDir, "--artist", job.Tab.Artist, "--song", job.Tab.Song, "--job-id", job.ID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.finishJob(job, "failed", err.Error(), "", 0)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.finishJob(job, "failed", err.Error(), "", 0)
		return
	}
	s.mu.Lock()
	job.Status = "running"
	s.mu.Unlock()
	if err = cmd.Start(); err != nil {
		s.finishJob(job, "failed", err.Error(), "", 0)
		return
	}
	var stdoutData, stderrData bytes.Buffer
	var streams sync.WaitGroup
	copyStream := func(rd io.Reader, dst *bytes.Buffer) {
		defer streams.Done()
		_, _ = io.Copy(dst, rd)
	}
	streams.Add(2)
	go copyStream(stdout, &stdoutData)
	go copyStream(stderr, &stderrData)
	waitErr := cmd.Wait()
	streams.Wait()
	if logFile != nil {
		_, _ = fmt.Fprintf(logFile, "[dashboard] worker-exit=%v\n", waitErr)
		if stderrData.Len() > 0 {
			_, _ = fmt.Fprintf(logFile, "[stderr]\n%s\n", stderrData.String())
		}
		_, _ = fmt.Fprintf(logFile, "[stdout]\n%s\n", stdoutData.String())
	}

	var saved *workerEvent
	var completed bool
	var workerError string
	scanner := bufio.NewScanner(bytes.NewReader(stdoutData.Bytes()))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event workerEvent
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		s.mu.Lock()
		job.Events = append(job.Events, event)
		s.mu.Unlock()
		switch event.Event {
		case "saved":
			copy := event
			saved = &copy
		case "completed":
			completed = true
		case "error":
			if event.Message != "" {
				workerError = event.Message
			}
		}
	}
	if waitErr == nil && (saved != nil || completed) {
		if saved != nil {
			s.finishJob(job, "completed", "download finished", saved.Path, saved.Bytes)
		} else {
			s.finishJob(job, "completed", "worker completed the download", "", 0)
		}
		return
	}
	message := workerError
	if ctx.Err() == context.DeadlineExceeded {
		message = fmt.Sprintf("worker exceeded the %s job timeout", workerJobTimeout)
	}
	if message == "" && stderrData.Len() > 0 {
		message = strings.TrimSpace(stderrData.String())
	}
	if message == "" && waitErr != nil {
		message = waitErr.Error()
	}
	if message == "" {
		message = "worker exited without a saved or completed event"
	}
	s.finishJob(job, "failed", message, "", 0)
}

func (s *dashboardState) finishJob(job *dashboardJob, status, message, path string, bytes int64) {
	now := time.Now()
	s.mu.Lock()
	job.Status, job.Message, job.Path, job.Bytes, job.EndedAt = status, message, path, bytes, &now
	s.mu.Unlock()
}
