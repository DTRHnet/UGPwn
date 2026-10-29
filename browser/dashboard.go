package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// dashboardFiles embeds the built frontend. The Vite build
// (`npm --prefix web run build`) writes browser/dashboard/dist; nothing outside
// dist is ever served.
//
//go:embed dashboard/dist
var dashboardFiles embed.FS

// dashboardDist is the filesystem root of the built frontend.
var dashboardDist = mustSubFS(dashboardFiles, "dashboard/dist")

func mustSubFS(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		// A missing dist directory is a build error, not a runtime condition.
		panic(fmt.Sprintf("embed %s: %v", dir, err))
	}
	return sub
}

type dashboardConfig struct {
	DownloadDir string `json:"downloadDir"`
	WorkerPath  string `json:"workerPath"`
	LogDir      string `json:"logDir"`
}

type dashboardState struct {
	mu       sync.RWMutex
	config   dashboardConfig
	fetcher  *Fetcher
	cache    *Cache
	jobs     map[string]*Job
	order    []string
	queue    chan *Job
	token    string
	jobLimit int
	logs     *logRing
	options  Options
	runner   jobRunner
}

const (
	// maxDashboardJobs bounds the live job map for long-running sessions.
	maxDashboardJobs = 200
	// workerJobTimeout is the fallback deadline when a job carries none.
	workerJobTimeout = 20 * time.Minute
	// tokenPlaceholder is substituted into index.html at serve time.
	tokenPlaceholder = "{{ECHOHEIST_TOKEN}}"
	// maxJobsPerRequest caps one POST /api/jobs call.
	maxJobsPerRequest = 100
	// maxHistoryLimit caps GET /api/history?limit=.
	maxHistoryLimit = 500
	// logTailLines is how many lines GET /api/logs/{name} returns.
	logTailLines = 500
)

func newDashboardState(cfg dashboardConfig, fetcher *Fetcher, cache *Cache, token string) *dashboardState {
	s := &dashboardState{
		config:   cfg,
		fetcher:  fetcher,
		cache:    cache,
		jobs:     make(map[string]*Job),
		queue:    make(chan *Job, jobQueueDepth),
		token:    token,
		jobLimit: maxDashboardJobs,
		logs:     newLogRing(maxLogLines),
		options:  defaultOptions(),
	}
	s.runner = s.runWorker
	// Restore the last-used options so a restart does not silently reset them.
	if cache != nil {
		if raw, err := cache.Setting(settingOptions); err == nil && strings.TrimSpace(raw) != "" {
			var stored Options
			if json.Unmarshal([]byte(raw), &stored) == nil {
				s.options = stored.normalize()
			}
		}
	}
	return s
}

func runDashboard(addr string, fetcher *Fetcher, cache *Cache, config dashboardConfig) error {
	token, err := newDashboardToken()
	if err != nil {
		return err
	}
	s := newDashboardState(config, fetcher, cache, token)
	if config.WorkerPath != "" {
		if _, statErr := os.Stat(config.WorkerPath); statErr != nil {
			fmt.Printf("warning: download worker not found at %s: %v\n", config.WorkerPath, statErr)
		}
	}
	s.startDispatcher()
	s.logf("", LevelInfo, "dashboard started")

	server := &http.Server{Addr: addr, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Printf("Echoheist dashboard %s running at http://%s\n", version, addr)
	fmt.Printf("Every /api request requires this run's token; open the URL above in a browser to pick it up.\n")
	return server.ListenAndServe()
}

// newDashboardToken returns the per-run secret that gates /api. Without it the
// dashboard is a drive-by target: any page the user visits could repoint the
// worker and have it executed locally.
func newDashboardToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate dashboard token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// routes wires the HTTP surface. No CORS headers are emitted (same-origin only)
// and every /api endpoint requires the run token.
func (s *dashboardState) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.frontend)
	mux.HandleFunc("/dashboard/", s.asset)

	guard := s.requireToken
	mux.HandleFunc("/api/state", guard(s.state))
	mux.HandleFunc("/api/jobs", guard(s.jobsHandler))
	mux.HandleFunc("/api/jobs/", guard(s.jobAction))
	mux.HandleFunc("/api/history", guard(s.history))
	mux.HandleFunc("/api/favorites", guard(s.favorites))
	mux.HandleFunc("/api/favorites/", guard(s.favoriteItem))
	mux.HandleFunc("/api/sources", guard(s.sourcesHandler))
	mux.HandleFunc("/api/logs", guard(s.logList))
	mux.HandleFunc("/api/logs/", guard(s.logTail))
	mux.HandleFunc("/api/config", guard(s.configHandler))
	mux.HandleFunc("/api/health", guard(s.health))
	mux.HandleFunc("/api/artists", guard(s.artists))
	mux.HandleFunc("/api/artists/", guard(s.tabs))
	mux.HandleFunc("/api/search", guard(s.search))
	mux.HandleFunc("/api/fetch/artists", guard(s.fetchArtists))
	mux.HandleFunc("/api/fetch/tabs", guard(s.fetchTabs))
	return mux
}

