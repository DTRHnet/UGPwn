package main

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

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
	db, err := sql.Open("sqlite", filepath.Join(dir, "cache.db"))
	if err != nil {
		return nil, err
	}
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS artists (id INTEGER PRIMARY KEY, name TEXT NOT NULL, url TEXT NOT NULL, tab_count INTEGER NOT NULL, fetched_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS tabs (id INTEGER PRIMARY KEY, song_id INTEGER, artist_id INTEGER, song TEXT, artist TEXT, type TEXT, url TEXT, author TEXT, file_extension TEXT, file_size INTEGER, rating REAL, votes INTEGER, difficulty TEXT, tuning TEXT, tab_key TEXT, description TEXT, date TEXT, date_posted TEXT, views INTEGER, favorites INTEGER, fetched_at INTEGER NOT NULL)`,
	} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	// Keep existing user caches usable after adding file metadata.
	for _, q := range []string{
		`ALTER TABLE tabs ADD COLUMN file_extension TEXT`,
		`ALTER TABLE tabs ADD COLUMN file_size INTEGER`,
		`ALTER TABLE tabs ADD COLUMN date_posted TEXT`,
	} {
		_, _ = db.Exec(q)
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
