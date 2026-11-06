package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	letters screen = iota
	artistsScreen
	songsScreen
)

type pane int

const (
	topPane pane = iota
	artistsPane
	songsPane
)

type loadedArtists struct {
	xs     []Artist
	source string
	err    error
}
type loadedTabs struct {
	xs     []Tab
	source string
	err    error
}
type loadedDetail struct {
	t   Tab
	err error
}

type songRow struct {
	group, variant int
	child          bool
	label          string
}

type model struct {
	fetch            *Fetcher
	cache            *Cache
	view             screen
	focus            pane
	letters          []string
	alphaSelected    int
	artists          []Artist
	groups           []SongGroup
	selected         int
	artistPage       int
	artistPageRows   int
	query            textinput.Model
	querying         bool
	expanded         map[int]bool
	status           string
	width, height    int
	currentArtist    Artist
	currentLetter    string
	currentTab       Tab
	downloadDir      string
	workerPath       string
	downloadQueue    []Tab
	downloadIndex    int
	downloadDone     int
	downloadBusy     bool
	downloadPrompt   bool
	downloadState    map[int]string
	downloadCtx      context.Context
	downloadCancel   context.CancelFunc
	downloadCanceled bool
	debugLog         []string
	debugLogging     bool
	logDir           string
	debugFile        *os.File
	debugPath        string
}

var (
	accentStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	cyanStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	greenStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	yellowStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	valueStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))
	urlStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	descriptionStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	mutedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	redStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	selectedRowStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Background(lipgloss.Color("24"))
	panelStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	focusedPanelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("39")).Padding(0, 1)
)

func newModel(f *Fetcher, c *Cache) model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 100
	in.Width = 80
	return model{fetch: f, cache: c, view: letters, focus: topPane, letters: append([]string{"0-9"}, strings.Split("ABCDEFGHIJKLMNOPQRSTUVWXYZ", "")...), query: in, expanded: make(map[int]bool), downloadDir: "./tabs", downloadState: make(map[int]string), workerPath: "download-worker.mjs", logDir: "./logs", status: "↑↓ navigate  enter select  / search  v/space queue  w observe  L logging  q quit"}
}

func (m model) Init() tea.Cmd { return nil }

func loadArtists(f *Fetcher, c *Cache, letter string) tea.Cmd {
	return func() tea.Msg {
		xs, u, e := f.Artists(context.Background(), letter)
		if e == nil {
			e = c.SaveArtists(xs)
		}
		return loadedArtists{xs, u, e}
	}
}
func loadTabs(f *Fetcher, c *Cache, a Artist) tea.Cmd {
	return func() tea.Msg {
		xs, u, e := f.Tabs(context.Background(), a)
		if e != nil {
			xs, _ = c.Tabs(a.ID)
			xs = f.filterValidTabs(context.Background(), xs)
		} else {
			_ = c.SaveTabs(xs)
		}
		return loadedTabs{xs, u, e}
	}
}
func loadDetail(f *Fetcher, t Tab) tea.Cmd {
	return func() tea.Msg { x, e := f.Detail(context.Background(), t); return loadedDetail{x, e} }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = x.Width, x.Height
		m.query.Width = maxInt(20, x.Width/2)
		m.artistPageRows = m.artistRowsForTerminal(x.Width, x.Height)
		m.syncArtistPage()
	case loadedArtists:
		m.status = "loaded " + x.source
		if x.err != nil {
			m.status = "offline/cache: " + x.err.Error()
			if cached, e := m.cache.Artists(); e == nil {
				m.artists = cached
			}
		} else {
			m.artists = x.xs
		}
		m.view, m.focus, m.selected, m.artistPage = artistsScreen, artistsPane, 0, 0
		m.query.Reset()
	case loadedTabs:
		m.status = "loaded " + x.source
		if x.err != nil {
			m.status = "cached/error: " + x.err.Error()
		}
		m.groups, m.expanded = groupTabs(x.xs), make(map[int]bool)
		m.view, m.focus, m.selected = songsScreen, songsPane, 0
		m.query.Reset()
	case loadedDetail:
		if x.err == nil {
			m.currentTab = x.t
		} else {
			m.status = "detail: " + x.err.Error()
		}
	case downloadResult:
		m.downloadBusy = false
		if x.Err != nil {
			m.debugLog = append(m.debugLog, "download-result: ERROR: "+x.Err.Error())
		} else {
			m.debugLog = append(m.debugLog, fmt.Sprintf("download-result: completed path=%s bytes=%d report=%s", x.Path, x.Bytes, x.Report))
		}
		m.downloadDone++
		failed := x.Err != nil
		if m.downloadCanceled {
			m.downloadState[x.Tab.ID] = "canceled"
			m.status = "download queue canceled"
			m.downloadQueue = nil
			m.downloadCancel = nil
			m.closeDebugLog()
			return m, nil
		}
		if x.Err != nil {
			m.downloadState[x.Tab.ID] = "failed"
			m.status = fmt.Sprintf("check failed: %s", x.Err)
		} else if x.Path == "" {
			// Observe-only build: the worker reports metadata and never a file,
			// so an empty Path is the expected shape of a success.
			m.downloadState[x.Tab.ID] = "observed"
			m.status = fmt.Sprintf("observed %s · %s (download disabled)", x.Observation, formatBytes(x.Bytes))
		} else {
			m.downloadState[x.Tab.ID] = "done"
			m.status = fmt.Sprintf("saved %s (%s)", x.Path, formatBytes(x.Bytes))
		}
		if m.downloadIndex < len(m.downloadQueue) {
			return m, m.startNextDownload()
		}
		if !failed {
			m.status = fmt.Sprintf("queue complete: %d/%d observed", m.downloadDone, len(m.downloadQueue))
		}
		m.downloadCancel = nil
		m.downloadCtx = nil
		m.downloadQueue = nil
		m.closeDebugLog()
	case downloadProgress:
		line := x.line
		if x.event.Event != "" {
			if !showDebugEvent(x.event) {
				return m, readDownloadOutput(x.session)
			}
			line = formatWorkerEvent(x.event)
		}
		if line != "" {
			prefix := "worker"
			if x.stream == "stderr" {
				prefix = "browser"
			}
			m.debugLog = append(m.debugLog, prefix+": "+line)
			if len(m.debugLog) > 1000 {
				m.debugLog = m.debugLog[len(m.debugLog)-1000:]
			}
		}
		return m, readDownloadOutput(x.session)
	case tea.KeyMsg:
		return m.handleKey(x)
	}
	return m, nil
}

