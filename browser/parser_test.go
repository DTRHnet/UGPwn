package main

import (
	"html"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
)

func TestStateData(t *testing.T) {
	p := `<script>&quot;page&quot;:{&quot;data&quot;:{&quot;artists&quot;:[1],&quot;ok&quot;:true},&quot;x&quot;:1}</script>`
	d := stateData(html.UnescapeString(p))
	if d != "{\"artists\":[1],\"ok\":true}" {
		t.Fatalf("got %q", d)
	}
}

func TestShowDebugEventLevelThree(t *testing.T) {
	if showDebugEvent(workerEvent{Event: "trace", Phase: "request"}) {
		t.Fatal("routine network requests should be hidden")
	}
	for _, phase := range []string{"launch", "file-saved", "settled"} {
		if !showDebugEvent(workerEvent{Event: "trace", Phase: phase}) {
			t.Fatalf("important phase %q should be visible", phase)
		}
	}
	if showDebugEvent(workerEvent{Event: "trace", Phase: "request-failed", URL: "https://sync.example-ad-network.test/pixel"}) {
		t.Fatal("third-party request failures should be hidden")
	}
	for _, url := range []string{
		"https://tabs.ultimate-guitar.com/tab/example/song-guitar-pro-1",
		"https://cdn.example.test/download/public/song.gp5",
	} {
		if !showDebugEvent(workerEvent{Event: "trace", Phase: "request-failed", URL: url}) {
			t.Fatalf("download-critical request failure %q should be visible", url)
		}
	}
	for _, event := range []string{"started", "saved", "completed", "error"} {
		if !showDebugEvent(workerEvent{Event: event}) {
			t.Fatalf("lifecycle event %q should be visible", event)
		}
	}
}

func TestFormatWorkerEventIncludesPhase(t *testing.T) {
	got := formatWorkerEvent(workerEvent{Event: "trace", Phase: "file-saved", Path: "/tmp/song.gp5", Bytes: 42})
	want := "trace — file-saved — /tmp/song.gp5 — 42 B"
	if got != want {
		t.Fatalf("formatWorkerEvent() = %q, want %q", got, want)
	}
}
func TestGroupTabs(t *testing.T) {
	xs := []Tab{{ID: 1, ArtistID: 2, SongID: 3, Song: "Same"}, {ID: 2, ArtistID: 2, SongID: 3, Song: "Same"}, {ID: 4, ArtistID: 2, SongID: 9, Song: "Other"}}
	g := groupTabs(xs)
	if len(g) != 2 || len(g[1].Tabs) != 2 {
		t.Fatalf("groups=%#v", g)
	}
}

func TestTabObjects(t *testing.T) {
	data := `"other_tabs":[{"id":11,"song_name":"Fly On Your Wall","song_id":22,"artist_id":33,"artist_name":"Angel Olsen","type":"Pro","rating":4.5,"votes":2,"tab_url":"https://tabs.ultimate-guitar.com/tab/angel-olsen/fly-on-your-wall-guitar-pro-11","type_name":"Guitar Pro","tuning":"Standard"}],"feat_tabs"`
	tabs := tabObjects(data)
	if len(tabs) != 1 || tabs[0].ID != 11 || tabs[0].Song != "Fly On Your Wall" || tabs[0].Type != "Guitar Pro" {
		t.Fatalf("unexpected tabs: %#v", tabs)
	}
}

func TestTabObjectsDecodesJSONPayload(t *testing.T) {
	data := `{"other_tabs":[{"id":11,"song_name":"Fly On Your Wall","artist_name":"Angel Olsen","tab_url":"https://tabs.ultimate-guitar.com/tab/angel-olsen/fly-on-your-wall-guitar-pro-11","type_name":"Guitar Pro"}]}`
	tabs := tabObjects(data)
	if len(tabs) != 1 || tabs[0].ID != 11 {
		t.Fatalf("unexpected decoded tabs: %#v", tabs)
	}
}

func TestTabObjectsPreserveCanonicalSlugURL(t *testing.T) {
	data := `"other_tabs":[{"id":6590747,"song_name":"Lucid Daydream","artist_name":"Billy Strings","tab_url":"https://tabs.ultimate-guitar.com/tab/6590747","type_name":"Guitar Pro"}],"feat_tabs"`
	tabs := tabObjects(data)
	want := "https://tabs.ultimate-guitar.com/tab/billy-strings/lucid-daydream-guitar-pro-6590747"
	if len(tabs) != 1 || tabs[0].URL != want {
		t.Fatalf("unexpected canonical tab URL: %#v", tabs)
	}
}

func TestTabFileMetadata(t *testing.T) {
	tab := tabFromObject(`{"id":11,"song_name":"Fly On Your Wall","username":"justrutko1","fileExtension":".gp5","size":17351,"tab_created":1630381878}`)
	if tab.FileExtension != ".gp5" || tab.FileSize != 17351 || tab.Author != "justrutko1" || tab.Date.IsZero() {
		t.Fatalf("unexpected file metadata: %#v", tab)
	}
}

func TestTabObjectsRejectProLandingPage(t *testing.T) {
	data := `"other_tabs":[{"id":11,"song_name":"Song","tab_url":"https://www.ultimate-guitar.com/pro/?tab_id=11","type_name":"Guitar Pro"},{"id":11,"song_name":"Song","tab_url":"https://tabs.ultimate-guitar.com/tab/11","type_name":"Guitar Pro"}],"feat_tabs"`
	tabs := tabObjects(data)
	if len(tabs) != 1 || tabs[0].URL != "https://tabs.ultimate-guitar.com/tab/11" {
		t.Fatalf("unexpected tab URL: %#v", tabs)
	}
}

