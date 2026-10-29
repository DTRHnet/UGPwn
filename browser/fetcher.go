package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const baseURL = "https://www.ultimate-guitar.com"

var (
	artistRe     = regexp.MustCompile(`"id"\s*:\s*(\d+),"name"\s*:\s*"([^"]*)","tabscount"\s*:\s*(\d+)(?:,"(?:guitarprocount|guitar_pro_count|guitar_pro_tabs|protabscount)"\s*:\s*(\d+))?,"artist_url"\s*:\s*"([^"]*)"`)
	intFieldRe   = func(name string) *regexp.Regexp { return regexp.MustCompile(`"` + name + `"\s*:\s*(\d+)`) }
	strFieldRe   = func(name string) *regexp.Regexp { return regexp.MustCompile(`"` + name + `"\s*:\s*"((?:\\.|[^"])*)"`) }
	floatFieldRe = func(name string) *regexp.Regexp {
		return regexp.MustCompile(`"` + name + `"\s*:\s*(-?[0-9]+(?:\.[0-9]+)?)`)
	}
)

type Fetcher struct {
	client *http.Client
}

func NewFetcher() *Fetcher {
	return &Fetcher{client: &http.Client{Timeout: 25 * time.Second}}
}

func (f *Fetcher) get(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "echoheist-guitar-pro-tui/0.1 (metadata browser)")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return "", err
	}
	return html.UnescapeString(string(b)), nil
}

func stateData(page string) string {
	marker := `"page":{"data":`
	i := strings.Index(page, marker)
	if i < 0 {
		return ""
	}
	start := i + len(marker)
	if start >= len(page) || page[start] != '{' {
		return ""
	}
	depth, quoted, escaped := 0, false, false
	for i := start; i < len(page); i++ {
		c := page[i]
		if quoted {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return page[start : i+1]
			}
		}
	}
	return ""
}

func (f *Fetcher) Artists(ctx context.Context, letter string) ([]Artist, string, error) {
	path := strings.ToLower(letter) + ".htm"
	if letter == "0-9" {
		path = "0-92.htm"
	}
	url := baseURL + "/bands/" + path
	pages := []string{artistListURL(url, 1)}
	if letter != "0-9" {
		pages = append(pages, artistListURL(baseURL+"/bands/"+strings.ToLower(letter)+"2.htm", 1))
	}
	artists := make([]Artist, 0)
	seen := map[int]bool{}
	for pageIndex, pageURL := range pages {
		p, err := f.get(ctx, pageURL)
		if err != nil {
			if pageIndex == 0 {
				return nil, url, err
			}
			continue
		}
		data := stateData(p)
		if data == "" {
			if pageIndex == 0 {
				return nil, url, fmt.Errorf("artist-list state not found")
			}
			continue
		}
		for _, m := range artistRe.FindAllStringSubmatch(data, -1) {
			id := atoi(m[1])
			if seen[id] {
				continue
			}
			seen[id] = true
			count := atoi(m[3])
			if m[4] != "" {
				count = atoi(m[4])
			}
			u := m[5]
			if strings.HasPrefix(u, "/") {
				u = baseURL + u
			}
			artists = append(artists, Artist{ID: id, Name: m[2], TabCount: count, URL: u})
		}
	}
	return artists, url, nil
}

