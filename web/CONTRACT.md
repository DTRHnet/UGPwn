# echoheist web contract

Single source of truth for the Go backend and the Vite/Preact frontend. Both
halves must match this file exactly; change it here first.

Serving model: the frontend is built by `npm --prefix web run build` into
`browser/dashboard/dist` and embedded with `go:embed`. The dashboard process is
the only server. There is no dev proxy in production.

Security posture (do not weaken):
- Every `/api/*` request requires header `X-Echoheist-Token` (or `?token=`),
  compared in constant time against the per-run token.
- The served `index.html` has `{{ECHOHEIST_TOKEN}}` substituted with that token.
- No `Access-Control-Allow-*` headers are ever emitted.
- `workerPath` is never settable over HTTP.

## Types (JSON field names are canonical)

```ts
type Artist = {
  id: number;
  name: string;
  url: string;
  tabCount: number;
};

type Tab = {
  id: number; songId: number; artistId: number;
  song: string; artist: string; type: string; url: string;
  author: string; fileExtension: string; fileSize: number;
  rating: number; votes: number; difficulty: string;
  tuning: string; key: string; description: string;
  date: string;      // RFC3339
  views: number; favorites: number;
};

type Options = {
  format: "gp7" | "gp6" | "gp5" | "gpx" | "gp";
  versions: string[];   // e.g. ["official","official2","gp-user","tabber","all"]
  include: string[];    // e.g. ["tabs","bass","rhythm","drums","chords","backing","lyrics","tone"]
  timeoutSec: number;   // default 30
  retries: number;      // default 5
  delaySec: number;     // default 1.5
  verify: boolean;      // default true
};

type JobStatus = "queued" | "running" | "completed" | "failed" | "canceled";

type Job = {
  id: string;
  tab: Tab;
  status: JobStatus;
  message?: string;
  path?: string;        // saved file path
  bytes: number;        // bytes written
  progress: number;     // 0..100
  source: string;       // e.g. "Ultimate Guitar"
  tag: string;          // e.g. "GP7"
  startedAt: string;    // RFC3339
  endedAt?: string | null;
};

type LogLine = {
  seq: number;          // monotonically increasing across the process
  jobId: string;        // "" for server-level lines
  level: "info" | "warn" | "error" | "debug";
  text: string;
  at: string;           // RFC3339
};

type Source = { id: string; name: string; enabled: boolean; reason?: string };

// successRate is a FRACTION in 0..1 (completed / (completed+failed)), 0 when
// nothing has finished yet — not a percentage. `bytes` is an absolute total of
// saved bytes, never a rate.
type Metrics = { totalDownloads: number; successRate: number; bytes: number };

type State = {
  version: string;
  config: { downloadDir: string; workerPath: string; logDir: string };
  jobs: Job[];
  counts: { queued: number; running: number; completed: number; failed: number };
  metrics: Metrics;
  sources: Source[];
  options: Options;     // server-side defaults, so the UI can reset to them
};
```

## Endpoints

All under `/api`, all token-gated, all JSON. Errors are
`{"error": "message"}` with an HTTP status.

