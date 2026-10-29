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
  for (let i = 2; i < argv.length; i += 1) {
    const key = argv[i];
    if (key === "--url") args.url = argv[++i];
    else if (key === "--output-dir") args.outputDir = argv[++i];
    else if (key === "--artist") args.artist = argv[++i];
    else if (key === "--song") args.song = argv[++i];
    else if (key === "--job-id") args.jobId = argv[++i];
    else if (key === "--help") args.help = true;
    else throw new Error(`unknown option: ${key}`);
  }
  return args;
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

function extensionFor(filename, contentType) {
  const ext = path.extname(filename || "");
  if (ext) return ext.toLowerCase();
  if (contentType?.includes("zip")) return ".zip";
  if (contentType?.includes("pdf")) return ".pdf";
  return ".gp";
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

async function main() {
  const args = parseArgs(process.argv);
  if (args.help) {
    process.stdout.write("Usage: node download-worker.mjs --url <tab-url> --output-dir <dir> [--job-id <id>]\n");
    return;
  }
  const requestedUrl = validateUrl(args.url);
  const url = canonicalUrl(requestedUrl);
  const outputDir = path.resolve(args.outputDir || "");
  const jobId = args.jobId || "download";
  await fs.mkdir(outputDir, { recursive: true });
  const tempDir = await fs.mkdtemp(path.join(os.tmpdir(), "echoheist-download-"));
  let browser;
  const observations = [];
  let bodyBuffer = null;
    let downloadRequest = null;
  let expectedName = null;
  let expectedSize = null;
  emit({ event: "started", jobId, url, requestedUrl });
  trace(jobId, "arguments", { requestedUrl, url, outputDir, artist: args.artist || null, song: args.song || null, pid: process.pid, cwd: process.cwd() });
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
        for (let attempt = 0; attempt < 3 && !bodyBuffer; attempt += 1) {
          if (!requestId) {
            await new Promise(resolve => setTimeout(resolve, 250));
            const retryIds = responseIds.get(response.url()) || [];
            requestId = retryIds.shift();
            if (retryIds.length === 0) responseIds.delete(response.url());
          }
          if (requestId) {
            try {
              const result = await timeout(cdp.send("Network.getResponseBody", { requestId }), 3000);
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
          const candidate = await timeout(response.buffer(), 3000);
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
    try {
      trace(jobId, "goto-start", { url });
      navigation = await page.goto(url, { waitUntil: "domcontentloaded", timeout: 60000 });
      trace(jobId, "goto-complete", { url: page.url(), status: navigation?.status() ?? null });
    } catch (error) {
      // Attachment responses can detach the frame while Puppeteer is waiting
      // for navigation. The download request is still usable in that case.
      if (!downloadRequest && !bodyBuffer) throw error;
      trace(jobId, "goto-warning", { message: error.message || String(error), pageUrl: page.url(), hasDownloadRequest: Boolean(downloadRequest), bodyBytes: bodyBuffer?.length || 0 });
      emit({ event: "navigation-warning", jobId, message: error.message || String(error) });
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
    if (!bodyBuffer && downloadRequest) {
      trace(jobId, "fetch-fallback-start", { url: downloadRequest.url, headers: Object.keys(downloadRequest.headers) });
      const direct = await fetch(downloadRequest.url, { method: "GET", headers: downloadRequest.headers, redirect: "follow" });
      trace(jobId, "fetch-fallback-response", { status: direct.status, ok: direct.ok, url: direct.url, headers: Object.fromEntries(direct.headers.entries()) });
      if (direct.ok) bodyBuffer = Buffer.from(await direct.arrayBuffer());
    }
    if (!bodyBuffer || bodyBuffer.length === 0) throw new Error("No browser download was produced");
    const artistDir = safeSegment(args.artist, "Unknown Artist");
    const songDir = safeSegment(args.song, "Unknown Song");
    const destinationDir = path.join(outputDir, artistDir, songDir);
    await fs.mkdir(destinationDir, { recursive: true });
    const responseObservation = observations.find(item => item.event === "response");
    const extension = extensionFor(expectedName, responseObservation?.contentType);
    const baseName = safeFilename(args.song || "ug-download").replace(/[. ]+$/gu, "") || "ug-download";
    const version = await nextVersion(destinationDir, baseName, extension);
    const destination = path.join(destinationDir, `${baseName}_v${version}${extension}`);
    // Keep the staging file beside the final file. The browser temp directory
    // may be on a different filesystem, where rename() would fail with EXDEV.
    const staged = path.join(destinationDir, `.staged-${process.pid}-${Date.now()}`);
    await fs.writeFile(staged, bodyBuffer, { flag: "wx" });
    await fs.rename(staged, destination);
    trace(jobId, "file-saved", { staged, destination, bytes: bodyBuffer.length });
    const report = { jobId, url, capturedAt: new Date().toISOString(), pageTitle: title, observations, path: destination, bytes: bodyBuffer.length };
    await fs.writeFile(`${destination}.json`, `${JSON.stringify(report, null, 2)}\n`, { mode: 0o600 });
    emit({ event: "saved", jobId, path: destination, bytes: bodyBuffer.length, report: `${destination}.json` });
    emit({ event: "completed", jobId });
  } finally {
    trace(jobId, "cleanup", { tempDir, hadBrowser: Boolean(browser) });
    if (browser) await browser.close().catch(() => {});
    await fs.rm(tempDir, { recursive: true, force: true });
  }
}

main().catch(error => fail(error.stack || error.message || String(error)));