func artistListURL(rawURL string, page int) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set("filter", "guitar_pro")
	if page > 1 {
		q.Set("page", fmt.Sprintf("%d", page))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func field(s, name string) string {
	m := strFieldRe(name).FindStringSubmatch(s)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}
func number(s, name string) int {
	m := intFieldRe(name).FindStringSubmatch(s)
	if len(m) == 2 {
		return atoi(m[1])
	}
	return 0
}
func decimal(s, name string) float64 {
	m := floatFieldRe(name).FindStringSubmatch(s)
	if len(m) == 2 {
		var v float64
		fmt.Sscan(m[1], &v)
		return v
	}
	return 0
}

func tabFromObject(s string) Tab {
	// Prefer the full slug URL if the record contains one. Some Ultimate
	// Guitar payloads expose a short `/tab/<id>` URL in `tab_url` while the
	// canonical slug is present elsewhere in the same object.
	u := field(s, "tab_url")
	for _, candidate := range regexp.MustCompile(`https://tabs\.ultimate-guitar\.com/tab/[^"&]+`).FindAllString(s, -1) {
		if strings.Count(strings.TrimPrefix(candidate, "https://tabs.ultimate-guitar.com/tab/"), "/") > 0 {
			u = candidate
			break
		}
	}
	u = canonicalTabURL(u)
	// Current Ultimate Guitar artist data often includes a Pro landing URL
	// followed by the actual tab endpoint in the same record. The landing page
	// is not downloadable; prefer the tabs host when both are present.
	if strings.Contains(u, "/pro/") {
		if tabURL := regexp.MustCompile(`https://tabs\.ultimate-guitar\.com/tab/[^"&]+`).FindString(s); tabURL != "" {
			u = tabURL
		} else if parsed, err := url.Parse(u); err == nil && parsed.Query().Get("tab_id") != "" {
			u = "https://tabs.ultimate-guitar.com/tab/" + parsed.Query().Get("tab_id")
		}
	}
	u = canonicalTabURL(u)
	if strings.HasPrefix(u, "/") {
		u = baseURL + u
	}
	posted := number(s, "tab_created")
	if posted == 0 {
		posted = number(s, "date")
	}
	difficulty := field(s, "ug_difficulty")
	if difficulty == "" {
		difficulty = field(s, "difficulty")
	}
	t := Tab{ID: number(s, "id"), SongID: number(s, "song_id"), ArtistID: number(s, "artist_id"), Song: field(s, "song_name"), Artist: field(s, "artist_name"), Type: field(s, "type_name"), URL: u, Author: field(s, "username"), FileExtension: field(s, "fileExtension"), FileSize: int64(number(s, "size")), Rating: decimal(s, "rating"), Votes: number(s, "votes"), Difficulty: difficulty, Tuning: field(s, "tuning"), Key: field(s, "tonality_name"), Description: field(s, "version_description"), Views: number(s, "tab_views"), Favorites: number(s, "tab_favourites_count")}
	t.URL = canonicalTabRecordURL(t)
	if posted > 0 {
		t.Date = time.Unix(int64(posted), 0)
	}
	return t
}

func canonicalTabRecordURL(t Tab) string {
	u := canonicalTabURL(t.URL)
	const prefix = "https://tabs.ultimate-guitar.com/tab/"
	if strings.HasPrefix(u, prefix) && strings.Count(strings.TrimPrefix(u, prefix), "/") == 0 && t.ID > 0 && t.Artist != "" && t.Song != "" {
		parts := compactStrings([]string{slugPart(t.Song), slugPart(t.Type), fmt.Sprintf("%d", t.ID)})
		return fmt.Sprintf("%s%s/%s", prefix, slugPart(t.Artist), strings.Join(parts, "-"))
	}
	return u
}

func slugPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^\pL\pN]+`).ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// canonicalTabURL converts the several URL forms Ultimate Guitar emits into
// the tabs-host URL accepted reliably by the download worker. In particular,
// /pro/?tab_id=... is a landing page, not the downloadable tab route.
func canonicalTabURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/") {
		raw = baseURL + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Host == "www.ultimate-guitar.com" && u.Path == "/pro/" {
		if id := u.Query().Get("tab_id"); id != "" {
			return "https://tabs.ultimate-guitar.com/tab/" + id
		}
	}
	if u.Host == "tabs.ultimate-guitar.com" && strings.HasPrefix(u.Path, "/tab/") {
		return "https://tabs.ultimate-guitar.com" + u.Path
	}
	return u.String()
}

func tabObjects(data string) []Tab {
	// Prefer decoding the page data as JSON. The artist payload changes field
	// order and includes nested metadata, which makes a record-wide regexp too
	// brittle.
	var payload struct {
		OtherTabs []json.RawMessage `json:"other_tabs"`
	}
	if json.Unmarshal([]byte(data), &payload) != nil {
		_ = json.Unmarshal([]byte("{"+data+"}"), &payload)
	}
	if len(payload.OtherTabs) > 0 {
		return tabsFromObjects(payload.OtherTabs)
	}

	// Keep the tolerant fallback for older cached/parser fixtures that contain
	// only the contents of the other_tabs property.
	start := strings.Index(data, `"other_tabs":[`)
	if start < 0 {
		start = 0
	}
	end := strings.Index(data[start:], `],"feat_tabs"`)
	if end > 0 {
		data = data[start : start+end]
	}
	matches := regexp.MustCompile(`\{"id":\d+,"song_name":.*?"tab_url":"[^"]+".*?\}`).FindAllString(data, -1)
	objects := make([]json.RawMessage, 0, len(matches))
	for _, obj := range matches {
		objects = append(objects, json.RawMessage(obj))
	}
	return tabsFromObjects(objects)
}