| Method | Path | Body | Returns |
|---|---|---|---|
| GET | `/api/state` | – | `State` |
| GET | `/api/jobs?since=<seq>` | – | `{ jobs: Job[], logs: LogLine[], cursor: number }` |
| POST | `/api/jobs` | `{ tabs: Tab[], options?: Options }` | `{ ids: string[] }` (202) |
| DELETE | `/api/jobs/{id}` | – | `{ ok: true }` — cancels a running job, removes a queued one |
| POST | `/api/jobs/{id}/retry` | – | `{ id: string }` — new job from the same tab+options |
| POST | `/api/jobs/clear` | – | `{ removed: number }` — drops finished jobs from the live list |
| GET | `/api/history?limit=100` | – | `{ items: Job[] }` — persisted, newest first |
| GET | `/api/favorites` | – | `{ items: Tab[] }` |
| POST | `/api/favorites` | `{ tab: Tab }` | `{ ok: true }` |
| DELETE | `/api/favorites/{id}` | – | `{ ok: true }` |
| GET | `/api/sources` | – | `{ items: Source[] }` |
| GET | `/api/logs` | – | `{ files: { name: string; size: number; modified: string }[] }` |
| GET | `/api/logs/{name}` | – | `{ name: string; lines: string[] }` — last 500 |
| GET | `/api/artists` | – | cached artists, no network |
| GET | `/api/artists?letter=X` | – | `{ items: Artist[], source: string }` (fetches live) |
| GET | `/api/artists/{id}` | – | `{ items: Tab[], source: string }` (cached) |
| GET | `/api/artists/{id}?refresh=1` | – | live fetch |
| POST | `/api/fetch/artists` | `{ letter: string }` | `{ items: Artist[], source: string }` |
| POST | `/api/fetch/tabs` | `{ artistId: number }` | `{ items: Tab[], source: string }` |
| GET | `/api/search?q=&letter=` | – | `{ items: { artist: Artist; tab: Tab }[], artists: Artist[], count: number, artistCount: number }` |
| GET | `/api/config` / PUT | `{ downloadDir?: string; logDir?: string }` | current config |
| GET | `/api/health` | – | `{ ok: true, time, jobs: number }` |

`GET /api/jobs?since=N` returns every log line with `seq > N` plus the full current
job list (job list is small; logs are incremental). The frontend keeps its own
`cursor`.

## Semantics the frontend must not assume away

1. **Queueing starts work; there is no separate start endpoint.** `POST /api/jobs`
   enqueues and the server's single-slot dispatcher begins immediately. A UI
   "Start all" can therefore only mean "queue these rows" — it is not a scheduler
   control, and the copy should say so.
2. **`seq` is process-local.** The ring restarts at 1 when the dashboard restarts.
   A client that reloads must reset its cursor to 0. As a safety net the server
   replays from the start when `since` is *ahead* of the head, so a cursor that
   survived a restart cannot leave the console permanently empty. Buffered lines
   are capped (5 000) and the oldest are dropped; a client further behind than
   the buffer accepts the gap rather than stalling.
   `endedAt` is **omitted** (not `null`) while a job is unfinished; `startedAt`
   carries queue time and is updated to the real start when execution begins.
3. **There is no resolve-from-URL endpoint.** `POST /api/jobs` takes `Tab` objects
   and trusts `url` as authoritative; a Tab carrying only a URL is queued with
   empty artist/song, which the worker saves under `Unknown_Artist/Unknown_Song`.
   Callers wanting real names should fetch the track first (`POST /api/fetch/tabs`)
   and send a populated Tab.
4. **`State.sources` is authoritative** for which sources the UI enables;
   `GET /api/sources` returns the same list and exists for convenience. Entries
   that are `enabled: false` always carry a `reason` and must render disabled.
5. **`workerPath` is never writable over HTTP.** `PUT /api/config` accepts only
   `downloadDir` and `logDir`; anything else in the body is ignored. `GET /api/config`
   returns `workerPath` for display.
6. **Cached artist reads** (`GET /api/artists`, `GET /api/artists/{id}`) exist but
   the current UI browses through the live `POST /api/fetch/*` endpoints, which
   return the same shape plus a `source` string.

## Worker protocol (stdout, one JSON object per line)

Existing: `started`, `trace`, `request`, `response`, `navigated`,
`navigation-warning`, `saved`, `completed`, `error`.

Added:

```json
{"event":"log","jobId":"...","level":"info","text":"..."}
{"event":"progress","jobId":"...","bytes":12345,"total":156000,"percent":7}
{"event":"canceled","jobId":"..."}
```

`progress` is throttled to at most ~5 lines/second. `total` is 0 when unknown.

Worker flags: `--url`, `--output-dir`, `--artist`, `--song`, `--job-id`, plus
`--format`, `--versions` (comma list), `--include` (comma list), `--timeout-sec`,
`--retries`, `--delay-sec`.

