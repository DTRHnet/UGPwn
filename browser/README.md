# Echoheist Guitar Pro browser

A cross-platform terminal UI for browsing Ultimate Guitar's Guitar Pro metadata.
It can also queue selected Guitar Pro variants for a local Puppeteer worker that
**observes** the site's download response and reports what it is: artist, song,
byte count and Guitar Pro version.

The worker does not save the payload, and it does not re-issue the captured
request from Node. Those paths were removed deliberately — the finding they were
built to demonstrate is documented in `../README.md`, and `../SECURITY.md` records
why a working downloader is not the same thing as evidence. `--download-dir` is
accepted and ignored.

## Run

```sh
go run .
```

From the repository root, install the worker dependencies before using the
observation queue:

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
- `Space` or `v`: add/remove the highlighted Guitar Pro variant from the observation queue
- `d`: edit the (ignored) download destination directory
- `w`: run the queue, one observation at a time
- `x`: clear the queue when nothing is running
- `Esc` / `Left`: go back
- `q`: quit

Artist pages are fetched only after selecting a letter and an artist. Guitar Pro
variants are grouped by Ultimate Guitar's artist/song IDs, so duplicate rows for
one song become a single `+` tree entry. Expanded variants appear as indented
children, and the right preview pane follows the selected song or variant.

Selecting a tab also reads the detail page's metadata when available, including
the contributor, posted date, Guitar Pro extension (`.gp3`, `.gp4`, `.gp5`, and
similar), file size, rating, tuning, difficulty, views, and favorites. A queued
run opens each selected tab in Chromium and reports
`<artist> — <song> · <extension> · <bytes>`; nothing is written to disk.

The layout is responsive: the alphabet occupies a single top strip, the artist
browser fills the lower-left half, and the lower-right half is split into
metadata above and songs/tabs below. The artist list is paged based on terminal
height; use `A` and uppercase `D` to move between pages.

## Verify

```sh
go test ./...
go build ./...
```
