// Headless render check: loads the running dashboard at the mockup's reference
// resolution, dumps the computed geometry and saves a screenshot so the result
// can be compared against echoheist.png with pixel tools.
//
// Usage: node scripts/render_check.mjs [port] [outfile]
//
// It only reads the served page; it never queues a download.

import puppeteer from "puppeteer";

const port = process.argv[2] || "8795";
const outFile = process.argv[3] || "ui-render.png";

const browser = await puppeteer.launch({
  headless: true,
  executablePath: process.env.CHROME_PATH || "/opt/google/chrome/chrome",
  args: ["--no-sandbox", "--disable-setuid-sandbox", "--disable-dev-shm-usage", "--disable-gpu"],
});

try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1536, height: 1024, deviceScaleFactor: 1 });
  const consoleErrors = [];
  const failedRequests = [];
  page.on("pageerror", (err) => consoleErrors.push(String(err)));
  page.on("console", (msg) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });
  page.on("response", (res) => {
    if (res.status() >= 400) failedRequests.push({ status: res.status(), url: res.url() });
  });
  page.on("requestfailed", (req) => {
    failedRequests.push({ status: "network", url: req.url(), error: req.failure()?.errorText });
  });

  await page.goto(`http://127.0.0.1:${port}/`, { waitUntil: "networkidle2", timeout: 30000 });
  // Let the first /api/state poll land so the shell is fully populated.
  await new Promise((resolve) => setTimeout(resolve, 1200));

  const report = await page.evaluate(() => {
    const box = (el) => {
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) };
    };
    const q = (sel) => document.querySelector(sel);
    const workspace = q(".workspace");
    const columns = workspace ? [...workspace.children].map((child) => ({
      cls: String(child.className || child.tagName).split(" ")[0],
      box: box(child),
    })) : [];
    const disabled = [...document.querySelectorAll('[aria-disabled="true"]')]
      .map((el) => ({ text: (el.textContent || "").trim().slice(0, 40), title: (el.getAttribute("title") || "").slice(0, 60) }));

    return {
      viewport: { w: window.innerWidth, h: window.innerHeight },
      bodyBg: getComputedStyle(document.body).backgroundColor,
      title: document.title,
      gridTemplateColumns: workspace ? getComputedStyle(workspace).gridTemplateColumns : null,
      header: box(q("header")),
      footer: box(q("footer")),
      workspace: box(workspace),
      columns,
      navItemCount: document.querySelectorAll("nav button, nav a, .nav-item").length,
      disabledCount: disabled.length,
      disabled: disabled.slice(0, 24),
      headings: [...document.querySelectorAll("h1, h2, h3")].map((el) => (el.textContent || "").trim()).slice(0, 24),
      buttons: [...document.querySelectorAll("button")].map((el) => (el.textContent || "").trim()).filter(Boolean).slice(0, 30),
      hasTokenMeta: Boolean(document.querySelector('meta[name="echoheist-token"]')?.content),
      scrollHeight: document.documentElement.scrollHeight,
    };
  });

  await page.screenshot({ path: outFile });
  report.consoleErrors = consoleErrors.slice(0, 10);
  report.failedRequests = failedRequests.slice(0, 20);
  report.screenshot = outFile;
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser.close();
}
