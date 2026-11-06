# Security notes — echoheistv2

This file exists because earlier documentation of this tool claimed it never
replayed captured browser requests. **That claim was false**, and this document
exists so the tool can be used honestly and safely. It was rewritten again when
the payload path was removed; everything below describes the code as it is.

## Intended use

Private security research against a flow the author has already disclosed: the
Guitar Pro download route on Ultimate Guitar. See [README.md](README.md#1-summary) for the
finding itself. Use it only on content you are authorized to access.

## What the worker does now: it observes, and it cannot download

`browser/download-worker.mjs` opens the queued tab page in a real Chromium and
records what the page's own download request returns. It emits one metadata
event — artist, song, byte count, Guitar Pro version — and **stops there**.

It does not:

- re-issue the captured request from Node (the old capture-and-replay fallback);
- write the payload to `--output-dir`, a temp file, or a sidecar report;
- emit a `saved` event, so no caller can be handed a file.

The removals are load-bearing, not cosmetic. The replay stage used to retrieve the
payload **outside** the browser session that the page believed it was serving,
which is the exact mechanism a real attacker would use. Without it, and without a
write, this is evidence-gathering for a disclosure rather than an exploit kit.

`browser/worker_contract_test.go` fails if any of those three return. It is a
source-level contract because the worker is JavaScript inside a Go module with no
Node test runner.

### What a run still touches

- The tab page and its download response are fetched by the browser, as any
  visitor's browser would fetch them.
- Chromium may stage a partial response in an OS temp directory. The worker
  deletes that directory before exiting; nothing lands in the download directory.
- `--output-dir` is still accepted so the CLI contract does not change, and it is
  **ignored**.
- The payload check (`--verify`) still runs and its result is reported, but a
  failure is no longer fatal: there is no file to reject, only a claim to
  qualify.

## Other behaviours worth knowing

- **No credentials are stored.** The worker launches a fresh browser profile per
  job and keeps no session, cookies, or tokens on disk.
- **Debug logs can contain response headers.** Verbose logging (`L` in the TUI,
  or the dashboard's log directory) writes worker trace lines, including
  response headers from the page. Treat `logs/` as sensitive and git-ignored.
- **`--no-sandbox` is passed to Chromium** because it runs acceptably in
  containers. It is a real reduction in browser isolation against a page you do
  not control.
- **The dashboard executes a local script.** `--worker` names the Node script run
  for every job. It is settable only at startup, never over HTTP, and every
  `/api/*` route requires the per-run token; the server emits no CORS headers. Do
  not bind it to a non-loopback address.
- **`--worker` is a trust boundary.** Nothing stops an operator pointing it at
  their own script; the shipped script is observe-only, the flag is not a
  security control.

## Scope guardrails

- Only `https://tabs.ultimate-guitar.com/...` tab URLs are accepted by the
  worker; the URL allowlist is enforced before navigation.
- The tool performs no request interception or header forging inside the page it
  loads: the page makes its own requests, and they are only read.

## Dashboard API surface

- Every `/api/*` route requires the per-run token (`X-Echoheist-Token` header or
  `?token=`), compared in constant time. The token is generated fresh on each
  start and substituted into the served page.
- No `Access-Control-Allow-*` header is emitted, so the API is same-origin only;
  a cross-site page cannot read responses and preflighted requests fail.
- `workerPath` is read-only over HTTP — it is the script that gets executed, so it
  can only be set with `--worker` at startup.
- `downloadDir` / `logDir` are validated (non-empty, no NUL bytes, cleaned) before
  use. `downloadDir` is no longer written by the worker.
- Cancelling a job kills the worker's process group, so Chromium is not orphaned.
- The live log ring buffer and the `logs/` files contain worker trace lines,
  including response headers from the target page. Treat both as sensitive.
- Job history and favorites are persisted in SQLite: tab metadata and job
  outcome. A completed job now records an observation, not a file path.

## The standalone script

`upstream-echoHEIST.sh` is deliberately neutralised. It prints artist, song, size
and Guitar Pro version for the tab URL you give it, and cannot save anything: its
`curl` replay and file write are gone. Running it expecting a file is a user
error, and the script says so before it does anything.

## Reporting

This is internal research tooling; it has no external security contact. If you
find a problem in the tool itself, report it to the repository owner rather than
upstream. Findings about the target site belong in the disclosure channel already
established for that report.

## Disclosure artifacts are separate

The observation harness and its redaction rules live in the research appendix of
this project, not in the shipped worker. Anything that records capability tokens
must keep redacting them: the short-hash fingerprint plus length is enough to
correlate a request without storing the capability itself.
