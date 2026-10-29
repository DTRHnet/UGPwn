# Security notes — echoheistv2

This file exists because earlier documentation of this tool claimed it never
replayed captured browser requests. **That claim was false**, and this document
describes what the code actually does so the tool can be used honestly and
safely.

## Intended use

Private security research against a flow the author has already disclosed:
the Guitar Pro download route on Ultimate Guitar. Use it only on content you are
authorized to retrieve and only for producing redacted evidence for that
disclosure. Do not redistribute this tool or the files it saves.

## What the download worker actually does

`browser/download-worker.mjs` opens the queued tab page in a real Chromium and
tries, in order, to obtain the Guitar Pro payload:

1. **CDP body read** — `Network.getResponseBody` for the response matching the
   download route (up to three attempts).
2. **Puppeteer body read** — `response.buffer()`.
3. **Filesystem watch** — the browser's own download into a temp directory.
4. **Capture-and-replay fallback** — if all three produce nothing, the worker
   re-issues the captured download request from Node with the tab page's own
   request headers (`cookie`, `authorization`, `referer`, `origin`,
   user-agent) via `fetch`.

Stage 4 is the important one. It is deliberately kept: it reproduces the
access-control finding without depending on browser download plumbing, which is
what makes the observation reproducible. It also means:

- the worker can retrieve the payload **outside** the browser session that the
  page believed it was serving;
- the same shortcut is the mechanism a real attacker would use, so treat results
  as exploit-capable evidence, not as harmless scraping;
- the saved files are third-party copyrighted content. They are git-ignored and
  must never be committed, packed into a release, or shared.

If you do not need stage 4, delete it (`download-worker.mjs`, the
`downloadRequest` capture and the `fetch` fallback) and the tool degrades to
"browser downloads only" — still capable of the same access, just with more
failure modes.

## Other behaviours worth knowing

- **No credentials are stored.** The worker launches a fresh browser profile per
  job and keeps no session, cookies, or tokens on disk.
- **Debug logs can contain response headers.** Verbose logging (`L` in the TUI,
  or the dashboard's log directory) writes worker trace lines, including
  response headers from the page. Treat `logs/` as sensitive and git-ignored.
- **`--no-sandbox` is passed to Chromium** because it runs acceptably in
  containers. It is a real reduction in browser isolation against a page you do
  not control.
- **The dashboard executes a local script.** `--worker` names the Node script
  run for every download. It is settable only at startup, never over HTTP, and
  every `/api` route requires the per-run token; the server emits no CORS
  headers. Do not bind it to a non-loopback address.
- **Saved payloads are not content-verified.** A 200 response carrying an HTML
  error page would be written with whatever extension the response suggested.

## Scope guardrails

- Only `https://tabs.ultimate-guitar.com/...` tab URLs are accepted by the
  worker; the URL allowlist is enforced before navigation.
- The tool performs no request interception or header forging inside the page
  it loads: the page makes its own requests, and stage 4 is a separate out-of-page
  replay from Node.

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