func tabsFromObjects(objects []json.RawMessage) []Tab {
	seen := map[int]int{}
	tabs := make([]Tab, 0, len(objects))
	for _, raw := range objects {
		obj := string(raw)
		// The guitar_pro catalog can contain landing-page records whose URL is
		// www.ultimate-guitar.com/pro. Do not turn those into synthetic tab
		// URLs: they are not downloadable songs and should not reach the UI.
		if strings.Contains(obj, "ultimate-guitar.com/pro") {
			continue
		}
		t := tabFromObject(obj)
		if t.ID == 0 {
			continue
		}
		if previous, ok := seen[t.ID]; ok {
			// Artist pages can expose one Pro landing record and a second
			// record containing the actual tabs-host download URL.
			if strings.Contains(t.URL, "tabs.ultimate-guitar.com/tab/") &&
				!strings.Contains(tabs[previous].URL, "tabs.ultimate-guitar.com/tab/") {
				tabs[previous] = t
			}
			continue
		}
		seen[t.ID] = len(tabs)
		tabs = append(tabs, t)
	}
	return tabs
}

func (f *Fetcher) Tabs(ctx context.Context, artist Artist) ([]Tab, string, error) {
	firstURL := artistTabsURL(artist.URL, 1)
	allTabs := make([]Tab, 0)
	seen := make(map[int]bool)

	// Large artist catalogs are paginated by Ultimate Guitar. Keep walking
	// pages until one contributes no new tabs. The duplicate check also makes
	// this safe if the site ignores an unsupported page number.
	for page := 1; page <= 100; page++ {
		pageURL := artistTabsURL(artist.URL, page)
		p, err := f.get(ctx, pageURL)
		if err != nil {
			if page == 1 {
				return nil, firstURL, err
			}
			break
		}
		data := stateData(p)
		if data == "" {
			if page == 1 {
				return nil, firstURL, fmt.Errorf("artist state not found")
			}
			break
		}

		pageTabs := tabObjects(data)
		newTabs := 0
		for i := range pageTabs {
			if pageTabs[i].ArtistID == 0 {
				pageTabs[i].ArtistID = artist.ID
			}
			if pageTabs[i].Artist == "" {
				pageTabs[i].Artist = artist.Name
			}
			if pageTabs[i].Type == "" {
				pageTabs[i].Type = "Guitar Pro"
			}
			pageTabs[i].URL = canonicalTabRecordURL(pageTabs[i])
			if !isTabsTabURL(pageTabs[i].URL) {
				continue
			}
			if pageTabs[i].ID == 0 || seen[pageTabs[i].ID] {
				continue
			}
			seen[pageTabs[i].ID] = true
			allTabs = append(allTabs, pageTabs[i])
			newTabs++
		}
		if newTabs == 0 {
			break
		}
	}
	return allTabs, firstURL, nil
}

// validTabURL follows the tab route before exposing it to the UI. Ultimate
// Guitar keeps retired/official records in catalog responses that redirect
// to www.ultimate-guitar.com/pro; those are landing pages, not downloadable
// Guitar Pro tabs, and must never be listed or cached as tabs.
func (f *Fetcher) validTabURL(ctx context.Context, raw string) (string, bool) {
	raw = canonicalTabURL(raw)
	if !isTabsTabURL(raw) {
		return "", false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", "echoheist-guitar-pro-tui/0.1 (metadata browser)")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2<<20))
	finalURL := resp.Request.URL.String()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !isTabsTabURL(finalURL) {
		return "", false
	}
	return canonicalTabURL(finalURL), true
}

func (f *Fetcher) filterValidTabs(ctx context.Context, xs []Tab) []Tab {
	_ = ctx
	out := make([]Tab, 0, len(xs))
	for _, tab := range xs {
		tab.URL = canonicalTabRecordURL(tab)
		if isTabsTabURL(tab.URL) {
			out = append(out, tab)
		}
	}
	return out
}

func isTabsTabURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "tabs.ultimate-guitar.com" {
		return false
	}
	if !strings.HasPrefix(u.Path, "/tab/") {
		return false
	}
	rest := strings.TrimPrefix(u.Path, "/tab/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return true
	}
	if len(parts) == 1 && parts[0] != "" {
		for _, r := range parts[0] {
			if r < '0' || r > '9' {
				return true
			}
		}
	}
	return false
}