func (s *dashboardState) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		supplied := r.Header.Get("X-Echoheist-Token")
		if supplied == "" {
			supplied = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(supplied), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, errors.New("missing or invalid dashboard token"))
			return
		}
		next(w, r)
	}
}

// --- static frontend -------------------------------------------------------

// frontend serves the SPA shell with the run token injected. Any non-API GET
// without a file extension is treated as a client route and also gets the shell.
func (s *dashboardState) frontend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if ext := path.Ext(r.URL.Path); ext != "" && ext != ".html" {
		http.NotFound(w, r)
		return
	}
	page, err := fs.ReadFile(dashboardDist, "index.html")
	if err != nil {
		http.Error(w, "dashboard build missing — run `npm --prefix web run build`", http.StatusInternalServerError)
		return
	}
	body := strings.ReplaceAll(string(page), tokenPlaceholder, s.token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.WriteString(w, body)
}

// asset serves everything under /dashboard/ from the embedded build. Unknown
// paths fall back to the SPA shell so client routing works; missing files under
// assets/ are a real 404 so broken bundles are visible instead of silent.
func (s *dashboardState) asset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	name = strings.TrimPrefix(name, "dashboard/")
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		s.frontend(w, r)
		return
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		http.NotFound(w, r)
		return
	}
	if st, err := fs.Stat(dashboardDist, clean); err != nil || st.IsDir() {
		if strings.HasPrefix(clean, "assets/") {
			http.NotFound(w, r)
			return
		}
		s.frontend(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.StripPrefix("/dashboard/", http.FileServer(http.FS(dashboardDist))).ServeHTTP(w, r)
}

// --- helpers ---------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed; use %s", allow))
}

func decodeJSON(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errors.New("request body is empty")
	}
	return json.Unmarshal(body, dst)
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

// --- core endpoints --------------------------------------------------------

func (s *dashboardState) state(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.RLock()
	cfg, opts := s.config, s.options
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, State{
		Version: version,
		Config:  cfg,
		Jobs:    s.snapshotJobs(),
		Counts:  s.jobCounts(),
		Metrics: s.metrics(),
		Sources: defaultSources(),
		Options: opts,
	})
}

// metrics reads the persisted aggregate used by the footer. Failures degrade to
// zeros rather than inventing numbers.
func (s *dashboardState) metrics() Metrics {
	if s.cache == nil {
		return Metrics{}
	}
	m, err := s.cache.Metrics()
	if err != nil {
		s.logf("", LevelWarn, "could not read metrics: "+err.Error())
		return Metrics{}
	}
	return m
}

type jobsResponse struct {
	Jobs   []*Job    `json:"jobs"`
	Logs   []LogLine `json:"logs"`
	Cursor int64     `json:"cursor"`
}

