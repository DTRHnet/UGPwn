package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// settingOptions is the settings key holding the last-used Options JSON.
const settingOptions = "options"

type Cache struct{ db *sql.DB }

func OpenCache() (*Cache, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "echoheist-guitar-pro")
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return OpenCacheAt(filepath.Join(dir, "cache.db"))
}

// OpenMemoryCache opens a throwaway in-memory cache. It keeps every call site
// working when the on-disk database is unavailable: browsing and search still
// function, nothing is persisted. The single-connection pool set in OpenCacheAt
// is what keeps the schema alive, since each SQLite connection would otherwise
// get its own private in-memory database.
func OpenMemoryCache() (*Cache, error) {
	return OpenCacheAt(":memory:")
}

// openCacheWithFallback returns the on-disk cache when it can be opened and an
// in-memory cache otherwise, reporting the degradation instead of aborting. A
// read-only $HOME (common in sandboxes and containers) used to make the whole
// process — including the dashboard — refuse to start.
func openCacheWithFallback() (*Cache, error) {
	c, err := OpenCache()
	if err == nil {
		return c, nil
	}
	memory, memoryErr := OpenMemoryCache()
	if memoryErr != nil {
		return nil, fmt.Errorf("cache: %v (in-memory fallback also failed: %w)", err, memoryErr)
	}
	fmt.Fprintf(os.Stderr, "warning: cache unavailable (%v); running with in-memory storage, nothing will be saved\n", err)
	return memory, nil
}