func artistTabsURL(rawURL string, page int) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set("filter", "guitar_pro")
	if page > 1 {
		q.Set("page", fmt.Sprintf("%d", page))
	} else {
		q.Del("page")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (f *Fetcher) Detail(ctx context.Context, tab Tab) (Tab, error) {
	tab.URL = canonicalTabRecordURL(tab)
	if tab.URL == "" {
		return tab, nil
	}
	p, err := f.get(ctx, tab.URL)
	if err != nil {
		return tab, err
	}
	data := stateData(p)
	if data == "" {
		return tab, fmt.Errorf("tab state not found")
	}
	start := strings.Index(data, `"tab":{`)
	if start < 0 {
		return tab, nil
	}
	obj := data[start+len(`"tab":`):]
	end := matchingObject(obj)
	if end > 0 {
		obj = obj[:end]
	}
	d := tabFromObject(obj)
	mergeTab(&tab, d)
	// File details live in `meta`, while posted time and view counters are
	// nested in the page's tracking context rather than the tab record.
	mergeTab(&tab, tabFromObject(embeddedObject(data, "meta")))
	mergeTab(&tab, tabFromObject(embeddedObject(data, "tracking")))
	tab.URL = canonicalTabURL(tab.URL)
	return tab, nil
}

func embeddedObject(s, key string) string {
	marker := `"` + key + `":`
	start := strings.Index(s, marker)
	if start < 0 {
		return ""
	}
	obj := s[start+len(marker):]
	end := matchingObject(obj)
	if end == 0 {
		return ""
	}
	return obj[:end]
}

func matchingObject(s string) int {
	if len(s) == 0 || s[0] != '{' {
		return 0
	}
	depth, quoted, escaped := 0, false, false
	for i, c := range []byte(s) {
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
		} else if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return 0
}
func mergeTab(dst *Tab, src Tab) {
	if src.ID > 0 {
		dst.ID = src.ID
	}
	if src.SongID > 0 {
		dst.SongID = src.SongID
	}
	if src.Song != "" {
		dst.Song = src.Song
	}
	if src.Artist != "" {
		dst.Artist = src.Artist
	}
	if src.Type != "" {
		dst.Type = src.Type
	}
	if src.URL != "" && preferTabURL(src.URL, dst.URL) {
		dst.URL = src.URL
	}
	if src.Author != "" {
		dst.Author = src.Author
	}
	if src.FileExtension != "" {
		dst.FileExtension = src.FileExtension
	}
	if src.FileSize != 0 {
		dst.FileSize = src.FileSize
	}
	if src.Rating != 0 {
		dst.Rating = src.Rating
	}
	if src.Votes != 0 {
		dst.Votes = src.Votes
	}
	if src.Difficulty != "" {
		dst.Difficulty = src.Difficulty
	}
	if src.Tuning != "" {
		dst.Tuning = src.Tuning
	}
	if src.Key != "" {
		dst.Key = src.Key
	}
	if src.Description != "" {
		dst.Description = src.Description
	}
	if src.Views != 0 {
		dst.Views = src.Views
	}
	if src.Favorites != 0 {
		dst.Favorites = src.Favorites
	}
	if !src.Date.IsZero() {
		dst.Date = src.Date
	}
}

// preferTabURL prevents later metadata fragments from replacing a usable
// canonical slug with the ID-only route, which Ultimate Guitar returns in
// some nested objects. The ID-only route currently responds with 404.
func preferTabURL(candidate, current string) bool {
	candidate = canonicalTabURL(candidate)
	current = canonicalTabURL(current)
	if current == "" {
		return candidate != ""
	}
	if candidate == "" {
		return false
	}
	return tabURLRank(candidate) >= tabURLRank(current)
}

func tabURLRank(value string) int {
	const prefix = "https://tabs.ultimate-guitar.com/tab/"
	if !strings.HasPrefix(value, prefix) {
		return 0
	}
	rest := strings.TrimPrefix(value, prefix)
	if strings.Contains(rest, "/") {
		return 2
	}
	return 1
}
func atoi(s string) int { var n int; fmt.Sscan(s, &n); return n }