func (s *dashboardState) jobsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		since := int64(0)
		if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, errors.New("since must be a non-negative integer"))
				return
			}
			since = n
		}
		// Seq restarts at 1 when the process does. A cursor left over from the
		// previous process is ahead of the new head, so replay from the start
		// instead of leaving the console permanently empty.
		if cursor := s.logs.cursor(); since > cursor {
			since = 0
		}
		writeJSON(w, http.StatusOK, jobsResponse{
			Jobs:   s.snapshotJobs(),
			Logs:   s.logs.since(since),
			Cursor: s.logs.cursor(),
		})
	case http.MethodPost:
		var in struct {
			Tabs    []Tab         `json:"tabs"`
			Options *optionsInput `json:"options"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if len(in.Tabs) == 0 {
			writeError(w, http.StatusBadRequest, errors.New("tabs are required"))
			return
		}
		if len(in.Tabs) > maxJobsPerRequest {
			writeError(w, http.StatusBadRequest, fmt.Errorf("at most %d tabs per request", maxJobsPerRequest))
			return
		}
		s.mu.RLock()
		base := s.options
		s.mu.RUnlock()
		opts := base
		if in.Options != nil {
			opts = in.Options.merge(base)
		}
		opts = opts.normalize()
		s.saveOptions(opts)

		ids := make([]string, 0, len(in.Tabs))
		for _, tab := range in.Tabs {
			job := s.newJob(tab, opts)
			s.enqueue(job)
			ids = append(ids, job.ID)
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"ids": ids})
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (s *dashboardState) jobAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, errors.New("job id is required"))
		return
	}
	if rest == "clear" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		removed := s.clearJobs()
		s.logf("", LevelInfo, fmt.Sprintf("cleared %d finished job(s)", removed))
		writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
		return
	}

	id, action, _ := strings.Cut(rest, "/")
	switch {
	case action == "retry":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		next, err := s.retryJob(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": next.ID})
	case action == "":
		if r.Method != http.MethodDelete {
			methodNotAllowed(w, http.MethodDelete)
			return
		}
		if !s.cancelJob(id) {
			// 404 means "no such job"; 409 means "it exists but is no longer
			// cancelable". Conflating them made a finished job look like a typo
			// in the caller's id.
			if s.jobExists(id) {
				writeError(w, http.StatusConflict, errors.New("job already finished"))
				return
			}
			writeError(w, http.StatusNotFound, errors.New("job not found"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown job action %q", action))
	}
}

// jobExists reports whether a job id is known either to the live registry or to
// persisted history. It lets a handler distinguish "unknown id" (404) from
// "known but no longer actionable" (409).
func (s *dashboardState) jobExists(id string) bool {
	s.mu.RLock()
	_, live := s.jobs[id]
	s.mu.RUnlock()
	if live {
		return true
	}
	if s.cache != nil {
		if job, err := s.cache.Job(id); err == nil && job != nil {
			return true
		}
	}
	return false
}

func (s *dashboardState) history(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, errors.New("limit must be a positive integer"))
			return
		}
		limit = n
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}
	items := make([]Job, 0)
	if s.cache != nil {
		stored, err := s.cache.JobHistory(limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items = stored
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *dashboardState) favorites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items := make([]Tab, 0)
		if s.cache != nil {
			stored, err := s.cache.Favorites()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			items = stored
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		var in struct {
			Tab Tab `json:"tab"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if in.Tab.ID <= 0 {
			writeError(w, http.StatusBadRequest, errors.New("tab.id is required"))
			return
		}
		if s.cache == nil {
			writeError(w, http.StatusInternalServerError, errors.New("cache is unavailable"))
			return
		}
		if err := s.cache.AddFavorite(in.Tab); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (s *dashboardState) favoriteItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/favorites/"), "/")
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("favorite id must be a positive integer"))
		return
	}
	if s.cache == nil {
		writeError(w, http.StatusInternalServerError, errors.New("cache is unavailable"))
		return
	}
	if err := s.cache.RemoveFavorite(id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *dashboardState) sourcesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": defaultSources()})
}

// logFileInfo is one entry of GET /api/logs.
type logFileInfo struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

func (s *dashboardState) logList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.RLock()
	dir := s.config.LogDir
	s.mu.RUnlock()

	files := make([]logFileInfo, 0)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, logFileInfo{
			Name:     entry.Name(),
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// logTail returns the last lines of one job log. The name is resolved strictly
// inside the configured log directory: cleaning removes any ".." before the
// containment check, and path separators are refused outright.
func (s *dashboardState) logTail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	raw := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	name := strings.TrimPrefix(path.Clean("/"+raw), "/")
	if name == "" || name == "." || strings.ContainsAny(name, `/\`) {
		writeError(w, http.StatusBadRequest, errors.New("invalid log name"))
		return
	}

	s.mu.RLock()
	dir := s.config.LogDir
	s.mu.RUnlock()

	absDir, err := filepath.Abs(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	absFile, err := filepath.Abs(filepath.Join(dir, name))
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid log name"))
		return
	}
	if absFile != absDir && !strings.HasPrefix(absFile, absDir+string(os.PathSeparator)) {
		writeError(w, http.StatusBadRequest, errors.New("invalid log name"))
		return
	}

	lines, err := tailFile(absFile, logTailLines)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, fmt.Errorf("log %q not found", name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "lines": lines})
}

// tailFile returns the last n lines of a file, reading only its tail.
func tailFile(name string, n int) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", name)
	}
	const chunk = 256 << 10
	offset := info.Size() - chunk
	if offset < 0 {
		offset = 0
	}
	buf := make([]byte, info.Size()-offset)
	if len(buf) > 0 {
		if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	}
	text := strings.TrimRight(string(buf), "\r\n")
	if text == "" {
		return []string{}, nil
	}
	lines := strings.Split(text, "\n")
	if offset > 0 && len(lines) > 1 {
		// The first line of a partial tail chunk is almost certainly cut.
		lines = lines[1:]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

func (s *dashboardState) health(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	jobs := len(s.jobs)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "time": time.Now(), "jobs": jobs})
}

func (s *dashboardState) configHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		cfg := s.config
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		// The worker path is intentionally not settable over HTTP: it names the
		// script executed for every download, so allowing it to change at runtime
		// turns this endpoint into code execution. Set it with --worker instead.
		var in struct {
			DownloadDir string `json:"downloadDir"`
			LogDir      string `json:"logDir"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if in.DownloadDir != "" {
			dir, err := validateDirectory("downloadDir", in.DownloadDir)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			s.config.DownloadDir = dir
		}
		if in.LogDir != "" {
			dir, err := validateDirectory("logDir", in.LogDir)
			if err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			s.config.LogDir = dir
		}
		writeJSON(w, http.StatusOK, s.config)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPut)
	}
}

