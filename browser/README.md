# Echoheist Guitar Pro browser

A cross-platform terminal UI for browsing Ultimate Guitar's Guitar Pro metadata.
It can also queue selected Guitar Pro variants for sequential browser-managed
downloads through the local Puppeteer worker. Use downloads only for files you
are authorized to retrieve.

The worker downloads through Chromium first, but when every browser path fails it
falls back to replaying the captured download request from Node with the page's
own headers, including cookies. That fallback is deliberate research tooling; it
is described in full in `../SECURITY.md`. Do not redistribute this tool or the
files it saves.

## Run

```sh
go run .
```

From the repository root, install the download worker dependencies before using
downloads:

```sh
cd ..
npm install
cd browser
```

The worker uses Puppeteer's Chromium, or a browser specified with
`CHROME_PATH` / `PUPPETEER_EXECUTABLE_PATH`. A default destination can be set
at startup:

```sh
go run . --worker ../download-worker.mjs
```

Or build a native executable:

```sh
go build -o echoheist-guitar-pro .
```

The cache is stored in the platform cache directory under
`echoheist-guitar-pro/cache.db`:

- Linux: `$XDG_CACHE_HOME` or `~/.cache`
- macOS: `~/Library/Caches`
- Windows: `%LocalAppData%`

## Controls

- Arrow keys or `hjkl`: navigate the active list
- `Enter`: expand a song row or select a variant
- `Space`: toggle a song tree
- `/`: open the persistent fzf-style fuzzy query bar
- `Tab`: switch the highlighted pane
- `Home` / `End`: jump to the first or last row
- `Ctrl-U` / `Ctrl-D`: half-page scroll
- `r`: refresh the current page
- `y`: copy the selected canonical URL
- `Space` or `v`: add/remove the highlighted Guitar Pro variant from the download queue
- `d`: edit the download destination directory
- `w`: run queued downloads sequentially
- `x`: clear the queue when no download is running
- `Esc` / `Left`: go back
- `q`: quit

Artist pages are fetched only after selecting a letter and an artist. Guitar Pro
variants are grouped by Ultimate Guitar's artist/song IDs, so duplicate rows for
one song become a single `+` tree entry. Expanded variants appear as indented
children, and the right preview pane follows the selected song or variant.

Selecting a tab also reads the detail page's metadata when available, including
the contributor, posted date, Guitar Pro extension (`.gp3`, `.gp4`, `.gp5`, and
similar), file size, rating, tuning, difficulty, views, and favorites. Queued
downloads open each selected tab in Chromium and save files under
`tabs/<artist>/<song>/<song>_v1.gp`, `<song>_v2.gp`, `<song>_v3.gp`, and so on. Missing directories
are created automatically, and each file receives a sidecar JSON report.

The layout is responsive: the alphabet occupies a single top strip, the artist
browser fills the lower-left half, and the lower-right half is split into
metadata above and songs/tabs below. The artist list is paged based on terminal
height; use `A` and uppercase `D` to move between pages.

## Verify

```sh
go test ./...
go build ./...
```