## What is actually enforced vs recorded

Be honest in both code and UI copy — the right panel shows a small footnote on
the options row.

| Option | Behaviour |
|---|---|
| `timeoutSec` | **Enforced** — overall worker deadline and the CDP body read budget |
| `retries` | **Enforced** — CDP body-read attempts and failed-navigation retries |
| `delaySec` | **Enforced** — pause before navigating |
| `verify` | **Enforced** — magic-byte (`FICHIER GUITAR PRO`) / content-type check before saving |
| `format` | **Preference** — biases which saved extension is chosen; no transcoding |
| `versions` / `include` | **Recorded** — written into the sidecar report; they do not filter a single-file download |

## Sources

```ts
[
  { id: "ultimate-guitar", name: "Ultimate Guitar", enabled: true },
  { id: "songsterr", name: "Songsterr", enabled: false, reason: "Not implemented — outside the scope of this research tool" },
  { id: "guitar-pro-tabs", name: "Guitar Pro Tabs", enabled: false, reason: "…" },
  { id: "mysongbook", name: "MySongBook", enabled: false, reason: "…" },
  { id: "musescore", name: "Musescore", enabled: false, reason: "…" }
]
```

## UI requirements

- 3 columns measured from the mockup: left rail `0–253px`, center cards `253–1172px`,
  right panel `1192–1520px`, header `0–56px`, footer `984–1024px`, at 1536×1024.
  Implement with CSS grid that scales; the measured numbers define the ratio, not
  fixed pixels.
- Tokens: page `#070d15`, panel `#0d141e`, border `#141c27`, body text `#c5c7cd`,
  heading `#ffffff`, muted `#73737a`, accent `#5b30d6`, success `#31b352`.
- Sidebar groups and labels exactly as in `README` of the mockup transcription:
  CORE (Downloader, Batch Queue +count badge, History, Favorites), SOURCES
  (5 sources + Custom Scrapers, Cookies & Auth), TOOLS (Search Engine, Metadata
  Fixer, GPX → GP MAX, GP Viewer, Audio to Tab, Tab Translator), SETTINGS
  (Settings, Profiles, Proxies, Logs).
- Every nav entry that has no implementation renders **disabled** with a tooltip
  carrying the reason. No entry may look clickable and do nothing silently.
- Center column, top to bottom: URL bar with `HEIST IT` CTA + `Paste from
  clipboard` + `Load from file` + `Advanced options` disclosure; OPTIONS card
  (FORMAT / VERSIONS / INCLUDE checkbox groups + ADVANCED grid of select,
  number steppers and toggles); QUEUE card with `Start all` / `Clear` and a table
  (index, song+artist, tag, source, progress bar with %, size, status); OUTPUT
  console (monospace, auto-scroll, `Open folder` / `Clear`, level colouring).
- Right panel: TRACK INFO (cover placeholder = deterministic gradient from the
  song name + initials, since no artwork source exists), PREVIEW (stave
  illustration + `Guitar Pro` / `Tab` tabs — a static SVG mock until alphaTab is
  integrated), FILE DETAILS (format, size, tuning, rating, author, views;
  unknown values render `—`, never invented).
- Footer: version, total downloads, success rate, bandwidth placeholder, plus a
  live sparkline of recent job progress.
- Accessibility: visible focus rings, `aria-label` on icon buttons, keyboard
  map (`/` search, `j`/`k` rows, `space` toggle select, `enter` open,
  `ctrl+enter` queue), `prefers-reduced-motion` respected.
- Empty/loading/error states for every data region. Never render a fake number.

## Testing

- Go: table tests for the token gate on every new route, job lifecycle
  (queue → run → cancel → retry), incremental log cursor, history/favorites
  persistence, and worker event parsing.
- Frontend: `tsc --noEmit` must pass; keep components typed against this file.
- `make check` and `go test -race ./...` must stay green.
