import crypto from "node:crypto";
import fs from "node:fs/promises";
import fsSync from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import puppeteer from "puppeteer";

const marker = "/download/public/";

function emit(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

function trace(jobId, phase, details = {}) {
  emit({ event: "trace", jobId, phase, at: new Date().toISOString(), ...details });
}

function log(jobId, level, text) {
  emit({ event: "log", jobId, level, text });
}

function sleep(milliseconds) {
  return new Promise(resolve => setTimeout(resolve, milliseconds));
}

function timeout(promise, milliseconds) {
  return Promise.race([
    promise,
    new Promise((_, reject) => setTimeout(() => reject(new Error("operation timed out")), milliseconds))
  ]);
}

function fail(message, code = "worker_error") {
  emit({ event: "error", code, message });
  process.exitCode = 1;
}

function parseArgs(argv) {
  const args = {};
  const take = index => {
    const value = argv[index];
    if (value === undefined) throw new Error(`missing value for ${argv[index - 1]}`);
    return value;
  };
  for (let i = 2; i < argv.length; i += 1) {
    const key = argv[i];
    if (key === "--url") args.url = take(++i);
    else if (key === "--output-dir") args.outputDir = take(++i);
    else if (key === "--artist") args.artist = take(++i);
    else if (key === "--song") args.song = take(++i);
    else if (key === "--job-id") args.jobId = take(++i);
    else if (key === "--format") args.format = take(++i);
    else if (key === "--versions") args.versions = take(++i);
    else if (key === "--include") args.include = take(++i);
    else if (key === "--timeout-sec") args.timeoutSec = Number(take(++i));
    else if (key === "--retries") args.retries = Number(take(++i));
    else if (key === "--delay-sec") args.delaySec = Number(take(++i));
    else if (key === "--verify") args.verify = take(++i);
    else if (key === "--help") args.help = true;
    else throw new Error(`unknown option: ${key}`);
  }
  return args;
}

// intArg clamps a numeric option into a sane range. A missing or unusable value
// falls back rather than disabling a limit.
function intArg(value, fallback, min, max) {
  if (value === undefined || value === null || Number.isNaN(value)) return fallback;
  return Math.min(max, Math.max(min, Math.trunc(value)));
}

function floatArg(value, fallback, min, max) {
  if (value === undefined || value === null || Number.isNaN(value)) return fallback;
  return Math.min(max, Math.max(min, value));
}

function listArg(value) {
  if (!value) return [];
  return String(value)
    .split(",")
    .map(item => item.trim())
    .filter(Boolean);
}

function validateUrl(value) {
  try {
    const url = new URL(value);
    const isTabPage = url.hostname === "tabs.ultimate-guitar.com" && url.pathname.startsWith("/tab/");
    const isProPage = url.hostname === "www.ultimate-guitar.com" && url.pathname === "/pro/";
    if (url.protocol !== "https:" || (!isTabPage && !isProPage)) throw new Error("unsupported URL");
    return url.toString();
  } catch {
    throw new Error("URL must be an HTTPS Ultimate Guitar tab or /pro/ URL");
  }
}

function canonicalUrl(value) {
  const url = new URL(value);
  if (url.hostname === "www.ultimate-guitar.com" && url.pathname === "/pro/") {
    const tabId = url.searchParams.get("tab_id");
    if (tabId) return `https://tabs.ultimate-guitar.com/tab/${encodeURIComponent(tabId)}`;
  }
  if (url.hostname === "tabs.ultimate-guitar.com" && url.pathname.startsWith("/tab/")) {
    return `https://tabs.ultimate-guitar.com${url.pathname}`;
  }
  return url.toString();
}

function safeFilename(name) {
  // Keep Unicode artist/song names readable. `\w` is ASCII-only in
  // JavaScript, which previously turned names such as Аквариум/10 СТРЕЛ
  // into underscore-only path components.
  return name
    .normalize("NFC")
    .replace(/[<>:"/\\|?*\u0000-\u001f]/g, "_")
    .replace(/\s+/gu, "_")
    .replace(/[. ]+$/gu, "")
    .slice(0, 180) || "ug-download.bin";
}

function safeSegment(name, fallback) {
  return safeFilename(name || fallback).replace(/[. ]+$/g, "").slice(0, 120) || fallback;
}

async function nextVersion(directory, baseName, extension) {
  const names = await fs.readdir(directory).catch(() => []);
  const escapedExtension = extension.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const escapedBaseName = baseName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = new RegExp(`^${escapedBaseName}_v(\\d+)${escapedExtension}$`, "i");
  let highest = 0;
  for (const name of names) {
    const match = pattern.exec(name);
    if (match) highest = Math.max(highest, Number.parseInt(match[1], 10));
  }
  return highest + 1;
}

const FORMAT_EXTENSIONS = { gp7: ".gp", gp6: ".gp", gp5: ".gp5", gpx: ".gpx", gp: ".gp" };
const GP_BINARY_MAGIC = "FICHIER GUITAR PRO";

function extensionFor(filename, contentType, formatPreference) {
  const ext = path.extname(filename || "");
  if (ext) return ext.toLowerCase();
  // No usable extension in the response: label the file after the requested
  // format. This is a naming preference only — nothing is transcoded.
  const preferred = FORMAT_EXTENSIONS[String(formatPreference || "").toLowerCase()];
  if (preferred) return preferred;
  if (contentType?.includes("zip")) return ".zip";
  if (contentType?.includes("pdf")) return ".pdf";
  return ".gp";
}

// verifyPayload rejects HTML error or login pages that a server returns with a
// 200 and a Guitar Pro-ish content type.
function verifyPayload(buffer, contentType) {
  if (!buffer || buffer.length === 0) return { ok: false, reason: "empty payload" };
  const head = buffer.subarray(0, 64).toString("latin1");
  if (head.startsWith(GP_BINARY_MAGIC)) return { ok: true, kind: "gp-binary" };
  // GP6/GP7/GPX are ZIP containers.
  if (buffer[0] === 0x50 && buffer[1] === 0x4b) return { ok: true, kind: "zip-container" };
  if (/^\s*(<!doctype|<html|<\?xml|<head|<body)/i.test(head)) {
    return { ok: false, reason: "server returned an HTML document instead of a Guitar Pro file" };
  }
  if (/guitar|gp[3-7x]|zip|octet-stream/i.test(contentType || "")) {
    return { ok: true, kind: "content-type" };
  }
  return { ok: false, reason: `unrecognised payload (content-type ${contentType || "unknown"})` };
}

function filenameFromDisposition(value) {
  if (!value) return null;
  const match = /filename\*=(?:UTF-8''|utf-8'')([^;]+)/i.exec(value) ||
    /filename="([^"]+)"/i.exec(value) || /filename=([^;]+)/i.exec(value);
  if (!match) return null;
  try { return decodeURIComponent(match[1].trim()); } catch { return match[1].trim(); }
}

function fallbackFilename(responseUrl, contentType) {
  const parsed = new URL(responseUrl);
  const last = parsed.pathname.split("/").filter(Boolean).pop() || "download";
  const hash = crypto.createHash("sha256").update(responseUrl).digest("hex").slice(0, 10);
  const extension = contentType?.includes("zip") ? ".zip" : contentType?.includes("pdf") ? ".pdf" : ".bin";
  return `ug-download-${safeFilename(last)}-${hash}${extension}`;
}

async function completedFiles(dir) {
  const names = await fs.readdir(dir);
  const files = [];
  for (const name of names) {
    if (name.endsWith(".crdownload")) continue;
    const file = path.join(dir, name);
    const stat = await fs.stat(file).catch(() => null);
    if (stat?.isFile() && stat.size > 0) files.push({ file, size: stat.size });
  }
  return files;
}

async function waitForDownload(dir, expectedName, expectedSize, timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const files = await completedFiles(dir);
    const byName = files.find(x => safeFilename(path.basename(x.file)) === expectedName);
    const bySize = expectedSize ? files.find(x => x.size === expectedSize) : null;
    if (byName || bySize || files[0]) return (byName || bySize || files[0]).file;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  return null;
}

// startProgressReporter emits {"event":"progress"} while a download is in flight.
// It polls the browser download directory so the UI has a real byte count even
// though Chromium gives us no callback here. Throttled to ~5 lines/second.
function startProgressReporter(jobId, tempDir, totalGetter) {
  let lastAt = 0;
  let lastBytes = -1;
  let lastPercent = -1;
  const timer = setInterval(async () => {
    const now = Date.now();
    if (now - lastAt < 200) return;
    lastAt = now;
    try {
      const files = await completedFiles(tempDir);
      const bytes = files.reduce((highest, file) => Math.max(highest, file.size), 0);
      const total = totalGetter() || 0;
      const percent = total > 0 ? Math.min(100, Math.floor((bytes / total) * 100)) : 0;
      if (bytes === lastBytes && percent === lastPercent) return;
      lastBytes = bytes;
      lastPercent = percent;
      emit({ event: "progress", jobId, bytes, total, percent });
    } catch {
      // The temp directory can vanish during cleanup; ignore.
    }
  }, 200);
  if (typeof timer.unref === "function") timer.unref();
  return () => clearInterval(timer);
}

async function main() {
  const args = parseArgs(process.argv);
  if (args.help) {
    process.stdout.write(
      "Usage: node download-worker.mjs --url <tab-url> --output-dir <dir> [--job-id <id>] [--format gp7|gp6|gp5|gpx|gp] [--versions a,b] [--include a,b] [--timeout-sec N] [--retries N] [--delay-sec N] [--verify 0|1]\n"
    );
    return;
  }
  const requestedUrl = validateUrl(args.url);
  const url = canonicalUrl(requestedUrl);
  const outputDir = path.resolve(args.outputDir || "");
  const jobId = args.jobId || "download";

  // Option handling. timeoutSec 0 means "no overall deadline", which keeps the
  // terminal UI's long downloads working while the dashboard always passes one.
  const formatPreference = String(args.format || "").toLowerCase();
  const versions = listArg(args.versions);
  const include = listArg(args.include);
  const timeoutSec = intArg(args.timeoutSec, 0, 0, 3600);
  const retries = intArg(args.retries, 5, 0, 20);
  const delaySec = floatArg(args.delaySec, 0, 0, 60);
  const verifyEnabled = args.verify === undefined ? true : !/^(0|false|no)$/i.test(String(args.verify));
  const startedAt = Date.now();
  const deadline = timeoutSec > 0 ? startedAt + timeoutSec * 1000 : 0;
  const cdpBodyTimeout = timeoutSec > 0 ? Math.min(30000, Math.max(1000, timeoutSec * 250)) : 3000;
  const checkDeadline = stage => {
    if (deadline && Date.now() > deadline) {
      throw new Error(`timeout after ${timeoutSec}s during ${stage}`);
    }
  };

  await fs.mkdir(outputDir, { recursive: true });
  const tempDir = await fs.mkdtemp(path.join(os.tmpdir(), "echoheist-download-"));
  let browser;
  const observations = [];
  let bodyBuffer = null;
  let downloadRequest = null;
  let expectedName = null;
  let expectedSize = null;
  let stopProgress = null;
  let canceled = false;

  // Cancellation: the Go side kills the process group, but a polite signal must
  // still produce a canceled event and close the browser rather than orphaning
  // Chromium.
  const onSignal = async signal => {
    canceled = true;
    log(jobId, "warn", `received ${signal}; closing the browser`);
    emit({ event: "canceled", jobId });
    if (stopProgress) stopProgress();
    try {
      if (browser) await browser.close();
    } catch {
      // Nothing more we can do while shutting down.
    }
    process.exit(143);
  };
  process.once("SIGTERM", () => { onSignal("SIGTERM"); });
  process.once("SIGINT", () => { onSignal("SIGINT"); });

  emit({ event: "started", jobId, url, requestedUrl });
  trace(jobId, "arguments", { requestedUrl, url, outputDir, artist: args.artist || null, song: args.song || null, pid: process.pid, cwd: process.cwd() });
  log(jobId, "info", `starting ${jobId} (format ${formatPreference || "gp7"}, timeout ${timeoutSec || "none"}s, retries ${retries}, delay ${delaySec}s, verify ${verifyEnabled})`);
  try {
    const configured = process.env.CHROME_PATH || process.env.PUPPETEER_EXECUTABLE_PATH;
    const candidates = [configured, "/usr/bin/google-chrome", "/opt/google/chrome/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"].filter(Boolean);
    const executablePath = candidates.find(candidate => fsSync.existsSync(candidate));
    trace(jobId, "launch", { executablePath: executablePath || "puppeteer-default", candidates });
    browser = await puppeteer.launch({
      headless: true,
      ...(executablePath ? { executablePath } : {}),
      args: ["--no-sandbox", "--disable-setuid-sandbox", "--disable-dev-shm-usage", "--disable-gpu"]
    });
    trace(jobId, "browser-launched", { version: await browser.version() });
    await browser.defaultBrowserContext().setDownloadBehavior({ policy: "allow", downloadPath: tempDir });
    trace(jobId, "download-behavior", { policy: "allow", downloadPath: tempDir });
    const page = await browser.newPage();
    trace(jobId, "page-created");
    const cdp = await page.createCDPSession();
    await cdp.send("Network.enable");
    trace(jobId, "cdp-network-enabled");
    const responseIds = new Map();
    cdp.on("Network.responseReceived", event => {
      if (!event.response.url.includes(marker)) return;
      const ids = responseIds.get(event.response.url) || [];
      ids.push(event.requestId);
      responseIds.set(event.response.url, ids);
    });
    page.on("request", request => {
      const requestHeaders = request.headers();
      trace(jobId, "request", { url: request.url(), method: request.method(), resourceType: request.resourceType(), headers: Object.fromEntries(Object.entries(requestHeaders).map(([key, value]) => [key, /cookie|authorization/i.test(key) ? "[redacted]" : value])) });
      if (!request.url().includes(marker)) return;
      const sourceHeaders = requestHeaders;
      const headers = {};
      for (const name of ["accept", "accept-language", "authorization", "cookie", "origin", "referer", "user-agent"]) {
        if (sourceHeaders[name]) headers[name] = sourceHeaders[name];
      }
      downloadRequest = { url: request.url(), headers };
      observations.push({ event: "request", url: request.url(), method: request.method(), resourceType: request.resourceType(), hasCookieHeader: Boolean(request.headers().cookie), hasReferer: Boolean(request.headers().referer) });
      emit({ event: "request", jobId, method: request.method(), resourceType: request.resourceType() });
    });
    page.on("response", async response => {
      trace(jobId, "response-received", { url: response.url(), status: response.status(), statusText: response.statusText(), headers: response.headers(), resourceType: response.request().resourceType() });
      if (!response.url().includes(marker)) return;
      const headers = response.headers();
      const contentType = headers["content-type"] || null;
      expectedSize = headers["content-length"] ? Number.parseInt(headers["content-length"], 10) : null;
      expectedName = safeFilename(filenameFromDisposition(headers["content-disposition"]) || fallbackFilename(response.url(), contentType));
      const observation = { event: "response", url: response.url(), status: response.status(), statusText: response.statusText(), contentType, contentLength: headers["content-length"] || null, contentDisposition: headers["content-disposition"] || null, resourceType: response.request().resourceType() };
      if (response.status() < 200 || response.status() >= 300) {
        observations.push(observation);
        emit({ event: "response", jobId, status: response.status(), contentType, contentLength: observation.contentLength });
        return;
      }
      try {
        trace(jobId, "cdp-body-read-start", { url: response.url() });
        const ids = responseIds.get(response.url()) || [];
        let requestId = ids.shift();
        if (ids.length === 0) responseIds.delete(response.url());
        // `retries` bounds the CDP body-read attempts (enforced option).
        for (let attempt = 0; attempt <= retries && !bodyBuffer; attempt += 1) {
          checkDeadline("body read");
          if (!requestId) {
            await new Promise(resolve => setTimeout(resolve, 250));
            const retryIds = responseIds.get(response.url()) || [];
            requestId = retryIds.shift();
            if (retryIds.length === 0) responseIds.delete(response.url());
          }
          if (requestId) {
            try {
              const result = await timeout(cdp.send("Network.getResponseBody", { requestId }), cdpBodyTimeout);
              const candidate = result.base64Encoded ? Buffer.from(result.body, "base64") : Buffer.from(result.body, "utf8");
              if (candidate.length > 0) bodyBuffer = candidate;
            } catch (error) {
              trace(jobId, "cdp-body-read-retry", { url: response.url(), attempt, message: error.message || String(error) });
              await new Promise(resolve => setTimeout(resolve, 500));
            }
          }
          requestId = null;
        }
      } catch (error) {
        trace(jobId, "cdp-body-read-failed", { url: response.url(), message: error.message || String(error) });
        // Chrome may not expose attachment bodies through CDP.
      }
      if (!bodyBuffer) {
        // Puppeteer can still expose the body even when CDP cannot.
        try {
          trace(jobId, "puppeteer-body-read-start", { url: response.url() });
          const candidate = await timeout(response.buffer(), cdpBodyTimeout);
          if (candidate.length > 0) bodyBuffer = candidate;
        } catch (error) {
          trace(jobId, "puppeteer-body-read-failed", { url: response.url(), message: error.message || String(error) });
        }
      }
      observations.push(observation);
      emit({ event: "response", jobId, status: response.status(), contentType, contentLength: observation.contentLength });
    });
    page.on("requestfailed", request => trace(jobId, "request-failed", { url: request.url(), method: request.method(), failure: request.failure() }));
    page.on("console", message => trace(jobId, "page-console", { type: message.type(), text: message.text(), location: message.location() }));
    page.on("pageerror", error => trace(jobId, "page-error", { message: error.message, stack: error.stack || null }));
    page.on("framenavigated", frame => trace(jobId, "frame-navigated", { url: frame.url(), isMainFrame: frame === page.mainFrame() }));
    page.on("close", () => trace(jobId, "page-closed"));
    let navigation = null;
    // Progress polling starts before navigation so the UI sees bytes as the
    // browser writes them. expectedSize is filled in by the response handler.
    stopProgress = startProgressReporter(jobId, tempDir, () => expectedSize);
    if (delaySec > 0) {
      // Enforced option: pause before navigating so rapid batches stay polite.
      log(jobId, "info", `waiting ${delaySec}s before navigation`);
      await sleep(delaySec * 1000);
    }
    for (let attempt = 0; attempt <= retries; attempt += 1) {
      checkDeadline("navigation");
      try {
        trace(jobId, "goto-start", { url, attempt });
        navigation = await page.goto(url, { waitUntil: "domcontentloaded", timeout: 60000 });
        trace(jobId, "goto-complete", { url: page.url(), status: navigation?.status() ?? null });
        break;
      } catch (error) {
        // Attachment responses can detach the frame while Puppeteer is waiting
        // for navigation. The download request is still usable in that case.
        if (downloadRequest || bodyBuffer) {
          trace(jobId, "goto-warning", { message: error.message || String(error), pageUrl: page.url(), hasDownloadRequest: Boolean(downloadRequest), bodyBytes: bodyBuffer?.length || 0, attempt });
          emit({ event: "navigation-warning", jobId, message: error.message || String(error) });
          break;
        }
        if (attempt >= retries) throw error;
        log(jobId, "warn", `navigation attempt ${attempt + 1}/${retries + 1} failed: ${error.message || String(error)}; retrying`);
        await sleep(Math.min(5000, 500 * (attempt + 1)));
      }
    }
    await new Promise(resolve => setTimeout(resolve, 2000));
    trace(jobId, "settled", { pageUrl: page.url(), tempDir, files: await completedFiles(tempDir) });
    const title = await page.title();
    emit({ event: "navigated", jobId, title, status: navigation?.status() ?? null });
    if (!bodyBuffer) {
      trace(jobId, "filesystem-download-wait", { expectedName, expectedSize });
      const source = await waitForDownload(tempDir, expectedName, expectedSize);
      if (source) bodyBuffer = await fs.readFile(source);
      trace(jobId, "filesystem-download-result", { source, bodyBytes: bodyBuffer?.length || 0 });
    }
    if (stopProgress) {
      stopProgress();
      stopProgress = null;
    }
    if (!bodyBuffer && downloadRequest) {
      // Kept deliberately: the capture-and-replay path is what makes the
      // access-control finding reproducible without browser download plumbing.
      // See SECURITY.md — it re-issues the page's own request, cookies included.
      log(jobId, "warn", "no browser body was produced; using the capture-and-replay fallback");
      trace(jobId, "fetch-fallback-start", { url: downloadRequest.url, headers: Object.keys(downloadRequest.headers) });
      const direct = await fetch(downloadRequest.url, { method: "GET", headers: downloadRequest.headers, redirect: "follow" });
      trace(jobId, "fetch-fallback-response", { status: direct.status, ok: direct.ok, url: direct.url, headers: Object.fromEntries(direct.headers.entries()) });
      if (direct.ok) bodyBuffer = Buffer.from(await direct.arrayBuffer());
    }
    if (canceled) throw new Error("canceled");
    if (!bodyBuffer || bodyBuffer.length === 0) throw new Error("No browser download was produced");
    const artistDir = safeSegment(args.artist, "Unknown Artist");
    const songDir = safeSegment(args.song, "Unknown Song");
    const destinationDir = path.join(outputDir, artistDir, songDir);
    await fs.mkdir(destinationDir, { recursive: true });
    const responseObservation = observations.find(item => item.event === "response");
    // Enforced option: reject an HTML error page that arrived with a 200.
    let verification = { ok: false, skipped: true };
    if (verifyEnabled) {
      verification = verifyPayload(bodyBuffer, responseObservation?.contentType);
      if (!verification.ok) {
        throw new Error(`payload failed verification: ${verification.reason}`);
      }
      log(jobId, "info", `payload verified (${verification.kind})`);
    } else {
      log(jobId, "warn", "payload verification disabled (--verify 0)");
    }
    const extension = extensionFor(expectedName, responseObservation?.contentType, formatPreference);
    const baseName = safeFilename(args.song || "ug-download").replace(/[. ]+$/gu, "") || "ug-download";
    const version = await nextVersion(destinationDir, baseName, extension);
    const destination = path.join(destinationDir, `${baseName}_v${version}${extension}`);
    // Keep the staging file beside the final file. The browser temp directory
    // may be on a different filesystem, where rename() would fail with EXDEV.
    const staged = path.join(destinationDir, `.staged-${process.pid}-${Date.now()}`);
    await fs.writeFile(staged, bodyBuffer, { flag: "wx" });
    await fs.rename(staged, destination);
    trace(jobId, "file-saved", { staged, destination, bytes: bodyBuffer.length });
    const report = {
      jobId,
      url,
      capturedAt: new Date().toISOString(),
      pageTitle: title,
      observations,
      path: destination,
      bytes: bodyBuffer.length,
      // Preference metadata: recorded, not applied to the payload.
      options: {
        format: formatPreference || null,
        versions,
        include,
        timeoutSec,
        retries,
        delaySec,
        verify: verifyEnabled
      },
      verification,
      elapsedMs: Date.now() - startedAt
    };
    await fs.writeFile(`${destination}.json`, `${JSON.stringify(report, null, 2)}\n`, { mode: 0o600 });
    log(jobId, "info", `saved ${destination} (${bodyBuffer.length} bytes)`);
    emit({ event: "saved", jobId, path: destination, bytes: bodyBuffer.length, report: `${destination}.json` });
    emit({ event: "completed", jobId });
  } finally {
    if (stopProgress) stopProgress();
    trace(jobId, "cleanup", { tempDir, hadBrowser: Boolean(browser) });
    if (browser) await browser.close().catch(() => {});
    await fs.rm(tempDir, { recursive: true, force: true });
  }
}

main().catch(error => fail(error.stack || error.message || String(error)));