// OpenCacheAt opens (and creates) the cache at an explicit path. Tests use a
// temporary file; the TUI and dashboard use the platform cache directory.
func OpenCacheAt(path string) (*Cache, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection serialises writes inside this process; busy_timeout covers
	// the case where the TUI and the dashboard share the same database file.
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}

	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS artists (id INTEGER PRIMARY KEY, name TEXT NOT NULL, url TEXT NOT NULL, tab_count INTEGER NOT NULL, fetched_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS tabs (id INTEGER PRIMARY KEY, song_id INTEGER, artist_id INTEGER, song TEXT, artist TEXT, type TEXT, url TEXT, author TEXT, file_extension TEXT, file_size INTEGER, rating REAL, votes INTEGER, difficulty TEXT, tuning TEXT, tab_key TEXT, description TEXT, date TEXT, date_posted TEXT, views INTEGER, favorites INTEGER, fetched_at INTEGER NOT NULL)`,
		// Completed/failed/canceled history for the History view.
		`CREATE TABLE IF NOT EXISTS jobs (id TEXT PRIMARY KEY, tab TEXT NOT NULL, status TEXT NOT NULL, path TEXT NOT NULL DEFAULT '', bytes INTEGER NOT NULL DEFAULT 0, message TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, ended_at TEXT NOT NULL DEFAULT '', options TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS favorites (tab_id INTEGER PRIMARY KEY, tab TEXT NOT NULL, added_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}

	// Keep existing user caches usable after adding tab file metadata. These are
	// the pre-existing legacy columns; a repeated ALTER fails harmlessly.
	for _, q := range []string{
		`ALTER TABLE tabs ADD COLUMN file_extension TEXT`,
		`ALTER TABLE tabs ADD COLUMN file_size INTEGER`,
		`ALTER TABLE tabs ADD COLUMN date_posted TEXT`,
	} {
		_, _ = db.Exec(q)
	}

	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Cache{db: db}, nil
}

func (c *Cache) Close() {
	if c != nil && c.db != nil {
		c.db.Close()
	}
}

func (c *Cache) SaveArtists(xs []Artist) error {
	tx, e := c.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, a := range xs {
		if _, e = tx.Exec(`INSERT INTO artists(id,name,url,tab_count,fetched_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,url=excluded.url,tab_count=excluded.tab_count,fetched_at=excluded.fetched_at`, a.ID, a.Name, a.URL, a.TabCount, time.Now().Unix()); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (c *Cache) Artists() ([]Artist, error) {
	rows, e := c.db.Query(`SELECT id,name,url,tab_count FROM artists ORDER BY lower(name)`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Artist
	for rows.Next() {
		var a Artist
		if e = rows.Scan(&a.ID, &a.Name, &a.URL, &a.TabCount); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (c *Cache) SaveTabs(xs []Tab) error {
	tx, e := c.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, t := range xs {
		_, e = tx.Exec(`INSERT INTO tabs(id,song_id,artist_id,song,artist,type,url,author,file_extension,file_size,rating,votes,difficulty,tuning,tab_key,description,date,date_posted,views,favorites,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET song_id=excluded.song_id,artist_id=excluded.artist_id,song=excluded.song,artist=excluded.artist,type=excluded.type,url=excluded.url,author=excluded.author,file_extension=excluded.file_extension,file_size=excluded.file_size,rating=excluded.rating,votes=excluded.votes,difficulty=excluded.difficulty,tuning=excluded.tuning,tab_key=excluded.tab_key,description=excluded.description,date=excluded.date,date_posted=excluded.date_posted,views=excluded.views,favorites=excluded.favorites,fetched_at=excluded.fetched_at`, t.ID, t.SongID, t.ArtistID, t.Song, t.Artist, t.Type, t.URL, t.Author, t.FileExtension, t.FileSize, t.Rating, t.Votes, t.Difficulty, t.Tuning, t.Key, t.Description, t.Date.Format(time.RFC3339), t.Date.Format(time.RFC3339), t.Views, t.Favorites, time.Now().Unix())
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (c *Cache) Tabs(artistID int) ([]Tab, error) {
	rows, e := c.db.Query(`SELECT id,song_id,artist_id,song,artist,type,url,author,file_extension,file_size,rating,votes,difficulty,tuning,tab_key,description,date,date_posted,views,favorites FROM tabs WHERE artist_id=? ORDER BY lower(song), rating DESC`, artistID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Tab
	for rows.Next() {
		var t Tab
		var ds, posted string
		if e = rows.Scan(&t.ID, &t.SongID, &t.ArtistID, &t.Song, &t.Artist, &t.Type, &t.URL, &t.Author, &t.FileExtension, &t.FileSize, &t.Rating, &t.Votes, &t.Difficulty, &t.Tuning, &t.Key, &t.Description, &ds, &posted, &t.Views, &t.Favorites); e != nil {
			return nil, e
		}
		t.Date, _ = time.Parse(time.RFC3339, posted)
		if t.Date.IsZero() {
			t.Date, _ = time.Parse(time.RFC3339, ds)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- job history -----------------------------------------------------------

// sqlTimeLayout is a fixed-width RFC3339 form. Fixed width matters because
// history is ordered by the stored text: RFC3339Nano drops trailing zeros, so
// "…43Z" sorts after "…43.5Z" and rows would come back out of order.
const sqlTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(sqlTimeLayout)
}

func parseTimeText(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{sqlTimeLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// SaveJob upserts one finished job into the persisted history.
func (c *Cache) SaveJob(job *Job) error {
	if job == nil {
		return errors.New("nil job")
	}
	tabJSON, err := json.Marshal(job.Tab)
	if err != nil {
		return err
	}
	optionsJSON, err := json.Marshal(job.Options)
	if err != nil {
		return err
	}
	ended := ""
	if job.EndedAt != nil {
		ended = timeText(*job.EndedAt)
	}
	_, err = c.db.Exec(`INSERT INTO jobs(id,tab,status,path,bytes,message,started_at,ended_at,options) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET tab=excluded.tab,status=excluded.status,path=excluded.path,bytes=excluded.bytes,message=excluded.message,started_at=excluded.started_at,ended_at=excluded.ended_at,options=excluded.options`,
		job.ID, string(tabJSON), job.Status, job.Path, job.Bytes, job.Message, timeText(job.StartedAt), ended, string(optionsJSON))
	return err
}

// scanJobRow decodes one persisted row.
func scanJobRow(scan func(dest ...any) error) (Job, error) {
	var (
		job         Job
		tabJSON     string
		optionsJSON string
		startedAt   string
		endedAt     string
	)
	if err := scan(&job.ID, &tabJSON, &job.Status, &job.Path, &job.Bytes, &job.Message, &startedAt, &endedAt, &optionsJSON); err != nil {
		return Job{}, err
	}
	if err := json.Unmarshal([]byte(tabJSON), &job.Tab); err != nil {
		// A row written by an older build must not break history loading.
		job.Tab = Tab{}
	}
	if optionsJSON != "" {
		var opts Options
		if json.Unmarshal([]byte(optionsJSON), &opts) == nil {
			job.Options = opts.normalize()
		}
	}
	if t, ok := parseTimeText(startedAt); ok {
		job.StartedAt = t
	}
	if t, ok := parseTimeText(endedAt); ok {
		end := t
		job.EndedAt = &end
	}
	return job, nil
}

const jobColumns = `id,tab,status,path,bytes,message,started_at,ended_at,options`

// JobHistory returns persisted jobs, newest first.
func (c *Cache) JobHistory(limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := c.db.Query(`SELECT `+jobColumns+` FROM jobs ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Job, 0, limit)
	for rows.Next() {
		job, err := scanJobRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// Job returns one persisted job, or nil when it does not exist.
func (c *Cache) Job(id string) (*Job, error) {
	row := c.db.QueryRow(`SELECT `+jobColumns+` FROM jobs WHERE id=?`, id)
	job, err := scanJobRow(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// Metrics aggregates the footer numbers from persisted history. Bytes is a total,
// never a rate.
func (c *Cache) Metrics() (Metrics, error) {
	var m Metrics
	if err := c.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(bytes),0) FROM jobs WHERE status=?`, StatusCompleted).Scan(&m.TotalDownloads, &m.Bytes); err != nil {
		return Metrics{}, err
	}
	var finished int
	if err := c.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE status IN (?,?)`, StatusCompleted, StatusFailed).Scan(&finished); err != nil {
		return Metrics{}, err
	}
	if finished > 0 {
		m.SuccessRate = float64(m.TotalDownloads) / float64(finished)
	}
	return m, nil
}

// --- favorites -------------------------------------------------------------

// AddFavorite upserts a tab. Re-favoriting keeps the original added_at.
func (c *Cache) AddFavorite(tab Tab) error {
	tabJSON, err := json.Marshal(tab)
	if err != nil {
		return err
	}
	_, err = c.db.Exec(`INSERT INTO favorites(tab_id,tab,added_at) VALUES(?,?,?)
		ON CONFLICT(tab_id) DO UPDATE SET tab=excluded.tab`,
		tab.ID, string(tabJSON), timeText(time.Now()))
	return err
}

func (c *Cache) RemoveFavorite(id int) error {
	_, err := c.db.Exec(`DELETE FROM favorites WHERE tab_id=?`, id)
	return err
}

func (c *Cache) Favorites() ([]Tab, error) {
	rows, err := c.db.Query(`SELECT tab FROM favorites ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Tab, 0)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var tab Tab
		if json.Unmarshal([]byte(raw), &tab) == nil {
			out = append(out, tab)
		}
	}
	return out, rows.Err()
}

// --- settings --------------------------------------------------------------

func (c *Cache) SaveSetting(key, value string) error {
	_, err := c.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// Setting returns "" (and no error) when the key has never been stored.
func (c *Cache) Setting(key string) (string, error) {
	var value string
	err := c.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}