// showDebugEvent keeps the on-screen debug pane at approximately level 3:
// lifecycle, download, error, and key browser milestones remain visible while
// the lossless log file continues to receive every trace event.
func showDebugEvent(event workerEvent) bool {
	if event.Event == "progress" {
		// High-frequency tick (up to ~5/s): the pane stays useful only if these
		// are dropped. The log file still records everything.
		return false
	}
	if event.Event != "trace" {
		return true
	}
	switch event.Phase {
	case "arguments", "launch", "browser-launched", "download-behavior",
		"goto-start", "goto-complete", "filesystem-download-wait",
		"filesystem-download-result", "fetch-fallback-start", "fetch-fallback-response",
		"file-saved", "settled", "cleanup", "page-console":
		return true
	case "request-failed":
		// Most failed requests are ad, analytics, or cookie-sync traffic.
		// Keep only failures that could affect the actual tab/download flow.
		return event.URL == "" || strings.Contains(event.URL, "ultimate-guitar.com") || strings.Contains(event.URL, "/download/public/")
	default:
		return false
	}
}

func (m model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.querying {
		switch key.String() {
		case "esc":
			m.querying = false
			m.downloadPrompt = false
			m.query.Blur()
			return m, nil
		case "enter":
			if m.downloadPrompt {
				m.downloadDir = strings.TrimSpace(m.query.Value())
				m.downloadPrompt = false
				m.querying = false
				m.query.Blur()
				m.query.Reset()
				if m.downloadDir == "" {
					m.status = "download directory cleared"
				} else {
					m.status = "download directory: " + m.downloadDir
				}
				return m, nil
			}
			m.querying = false
			m.query.Blur()
			m.selected, m.artistPage = 0, 0
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.query, cmd = m.query.Update(key)
		m.selected, m.artistPage = 0, 0
		return m, cmd
	}
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "d":
		m.downloadPrompt = true
		m.querying = true
		m.query.SetValue(m.downloadDir)
		m.query.Focus()
		return m, nil
	case "v":
		return m, m.toggleDownloadSelection()
	case "w":
		return m, m.beginDownloads()
	case "L":
		m.debugLogging = !m.debugLogging
		if m.debugLogging {
			m.status = "debug logging enabled for the next download run"
		} else {
			m.status = "debug logging disabled"
		}
		return m, nil
	case "x":
		if m.downloadBusy {
			if m.downloadCancel != nil {
				m.downloadCanceled = true
				m.downloadCancel()
				m.status = "canceling active download"
			}
		} else {
			m.downloadQueue = nil
			m.downloadState = make(map[int]string)
			m.status = "download queue cleared"
		}
		return m, nil
	case "/":
		m.querying = true
		m.focus = artistsPane
		m.query.Focus()
		return m, nil
	case "tab":
		m.focus = (m.focus + 1) % 3
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
	case "up", "k":
		if m.focus == topPane {
			m.alphaMove(-1)
		} else {
			m.move(-1)
		}
	case "down", "j":
		if m.focus == topPane {
			m.alphaMove(1)
		} else {
			m.move(1)
		}
	case "home", "g":
		m.selected = 0
	case "end", "G":
		m.selected = maxInt(0, m.itemCount()-1)
	case "ctrl+u":
		m.move(-m.visibleRows() / 2)
	case "ctrl+d":
		m.move(m.visibleRows() / 2)
	case "left", "h", "esc":
		if m.focus == topPane {
			m.alphaMove(-1)
		} else if m.view == artistsScreen {
			m.view, m.focus, m.selected = letters, topPane, 0
		} else if m.view == songsScreen {
			m.view, m.focus, m.selected = artistsScreen, artistsPane, 0
		}
	case "right", "l", "enter":
		if m.focus == topPane && key.String() == "right" {
			m.alphaMove(1)
			return m, nil
		}
		return m.activate()
	case " ", "space": // bubbletea v1 reports KeySpace as " ", not "space"
		if m.view == songsScreen {
			rows := m.songRows()
			if m.selected < len(rows) && rows[m.selected].child {
				return m, m.toggleDownloadSelection()
			}
			m.toggleSelectedGroup()
		}
	case "r":
		return m.refresh()
	case "y":
		return m.copySelected()
	case "a", "A":
		if m.view == artistsScreen {
			m.artistPage--
			m.syncArtistPage()
			return m, nil
		}
	case "D":
		if m.view == artistsScreen {
			m.artistPage++
			m.syncArtistPage()
			return m, nil
		}
	}
	return m, nil
}

