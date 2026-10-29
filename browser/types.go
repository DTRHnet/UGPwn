package main

import "time"

type Artist struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	TabCount int    `json:"tabCount"`
}

type Tab struct {
	ID            int       `json:"id"`
	SongID        int       `json:"songId"`
	ArtistID      int       `json:"artistId"`
	Song          string    `json:"song"`
	Artist        string    `json:"artist"`
	Type          string    `json:"type"`
	URL           string    `json:"url"`
	Author        string    `json:"author"`
	FileExtension string    `json:"fileExtension"`
	FileSize      int64     `json:"fileSize"`
	Rating        float64   `json:"rating"`
	Votes         int       `json:"votes"`
	Difficulty    string    `json:"difficulty"`
	Tuning        string    `json:"tuning"`
	Key           string    `json:"key"`
	Description   string    `json:"description"`
	Date          time.Time `json:"date"`
	Views         int       `json:"views"`
	Favorites     int       `json:"favorites"`
}

type SongGroup struct {
	ArtistID int    `json:"artistId"`
	SongID   int    `json:"songId"`
	Song     string `json:"song"`
	Artist   string `json:"artist"`
	Tabs     []Tab  `json:"tabs"`
}