// saveOptions records options as the server-side default and persists them.
func (s *dashboardState) saveOptions(opts Options) {
	s.mu.Lock()
	s.options = opts
	s.mu.Unlock()
	if s.cache == nil {
		return
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		return
	}
	if err := s.cache.SaveSetting(settingOptions, string(raw)); err != nil {
		s.logf("", LevelWarn, "could not persist options: "+err.Error())
	}
}

// pruneJobs bounds the live job map so a long session cannot grow it without
// limit, dropping the oldest finished jobs first.
func (s *dashboardState) pruneJobs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) <= s.jobLimit {
		return
	}
	finished := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		if job.EndedAt != nil {
			finished = append(finished, job)
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].EndedAt.Before(*finished[j].EndedAt) })
	for i := 0; i < len(finished) && len(s.jobs) > s.jobLimit; i++ {
		delete(s.jobs, finished[i].ID)
	}
	kept := make([]string, 0, len(s.order))
	for _, id := range s.order {
		if _, ok := s.jobs[id]; ok {
			kept = append(kept, id)
		}
	}
	s.order = kept
}

// --- catalog endpoints (unchanged response shapes) -------------------------

func (s *dashboardState) artists(w http.ResponseWriter, r *http.Request) {
	letter := r.URL.Query().Get("letter")
	if letter != "" {
		xs, source, err := s.fetcher.Artists(r.Context(), letter)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		_ = s.cache.SaveArtists(xs)
		writeJSON(w, http.StatusOK, map[string]any{"items": xs, "source": source})
		return
	}
	xs, err := s.cache.Artists()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": xs, "source": "cache"})
}

func (s *dashboardState) tabs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/artists/"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid artist id"))
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
		writeError(w, http.StatusNotFound, errors.New("artist not found in cache; fetch the artist list first"))
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		xs, source, e := s.fetcher.Tabs(r.Context(), artist)
		if e != nil {
			writeError(w, http.StatusBadGateway, e)
			return
		}
		_ = s.cache.SaveTabs(xs)
		writeJSON(w, http.StatusOK, map[string]any{"items": xs, "source": source})
		return
	}
	xs, e := s.cache.Tabs(id)
	if e != nil {
		writeError(w, http.StatusInternalServerError, e)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": xs, "source": "cache"})
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
	writeJSON(w, http.StatusOK, map[string]any{"items": result, "artists": artistMatches, "count": len(result), "artistCount": len(artistMatches), "source": "local-cache", "letter": letter})
}

func (s *dashboardState) fetchArtists(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Letter string `json:"letter"`
	}
	if err := decodeJSON(r, &in); err != nil || in.Letter == "" {
		writeError(w, http.StatusBadRequest, errors.New("letter is required"))
		return
	}
	xs, source, err := s.fetcher.Artists(r.Context(), in.Letter)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	if err = s.cache.SaveArtists(xs); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": xs, "source": source})
}

func (s *dashboardState) fetchTabs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ArtistID int `json:"artistId"`
	}
	if err := decodeJSON(r, &in); err != nil || in.ArtistID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("artistId is required"))
		return
	}
	rr := r.Clone(r.Context())
	rr.URL.RawQuery = "refresh=1"
	rr.URL.Path = "/api/artists/" + strconv.Itoa(in.ArtistID)
	s.tabs(w, rr)
}