func (m *model) move(delta int) {
	n := m.itemCount()
	if n == 0 {
		m.selected = 0
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= n {
		m.selected = n - 1
	}
	if m.view == artistsScreen {
		m.syncArtistPage()
	}
}

func (m *model) alphaMove(delta int) {
	if len(m.letters) == 0 {
		return
	}
	m.alphaSelected += delta
	if m.alphaSelected < 0 {
		m.alphaSelected = 0
	}
	if m.alphaSelected >= len(m.letters) {
		m.alphaSelected = len(m.letters) - 1
	}
}

func (m *model) syncArtistPage() {
	size := m.artistPageSize()
	if size < 1 {
		size = 1
	}
	m.artistPage = m.selected / size
	if pages := m.artistPageCount(); pages > 0 && m.artistPage >= pages {
		m.artistPage = pages - 1
	}
}

func (m model) artistPageSize() int {
	if m.artistPageRows > 0 {
		return m.artistPageRows
	}
	return m.artistRowsForTerminal(m.width, m.height)
}
func (m model) artistRowsForTerminal(width, height int) int {
	if width < 80 {
		width = 100
	}
	if height < 12 {
		height = 12
	}
	alphaHeight := len(m.alphabetLines(width)) + 2
	const topPadding = 3
	mainHeight := maxInt(2, height-alphaHeight-topPadding-7)
	quadrantHeight := maxInt(1, mainHeight/2)
	// renderArtists receives the quadrant's inner height. It uses five rows
	// for its title, controls, spacing, and scroll hint.
	return maxInt(1, quadrantHeight-7)
}
func (m model) artistPageCount() int {
	n := len(m.filteredArtists())
	size := m.artistPageSize()
	if n == 0 {
		return 0
	}
	return (n + size - 1) / size
}
func (m *model) pageMove(delta int) {
	pages := m.artistPageCount()
	if pages == 0 {
		m.artistPage = 0
		m.selected = 0
		return
	}
	m.artistPage += delta
	if m.artistPage < 0 {
		m.artistPage = 0
	}
	if m.artistPage >= pages {
		m.artistPage = pages - 1
	}
	m.selected = m.artistPage * m.artistPageSize()
	if m.selected >= len(m.filteredArtists()) {
		m.selected = len(m.filteredArtists()) - 1
	}
}

// variantTab resolves a row to its tab, guarding the group and variant indexes
// so a stale row can never panic the TUI.
func (m model) variantTab(row songRow) (Tab, bool) {
	if row.group < 0 || row.group >= len(m.groups) {
		return Tab{}, false
	}
	tabs := m.groups[row.group].Tabs
	if row.variant < 0 || row.variant >= len(tabs) {
		return Tab{}, false
	}
	return tabs[row.variant], true
}

// alphaLetter returns the highlighted alphabet entry, clamping a stale
// selection instead of indexing out of range.
func (m model) alphaLetter() string {
	if len(m.letters) == 0 {
		return ""
	}
	if m.alphaSelected < 0 || m.alphaSelected >= len(m.letters) {
		return m.letters[0]
	}
	return m.letters[m.alphaSelected]
}

func (m model) activate() (tea.Model, tea.Cmd) {
	if m.focus == topPane {
		if letter := m.alphaLetter(); letter != "" {
			m.currentLetter = letter
			return m, loadArtists(m.fetch, m.cache, m.currentLetter)
		}
	}
	switch m.view {
	case letters:
		if letter := m.alphaLetter(); letter != "" {
			m.currentLetter = letter
			return m, loadArtists(m.fetch, m.cache, m.currentLetter)
		}
	case artistsScreen:
		xs := m.filteredArtists()
		if len(xs) == 0 || m.selected < 0 || m.selected >= len(xs) {
			return m, nil
		}
		m.currentArtist = xs[m.selected]
		return m, loadTabs(m.fetch, m.cache, m.currentArtist)
	case songsScreen:
		rows := m.songRows()
		if m.selected < 0 || m.selected >= len(rows) {
			return m, nil
		}
		row := rows[m.selected]
		if !row.child {
			m.toggleGroup(row.group)
			return m, nil
		}
		if tab, ok := m.variantTab(row); ok {
			m.currentTab = tab
			return m, loadDetail(m.fetch, m.currentTab)
		}
	}
	return m, nil
}

func (m *model) toggleSelectedGroup() {
	rows := m.songRows()
	if m.selected < len(rows) {
		m.toggleGroup(rows[m.selected].group)
	}
}
func (m *model) toggleGroup(group int) {
	if group < 0 || group >= len(m.groups) {
		return
	}
	tabs := m.groups[group].Tabs
	if len(tabs) == 0 {
		return
	}
	if len(tabs) == 1 {
		m.currentTab = tabs[0]
		return
	}
	m.expanded[group] = !m.expanded[group]
}
func (m model) refresh() (tea.Model, tea.Cmd) {
	if m.view == artistsScreen {
		return m, loadArtists(m.fetch, m.cache, m.currentLetter)
	}
	if m.view == songsScreen {
		return m, loadTabs(m.fetch, m.cache, m.currentArtist)
	}
	return m, nil
}
func (m model) copySelected() (tea.Model, tea.Cmd) {
	u := m.currentTab.URL
	if u == "" && m.view == songsScreen {
		rows := m.songRows()
		if m.selected >= 0 && m.selected < len(rows) {
			if row := rows[m.selected]; row.group >= 0 && row.group < len(m.groups) {
				if tabs := m.groups[row.group].Tabs; len(tabs) > 0 {
					u = tabs[0].URL
				}
			}
		}
	}
	if u == "" {
		m.status = "no URL selected"
		return m, nil
	}
	if err := copyClipboard(u); err != nil {
		m.status = "URL: " + u
	} else {
		m.status = "copied: " + u
	}
	return m, nil
}

func (m *model) toggleDownloadSelection() tea.Cmd {
	if m.view != songsScreen {
		m.status = "open a song list before queueing downloads"
		return nil
	}
	rows := m.songRows()
	if m.selected < 0 || m.selected >= len(rows) {
		m.status = "expand a song and highlight a variant first"
		return nil
	}
	row := rows[m.selected]
	if row.group < 0 || row.group >= len(m.groups) {
		m.status = "stale selection; reopen the song list"
		return nil
	}
	group := m.groups[row.group]
	if len(group.Tabs) == 0 {
		m.status = "this song has no Guitar Pro variants"
		return nil
	}
	if !row.child && len(group.Tabs) != 1 {
		m.status = "expand a song and highlight a variant first"
		return nil
	}
	variant := row.variant
	if !row.child {
		variant = 0
	}
	if variant < 0 || variant >= len(group.Tabs) {
		m.status = "stale selection; reopen the song list"
		return nil
	}
	tab := group.Tabs[variant]
	for i, queued := range m.downloadQueue {
		if queued.ID == tab.ID {
			m.downloadQueue = append(m.downloadQueue[:i], m.downloadQueue[i+1:]...)
			delete(m.downloadState, tab.ID)
			m.status = fmt.Sprintf("removed %s from queue", tab.Song)
			return nil
		}
	}
	m.downloadQueue = append(m.downloadQueue, tab)
	m.downloadState[tab.ID] = "queued"
	m.status = fmt.Sprintf("queued %s (%d total)", tab.Song, len(m.downloadQueue))
	return nil
}

func (m *model) beginDownloads() tea.Cmd {
	if m.downloadBusy {
		m.status = "a download is already running"
		return nil
	}
	if len(m.downloadQueue) == 0 {
		m.status = "download queue is empty; press v on a Guitar Pro variant"
		return nil
	}
	if strings.TrimSpace(m.downloadDir) == "" {
		m.status = "set a download directory with d first"
		return nil
	}
	m.downloadIndex, m.downloadDone, m.downloadBusy = 0, 0, true
	m.debugLog = []string{"starting download queue"}
	if m.debugLogging {
		if err := os.MkdirAll(m.logDir, 0o755); err != nil {
			m.downloadBusy = false
			m.status = fmt.Sprintf("cannot create log directory: %s", err)
			return nil
		}
		name := fmt.Sprintf("run-%s-%d.log", time.Now().Format("20060102-150405.000000000"), os.Getpid())
		file, err := os.OpenFile(filepath.Join(m.logDir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			m.downloadBusy = false
			m.status = fmt.Sprintf("cannot open debug log: %s", err)
			return nil
		}
		m.debugFile, m.debugPath = file, filepath.Join(m.logDir, name)
		m.debugLog = append(m.debugLog, "logging to "+m.debugPath)
	}
	m.downloadCtx, m.downloadCancel = context.WithCancel(context.Background())
	m.downloadCanceled = false
	return m.startNextDownload()
}

func (m *model) startNextDownload() tea.Cmd {
	if m.downloadIndex >= len(m.downloadQueue) {
		m.downloadBusy = false
		return nil
	}
	tab := m.downloadQueue[m.downloadIndex]
	m.downloadIndex++
	m.downloadState[tab.ID] = "running"
	m.status = fmt.Sprintf("downloading %d/%d: %s", m.downloadIndex, len(m.downloadQueue), tab.Song)
	return runDownload(m.downloadCtx, tab, m.downloadDir, m.workerPath, m.debugFile)
}

func (m *model) closeDebugLog() {
	if m.debugFile != nil {
		_ = m.debugFile.Close()
		m.debugFile = nil
	}
}

func (m model) filteredArtists() []Artist {
	q := strings.ToLower(strings.TrimSpace(m.query.Value()))
	out := make([]Artist, 0, len(m.artists))
	for _, a := range m.artists {
		if q == "" || fuzzyMatch(a.Name, q) {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if left == right {
			return out[i].Name < out[j].Name
		}
		return left < right
	})
	return out
}

// filteredGroupIndexes returns indexes into m.groups for the groups matching
// the active query, sorted by song name.
func (m model) filteredGroupIndexes() []int {
	q := strings.ToLower(strings.TrimSpace(m.query.Value()))
	out := make([]int, 0, len(m.groups))
	for i, g := range m.groups {
		if q == "" || fuzzyMatch(g.Song, q) {
			out = append(out, i)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		left, right := strings.ToLower(m.groups[out[a]].Song), strings.ToLower(m.groups[out[b]].Song)
		if left == right {
			return m.groups[out[a]].Song < m.groups[out[b]].Song
		}
		return left < right
	})
	return out
}

func (m model) filteredGroups() []SongGroup {
	idx := m.filteredGroupIndexes()
	out := make([]SongGroup, 0, len(idx))
	for _, i := range idx {
		out = append(out, m.groups[i])
	}
	return out
}

// songRows flattens the filtered groups into display rows. Each row carries an
// index into the unfiltered m.groups slice — every consumer (render, activate,
// queue toggles, preview) indexes m.groups with it. Numbering rows by their
// position in the filtered slice instead made the UI queue and inspect a
// different song than the one on screen whenever a query was active.
func (m model) songRows() []songRow {
	idx := m.filteredGroupIndexes()
	rows := make([]songRow, 0, len(idx))
	for _, group := range idx {
		g := m.groups[group]
		rows = append(rows, songRow{group: group, label: g.Song})
		if m.expanded[group] {
			for j := range g.Tabs {
				rows = append(rows, songRow{group: group, variant: j, child: true, label: variantLabel(g.Tabs[j])})
			}
		}
	}
	return rows
}
func variantLabel(t Tab) string {
	rating := "—"
	if t.Rating > 0 {
		rating = fmt.Sprintf("%.1f", t.Rating)
	}
	return fmt.Sprintf("%s  %s  ★ %s", blank(t.Type), blank(t.Author), rating)
}
func (m model) itemCount() int {
	switch m.view {
	case letters:
		return len(m.letters)
	case artistsScreen:
		return len(m.filteredArtists())
	case songsScreen:
		return len(m.songRows())
	}
	return 0
}
func (m model) visibleRows() int { return maxInt(1, m.height-11) }
func listWindow(selected, total, visible int) (int, int) {
	if total <= visible {
		return 0, total
	}
	start := selected - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (m model) View() string {
	width, height := m.width, m.height
	if width < 80 {
		width = 100
	}
	if height < 12 {
		height = 12
	}
	top := m.renderAlphabet(width)
	query := m.renderQuery(width)
	alphaHeight := len(m.alphabetLines(width)) + 2
	// Account for the header, query line, footer, separators, and the
	// intentional top breathing room. The old budget undercounted these rows,
	// causing the quadrant grid to render past the bottom of short terminals.
	const topPadding = 3
	mainHeight := maxInt(2, height-alphaHeight-topPadding-7)
	quadrantWidth := maxInt(30, (width-2)/2)
	quadrantHeight := maxInt(1, mainHeight/2)
	artistPanelStyle := panelStyle
	if m.focus == artistsPane {
		artistPanelStyle = focusedPanelStyle
	}
	songsPanelStyle := panelStyle
	if m.focus == songsPane {
		songsPanelStyle = focusedPanelStyle
	}
	artistPanel := artistPanelStyle.Width(quadrantWidth).Height(quadrantHeight).Render(m.renderArtists(quadrantHeight - 2))
	metadataPanel := panelStyle.Width(quadrantWidth).Height(quadrantHeight).Render(m.renderMetadata(quadrantWidth-4, quadrantHeight-2))
	songsPanel := songsPanelStyle.Width(quadrantWidth).Height(quadrantHeight).Render(m.renderSongs(quadrantHeight - 2))
	debugPanel := panelStyle.Width(quadrantWidth).Height(quadrantHeight).Render(m.renderDebug(quadrantWidth-4, quadrantHeight-2))
	body := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, artistPanel, metadataPanel),
		lipgloss.JoinHorizontal(lipgloss.Top, songsPanel, debugPanel),
	)
	return strings.Repeat("\n", topPadding) + m.renderHeader(width) + "\n" + top + "\n" + query + "\n" + body + "\n" + m.renderFooter(width)
}

func (m model) renderDebug(width, height int) string {
	var b strings.Builder
	b.WriteString(accentStyle.Render("DEBUG LOG") + "  " + mutedStyle.Render("[L] toggle "+debugLoggingLabel(m.debugLogging)) + "\n")
	if len(m.debugLog) == 0 {
		b.WriteString(mutedStyle.Render("waiting for worker output…"))
		return b.String()
	}
	lines := make([]string, 0, len(m.debugLog))
	for _, entry := range m.debugLog {
		lines = append(lines, wrap(entry, maxInt(20, width)))
	}
	joined := strings.Join(lines, "\n")
	all := strings.Split(joined, "\n")
	if height > 2 && len(all) > height-2 {
		all = all[len(all)-(height-2):]
	}
	b.WriteString(strings.Join(all, "\n"))
	return b.String()
}

func debugLoggingLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func (m model) renderHeader(width int) string {
	brand := accentStyle.Render("UGBrowser v0.1.0")
	account := mutedStyle.Render("admin@dtrh.net")
	hint := ""
	if width >= 110 {
		hint = mutedStyle.Render("↑↓ move  •  / search  •  Tab panes  •  A/D pages  •  Enter select")
	}
	used := lipgloss.Width(brand) + lipgloss.Width(account)
	if hint != "" {
		used += lipgloss.Width(hint)
	}
	gap := maxInt(2, width-used-4)
	if hint == "" {
		return brand + strings.Repeat(" ", gap) + account
	}
	leftGap := maxInt(2, gap/2)
	rightGap := maxInt(2, gap-leftGap)
	return brand + strings.Repeat(" ", leftGap) + hint + strings.Repeat(" ", rightGap) + account
}

func (m model) renderAlphabet(width int) string {
	return panelStyle.Width(maxInt(20, width-4)).Render(strings.Join(m.alphabetLines(width), "\n"))
}

func (m model) alphabetLines(width int) []string {
	boxWidth := maxInt(20, width-4)
	innerWidth := maxInt(10, boxWidth-4)
	var rows [][]int
	var current []int
	currentWidth := 0
	for i, letter := range m.letters {
		tokenWidth := utf8.RuneCountInString(letter) + 2
		if len(current) > 0 && currentWidth+1+tokenWidth > innerWidth {
			rows = append(rows, current)
			current = nil
			currentWidth = 0
		}
		current = append(current, i)
		currentWidth += tokenWidth
		if len(current) > 1 {
			currentWidth++
		}
	}
	if len(current) > 0 {
		rows = append(rows, current)
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		parts := make([]string, 0, len(row))
		for _, i := range row {
			label := " " + m.letters[i] + " "
			if i == m.alphaSelected {
				parts = append(parts, selectedRowStyle.Render(label))
			} else {
				parts = append(parts, cyanStyle.Render(label))
			}
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	return lines
}

func (m model) renderQuery(width int) string {
	if m.downloadPrompt {
		return accentStyle.Render(" download directory: ") + m.query.View() + "  " + mutedStyle.Render("Enter save  Esc cancel")
	}
	label := cyanStyle.Render(" / ")
	if m.querying {
		label = accentStyle.Render(" / ")
	}
	value := m.query.View()
	if !m.querying && m.query.Value() == "" {
		value = mutedStyle.Render("type to fuzzy-filter the active list")
	}
	return label + value + "  " + mutedStyle.Render(fmt.Sprintf("%d matches", m.itemCount()))
}
func (m model) renderArtists(visible int) string {
	var b strings.Builder
	items := m.filteredArtists()
	pageSize := maxInt(1, minInt(m.artistPageSize(), maxInt(1, visible-5)))
	start := m.artistPage * pageSize
	if start > len(items) {
		start = 0
	}
	end := minInt(len(items), start+pageSize)
	pageText := "page 0/0"
	if len(items) > 0 {
		pageText = fmt.Sprintf("page %d/%d", m.artistPage+1, m.artistPageCount())
	}
	b.WriteString(accentStyle.Render("ARTISTS") + "  " + mutedStyle.Render(m.currentLetter) + "  " + yellowStyle.Render(pageText) + "\n")
	b.WriteString(mutedStyle.Render("A/D page  •  Tab focus  •  Enter load") + "\n\n")
	for i := start; i < end; i++ {
		name := items[i].Name
		if visibleWidth(name) > 30 {
			name = string([]rune(name)[:30])
		}
		text := fmt.Sprintf(" %-30s\t%s", name, mutedStyle.Render(fmt.Sprintf("%d GP", items[i].TabCount)))
		b.WriteString(m.row(text, m.view == artistsScreen && i == m.selected) + "\n")
	}
	if len(items) == 0 {
		b.WriteString(mutedStyle.Render("no artists match the query"))
	}
	b.WriteString("\n" + scrollHint(start, end, len(items)))
	return b.String()
}

func (m model) renderSongs(visible int) string {
	var b strings.Builder
	rows := m.songRows()
	b.WriteString(accentStyle.Render("DOWNLOAD QUEUE") + "\n")
	b.WriteString(mutedStyle.Render("Directory: ") + valueStyle.Render(blank(m.downloadDir)) + "\n")
	b.WriteString(m.downloadSummary() + "\n")
	b.WriteString(yellowStyle.Render("[SPACE/V] queue") + "  " + greenStyle.Render("[W] start") + "  " + cyanStyle.Render("[D] folder") + "  " + mutedStyle.Render("[X] cancel/clear") + "\n\n")
	b.WriteString(accentStyle.Render("SONGS / TABS") + "  " + mutedStyle.Render(m.currentArtist.Name) + "\n")
	b.WriteString(mutedStyle.Render("+ expand  •  Enter inspect  •  y copy") + "\n\n")
	visible = maxInt(1, visible-6)
	start, end := listWindow(m.selected, len(rows), visible)
	for i := start; i < end; i++ {
		r := rows[i]
		g := m.groups[r.group]
		text := ""
		if !r.child {
			marker := "+"
			if m.expanded[r.group] {
				marker = "−"
			}
			text = fmt.Sprintf(" %s\t%s\t%s", yellowStyle.Render(marker), g.Song, greenStyle.Render(fmt.Sprintf("%d", len(g.Tabs))))
		} else {
			state := " "
			if s := m.downloadState[g.Tabs[r.variant].ID]; s == "queued" {
				state = "◇"
			} else if s == "running" {
				state = "…"
			} else if s == "done" {
				state = "✓"
			} else if s == "failed" {
				state = "!"
			}
			text = fmt.Sprintf(" %s └─ %s", state, r.label)
		}
		b.WriteString(m.row(text, m.view == songsScreen && i == m.selected) + "\n")
	}
	b.WriteString(scrollHint(start, end, len(rows)))
	return b.String()
}

func (m model) downloadSummary() string {
	queued, running, done, failed, canceled := 0, 0, 0, 0, 0
	for _, state := range m.downloadState {
		switch state {
		case "queued":
			queued++
		case "running":
			running++
		case "done":
			done++
		case "failed":
			failed++
		case "canceled":
			canceled++
		}
	}
	if m.downloadBusy {
		return greenStyle.Render(fmt.Sprintf("RUNNING  %d queued  %d/%d complete", queued, done, len(m.downloadQueue)))
	}
	if queued == 0 && done == 0 && failed == 0 && canceled == 0 {
		return mutedStyle.Render("No files selected")
	}
	return fmt.Sprintf("%s  %s  %s  %s  %s", yellowStyle.Render(fmt.Sprintf("QUEUED %d", queued)), greenStyle.Render(fmt.Sprintf("DONE %d", done)), redStyle.Render(fmt.Sprintf("FAILED %d", failed)), mutedStyle.Render(fmt.Sprintf("CANCELED %d", canceled)), mutedStyle.Render(fmt.Sprintf("TOTAL %d", queued+done+failed+canceled)))
}

func (m model) renderMetadata(width, height int) string {
	var b strings.Builder
	b.WriteString(accentStyle.Render("METADATA") + "  " + mutedStyle.Render("highlighted item") + "\n")
	t := m.previewTab()
	if t.Song == "" {
		b.WriteString("\n" + mutedStyle.Render("Select a song or variant to inspect its metadata."))
		return b.String()
	}
	b.WriteString(cyanStyle.Render(t.Song) + "  " + mutedStyle.Render("• "+t.Artist) + "\n\n")
	b.WriteString(detailLine("TYPE", blank(t.Type)))
	b.WriteString(detailLine("AUTHOR", blank(t.Author)))
	b.WriteString(detailLine("FILE", blank(t.FileExtension)))
	b.WriteString(detailLine("SIZE", formatBytes(t.FileSize)))
	b.WriteString(detailLine("POSTED", formatDate(t.Date)))
	b.WriteString(detailLine("RATING", ratingText(t)))
	b.WriteString(detailLine("DIFFICULTY", blank(t.Difficulty)))
	b.WriteString(detailLine("TUNING", blank(t.Tuning)))
	b.WriteString(detailLine("KEY", blank(t.Key)))
	b.WriteString(detailLine("ID", fmt.Sprintf("%d", t.ID)))
	if t.Description != "" && height > 8 {
		b.WriteString("\n" + yellowStyle.Render("DESCRIPTION") + "\n" + descriptionStyle.Render(wrap(t.Description, maxInt(20, width))) + "\n")
	}
	if height > 8 {
		b.WriteString("\n" + yellowStyle.Render("CANONICAL URL") + "\n" + urlStyle.Render(wrap(t.URL, maxInt(20, width))))
	}
	return b.String()
}
func (m model) previewTab() Tab {
	if m.view == songsScreen {
		rows := m.songRows()
		if m.selected >= 0 && m.selected < len(rows) {
			if tab, ok := m.variantTab(rows[m.selected]); ok {
				return tab
			}
		}
	}
	return m.currentTab
}
func (m model) renderFooter(width int) string {
	left := cyanStyle.Render("↑↓") + " navigate  " + cyanStyle.Render("enter") + " expand/select  " + cyanStyle.Render("/") + " search  " + cyanStyle.Render("y") + " copy  " + cyanStyle.Render("L") + " logging  " + cyanStyle.Render("q") + " quit"
	if width < 90 {
		left = cyanStyle.Render("↑↓") + " nav  " + cyanStyle.Render("enter") + " select  " + cyanStyle.Render("/") + " search  " + cyanStyle.Render("q") + " quit"
	}
	return left + "\n" + mutedStyle.Render(m.status)
}
func (m model) row(text string, selected bool) string {
	if selected {
		return selectedRowStyle.Render(text)
	}
	return text
}
func detailLine(label, value string) string {
	return mutedStyle.Render(fmt.Sprintf("%-11s", label)) + " " + valueStyle.Render(value) + "\n"
}
func ratingText(t Tab) string {
	if t.Rating <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f  (%d votes)", t.Rating, t.Votes)
}
func formatDate(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}
func formatBytes(size int64) string {
	if size <= 0 {
		return "—"
	}
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(size)/(1024*1024))
}
func scrollHint(start, end, total int) string {
	if total == 0 {
		return mutedStyle.Render("no matches")
	}
	if start > 0 && end < total {
		return mutedStyle.Render("↑ more  •  ↓ more")
	}
	if start > 0 {
		return mutedStyle.Render("↑ more")
	}
	if end < total {
		return mutedStyle.Render("↓ more")
	}
	return ""
}
func visibleWidth(s string) int { return utf8.RuneCountInString(s) }
func wrap(s string, width int) string {
	if width < 10 {
		width = 10
	}
	var out []string
	for len(s) > width {
		cut := strings.LastIndexByte(s[:width], ' ')
		if cut < 1 {
			cut = width
		}
		out = append(out, s[:cut])
		s = strings.TrimSpace(s[cut:])
	}
	out = append(out, s)
	return strings.Join(out, "\n")
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fuzzyMatch(value, query string) bool {
	if query == "" {
		return true
	}
	value, query = strings.ToLower(value), strings.ToLower(query)
	vi := 0
	for _, want := range query {
		found := false
		for vi < len(value) {
			r, size := utf8.DecodeRuneInString(value[vi:])
			vi += size
			if r == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func groupTabs(xs []Tab) []SongGroup {
	mm := map[string]*SongGroup{}
	for _, t := range xs {
		k := fmt.Sprintf("%d:%d", t.ArtistID, t.SongID)
		if t.SongID == 0 {
			k = strings.ToLower(t.Artist + "|" + t.Song)
		}
		if mm[k] == nil {
			mm[k] = &SongGroup{ArtistID: t.ArtistID, SongID: t.SongID, Song: t.Song, Artist: t.Artist}
		}
		mm[k].Tabs = append(mm[k].Tabs, t)
	}
	out := make([]SongGroup, 0, len(mm))
	for _, g := range mm {
		sort.Slice(g.Tabs, func(i, j int) bool { return g.Tabs[i].Rating > g.Tabs[j].Rating })
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Song) < strings.ToLower(out[j].Song) })
	return out
}
func blank(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func copyClipboard(s string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	default:
		cmd = exec.Command("sh", "-c", "command -v wl-copy >/dev/null && wl-copy || xclip -selection clipboard")
	}
	in, e := cmd.StdinPipe()
	if e != nil {
		return e
	}
	if e = cmd.Start(); e != nil {
		return e
	}
	_, e = in.Write([]byte(s))
	in.Close()
	if e != nil {
		return e
	}
	return cmd.Wait()
}