func TestTabObjectsRejectsRecordContainingProURL(t *testing.T) {
	data := `"other_tabs":[{"id":12,"song_name":"Song","tab_url":"https://www.ultimate-guitar.com/pro/?tab_id=12","download_url":"https://tabs.ultimate-guitar.com/tab/example/song-12","type_name":"Guitar Pro"}],"feat_tabs"`
	if tabs := tabObjects(data); len(tabs) != 0 {
		t.Fatalf("pro landing record should be excluded: %#v", tabs)
	}
}

func TestCanonicalTabURL(t *testing.T) {
	cases := map[string]string{
		"https://www.ultimate-guitar.com/pro/?tab_id=1794310": "https://tabs.ultimate-guitar.com/tab/1794310",
		"/pro/?tab_id=42": "https://tabs.ultimate-guitar.com/tab/42",
		"https://tabs.ultimate-guitar.com/tab/1794310?foo=bar": "https://tabs.ultimate-guitar.com/tab/1794310",
	}
	for input, want := range cases {
		if got := canonicalTabURL(input); got != want {
			t.Fatalf("canonicalTabURL(%q) = %q, want %q", input, got, want)
		}
	}
	if got := tabFromObject(`{"id":2529243,"song_name":"Hand In My Pocket","artist_name":"Alanis Morissette","type_name":"","tab_url":"https://tabs.ultimate-guitar.com/tab/2529243"}`).URL; got != "https://tabs.ultimate-guitar.com/tab/alanis-morissette/hand-in-my-pocket-2529243" {
		t.Fatalf("unexpected ID-only slug URL: %q", got)
	}
}

func TestMergeTabDoesNotReplaceFullSlugWithIDOnlyURL(t *testing.T) {
	dst := Tab{URL: "https://tabs.ultimate-guitar.com/tab/billy-strings/lucid-daydream-guitar-pro-6590747"}
	mergeTab(&dst, Tab{URL: "https://tabs.ultimate-guitar.com/tab/6590747"})
	if dst.URL != "https://tabs.ultimate-guitar.com/tab/billy-strings/lucid-daydream-guitar-pro-6590747" {
		t.Fatalf("full slug was replaced: %q", dst.URL)
	}
}

func TestIsTabsTabURLRejectsProAndIDOnlyRoutes(t *testing.T) {
	if isTabsTabURL("https://www.ultimate-guitar.com/pro/?tab_id=1") {
		t.Fatal("/pro/ URL must not be treated as a tab URL")
	}
	if isTabsTabURL("https://tabs.ultimate-guitar.com/tab/6590747") {
		t.Fatal("ID-only URL must not be treated as a valid canonical tab URL")
	}
	if !isTabsTabURL("https://tabs.ultimate-guitar.com/tab/billy-strings/lucid-daydream-guitar-pro-6590747") {
		t.Fatal("full slug URL should be treated as a valid tab URL")
	}
}

func TestArtistTabsURLPages(t *testing.T) {
	base := "https://www.ultimate-guitar.com/artist/led-zeppelin-123"
	if got := artistTabsURL(base, 1); got != base+"?filter=guitar_pro" {
		t.Fatalf("page 1 URL = %q", got)
	}
	if got := artistTabsURL(base+"?filter=tab", 3); got != base+"?filter=guitar_pro&page=3" {
		t.Fatalf("page 3 URL = %q", got)
	}
}

func TestArtistListURLUsesGuitarProFilter(t *testing.T) {
	base := "https://www.ultimate-guitar.com/bands/a.htm"
	if got := artistListURL(base, 1); got != base+"?filter=guitar_pro" {
		t.Fatalf("artist list URL = %q", got)
	}
}

func TestSongRowsExpandIntoTree(t *testing.T) {
	m := model{
		groups:   []SongGroup{{Song: "One", Tabs: []Tab{{ID: 1}, {ID: 2}}}, {Song: "Two", Tabs: []Tab{{ID: 3}}}},
		expanded: map[int]bool{0: true},
	}
	rows := m.songRows()
	if len(rows) != 4 || rows[0].child || rows[1].variant != 0 || !rows[2].child || rows[3].child {
		t.Fatalf("unexpected tree rows: %#v", rows)
	}
}

func TestFilteredArtistsAndGroupsSortAlphabetically(t *testing.T) {
	m := model{
		artists: []Artist{{Name: "Zebra"}, {Name: "alpha"}, {Name: "Bravo"}},
		groups:  []SongGroup{{Song: "Zoo"}, {Song: "apple"}, {Song: "Moon"}},
		query:   textinput.New(),
	}
	artists := m.filteredArtists()
	groups := m.filteredGroups()
	if artists[0].Name != "alpha" || artists[1].Name != "Bravo" || artists[2].Name != "Zebra" {
		t.Fatalf("artists not sorted: %#v", artists)
	}
	if groups[0].Song != "apple" || groups[1].Song != "Moon" || groups[2].Song != "Zoo" {
		t.Fatalf("groups not sorted: %#v", groups)
	}
}

func TestAlphabetWrapsForNarrowWidths(t *testing.T) {
	m := newModel(nil, nil)
	wide := m.alphabetLines(140)
	narrow := m.alphabetLines(40)
	if len(wide) >= len(narrow) {
		t.Fatalf("expected narrow alphabet to wrap: wide=%d narrow=%d", len(wide), len(narrow))
	}
}

func TestArtistPageSizeUsesQuadrantHeight(t *testing.T) {
	m := newModel(nil, nil)
	rows := m.artistRowsForTerminal(120, 40)
	if rows < 1 || rows >= 40 {
		t.Fatalf("unexpected artist page rows: %d", rows)
	}
	m.artistPageRows = rows
	if got := m.artistPageSize(); got != rows {
		t.Fatalf("artistPageSize() = %d, want cached resize value %d", got, rows)
	}
}
