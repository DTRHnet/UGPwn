# echoheistv2

Private research tooling for documenting the Guitar Pro tab-download authorization
flow on Ultimate Guitar. It has two front ends over one Go core:

- a **terminal UI** (Bubble Tea) for browsing artists → songs → tab variants;
- a **local web dashboard** for the same data with a download queue.

Both drive the same `Puppeteer` worker to save the files they obtain.

> **Status: private research use only.**
> This tree is internal tooling built around a security disclosure. It is not
> published, not redistributed, and must not be pointed at content you are not
> authorized to retrieve. Read [SECURITY.md](SECURITY.md) before running it — it
> describes exactly what the download worker does, including the
> capture-and-replay fallback and the risks that come with it.

## Requirements

- Go 1.23+
- Node.js 20+ with `npm`
- Chrome or Chromium (Puppeteer's bundled Chromium works; otherwise set `CHROME_PATH`)

## Setup

```sh
npm install         # installs Puppeteer for the download worker
make                # gofmt + vet + tests + binary
```

## Run

```sh
./start.sh              # terminal UI
./start-dashboard.sh    # web dashboard on http://127.0.0.1:8787
```

The dashboard prints a per-run token gate notice at startup. Every `/api` request
requires that run's token, which the served page picks up automatically; there is
no way to reach the API from another origin.

By default the dashboard binds to `127.0.0.1:8787`. Override it with
`ECHOHEIST_LISTEN=127.0.0.1:9090 ./start-dashboard.sh`. Binding it to a
non-loopback address exposes an unauthenticated-by-network tool that can execute a
local script — don't. Requests need the per-run token, but any client that can
reach `/` is served that token, so loopback is the real boundary.

## Web dashboard

The dashboard is a three-column workspace: a navigation rail (`CORE` — Downloader,
Batch Queue, History, Favorites; `SOURCES`; `TOOLS`; `SETTINGS`), a centre column
of cards (URL bar with `HEIST IT`, `OPTIONS`, `QUEUE`, `OUTPUT` console) and a
right-hand panel with `TRACK INFO`, `PREVIEW` and `FILE DETAILS`. A status bar
carries the version, download totals, success rate and byte count.

Navigation entries without an implementation are rendered **disabled with the
reason in a tooltip** — nothing in the UI pretends to work.

Keyboard: `/` search, `j`/`k` move the queue selection, `space` toggle row
selection, `enter` open the tab, `ctrl+enter` queue.

### Options: what is enforced and what is only recorded

| Option | Behaviour |
|---|---|
| Timeout, retries, delay | **Enforced** on the worker: overall deadline, CDP body/navigation attempts, pause before navigation |
| Verify files | **Enforced**: magic-byte (`FICHIER GUITAR PRO`) / content-type check before a file is saved |
| Format | **Preference only** — biases the chosen extension; there is no transcoding |
| Versions, include | **Recorded** in the sidecar report; they do not filter a single-file download |

The UI states this next to the options row so the controls cannot be mistaken for
conversions the tool does not perform.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--download-dir` | `./tabs` | where saved files go |
| `--worker` | `download-worker.mjs` | Puppeteer worker script (see SECURITY.md) |
| `--log-dir` | `./logs` | optional per-run debug logs |
| `--dashboard` | off | run the web dashboard instead of the TUI |
| `--listen` | `127.0.0.1:8787` | dashboard bind address |
| `--version` | | print version/commit/build date and exit |

## Terminal UI controls

| Key | Action |
|---|---|
| arrows / `hjkl` | move through the active pane |
| `Enter` | expand a song row, open the next pane, inspect a variant |
| `Space` or `v` | queue / unqueue the highlighted Guitar Pro variant |
| `w` | run the queue sequentially |
| `x` | cancel the active download, or clear an idle queue |
| `d` | edit the download directory |
| `L` | toggle verbose worker logging (unique file under `logs/`) |
| `/` | fuzzy search |
| `y` | copy the selected canonical URL |
| `r` | refresh the current pane |
| `Tab` / `Shift+Tab` | cycle panes |
| `a` / `D` | previous / next artist page |
| `Esc` / `Left` | go back |
| `q` / `Ctrl+C` | quit |

## Download layout

```
tabs/<artist>/<song>/<song>_v1.<ext>
tabs/<artist>/<song>/<song>_v2.<ext>
tabs/<artist>/<song>/<song>_v1.<ext>.json   # sidecar report
```

Extensions come from the server's declared filename/content type (`.gp3`, `.gp4`,
`.gp5`, `.gpx`, …). Existing versions are never overwritten; missing directories are
created automatically. The JSON sidecar records the observation metadata for the run.

## Development

```sh
make web      # build the Preact dashboard into browser/dashboard/dist
make test     # unit tests (hermetic: no network, no database)
make race     # unit tests under the race detector
make vet      # go vet
make fmt      # gofmt -w
make dist     # versioned tarball + SHA256SUMS under dist/
```

The Go module lives in `browser/`. `download-worker.mjs` at the root is a thin
import shim so the launcher and the module can share one worker implementation.

The web UI is Vite + TypeScript + Preact under `web/`. `make web` builds it into
`browser/dashboard/dist`, which `browser/dashboard.go` embeds with `go:embed`, so
the shipped binary is self-contained — no separate static server. `web/CONTRACT.md`
is the API contract shared by both halves; change it there first.

Tests are hermetic: scrapers, cache, keybindings and dashboard handlers are
exercised without touching the network or the site.

## What this is not

- Not a public release, package, or service.
- Not affiliated with or endorsed by Ultimate Guitar.
- Not a general-purpose downloader: it targets one documented flow for research.
- Not safe to expose beyond loopback (see SECURITY.md).
