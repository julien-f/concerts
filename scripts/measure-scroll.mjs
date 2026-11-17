// Driven by measure-scroll.sh. Compares scroll smoothness of ref.css and work.css on a phone.
//
// snapshot <tools> <groupURL> <dir>: saves the demo's group page as <dir>/page.html, past concerts open and
//   cards cloned ×5 (long enough for a run), without scripts or icons; the server fills in the CSS and
//   the benchmark (measure-scroll-page.js).
// serve <dir> <lan-ip> <iface> <ref>: serves it on <lan-ip>:8765 and drives the series: runs in ABBA BAAB
//   order (ref/work, against thermal drift), each page posting its frame intervals; prints the table.
//
// What it measures: requestAnimationFrame intervals during a scripted scroll, i.e. main-thread work plus
// the renderer falling behind (how Firefox's mask-image and box-shadow costs showed). It misses what only a
// finger (compositor) scroll hits, such as checkerboarding, and overstates main-thread costs.
// The LAN address is the default route's: with a full-tunnel VPN, that is the VPN's.
import { createRequire } from "node:module";
import { randomUUID } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";

const PORT = 8765;
const PLAN = ["ref", "work", "work", "ref", "work", "ref", "ref", "work"]; // pairs (0,1), (2,3)…
const [cmd, ...args] = process.argv.slice(2);

if (cmd === "snapshot") await snapshot(...args);
else if (cmd === "serve") serve(...args);
else throw new Error(`unknown command ${cmd}`);

async function snapshot(tools, groupURL, dir) {
  const require = createRequire(`${tools}/node_modules/`);
  const { chromium } = require("playwright-core");
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.goto(groupURL);
  await Promise.all([page.waitForNavigation(), page.click('button:has-text("Léa")')]);
  const html = await page.evaluate(() => {
    document.querySelectorAll('script, link[rel="icon"], link[rel="apple-touch-icon"]').forEach((e) => e.remove());
    document.querySelector('link[rel="stylesheet"]').setAttribute("href", "/__CSS__.css");
    document.head.append(document.createComment("BENCH"));
    document.querySelectorAll("details").forEach((d) => (d.open = true));
    for (const ul of document.querySelectorAll(".concerts")) {
      const items = [...ul.children];
      for (let i = 0; i < 4; i++) items.forEach((li) => ul.append(li.cloneNode(true)));
    }
    return "<!doctype html>" + document.documentElement.outerHTML;
  });
  await browser.close();
  writeFileSync(`${dir}/page.html`, html);
}

function serve(dir, lan, iface, ref) {
  const nonce = randomUUID();
  const page = readFileSync(`${dir}/page.html`, "utf8");
  const files = {
    "/ref.css": ["text/css", readFileSync(`${dir}/ref.css`)],
    "/work.css": ["text/css", readFileSync(`${dir}/work.css`)],
    "/bench.js": ["text/javascript", readFileSync(new URL("measure-scroll-page.js", import.meta.url))],
  };
  const results = [];
  let ua = "";

  const render = (run) =>
    page
      .replace("/__CSS__.css", `/${PLAN[run] ?? "work"}.css`)
      .replace(
        "<!--BENCH-->",
        `<script src="/bench.js" data-run="${run ?? ""}" data-runs="${PLAN.length}" data-nonce="${nonce}" defer></script>`,
      );

  const server = createServer((req, res) => {
    const url = new URL(req.url, "http://x");
    const send = (status, type, body) => {
      res.writeHead(status, { "Content-Type": type, "Cache-Control": "no-store" });
      res.end(body);
    };
    if (req.method === "POST" && url.pathname === "/results") return receive(req, () => send(204, "text/plain", ""));
    if (req.method !== "GET") return send(405, "text/plain", "method not allowed\n");
    if (files[url.pathname]) return send(200, ...files[url.pathname]);
    if (url.pathname !== "/") return send(404, "text/plain", "not found\n");
    const run = url.searchParams.has("run") ? Number(url.searchParams.get("run")) : undefined;
    if (run >= PLAN.length) return send(200, "text/plain; charset=utf-8", "Done: the results are in the terminal.\n");
    if (run !== undefined && !(Number.isInteger(run) && run >= 0)) return send(400, "text/plain", "bad run\n");
    send(200, "text/html; charset=utf-8", render(run));
  });

  function receive(req, ok) {
    let body = "";
    req.setEncoding("utf8");
    req.on("data", (chunk) => {
      body += chunk;
      if (body.length > 64 * 1024) req.destroy();
    });
    req.on("end", () => {
      ok();
      let r;
      try {
        r = JSON.parse(body);
      } catch {
        return;
      }
      if (r.nonce !== nonce || !Number.isInteger(r.run) || PLAN[r.run] === undefined) return;
      if (r.error) {
        console.error(`run ${r.run + 1}: ${r.error}`);
        process.exit(1);
      }
      if (!Array.isArray(r.dt) || r.dt.length < 10 || results[r.run]) return;
      ua ||= String(r.ua);
      results[r.run] = stats(r.dt.map(Number));
      console.log(`run ${r.run + 1}/${PLAN.length} ${PLAN[r.run]}: ${results[r.run].fps.toFixed(1)} fps`);
      if (PLAN.every((_, i) => results[i])) {
        report();
        setTimeout(() => process.exit(0), 1000); // lets the phone load the "Done" page
      }
    });
  }

  function report() {
    const done = PLAN.map((v, i) => results[i] && { v, ...results[i] }).filter(Boolean);
    if (!done.length) return console.log("no run received");
    const of = (v, k) => median(done.filter((r) => r.v === v).map((r) => r[k]));
    const pairs = [];
    for (let i = 0; i + 1 < PLAN.length; i += 2) {
      const [a, b] = [results[i], results[i + 1]];
      if (!a || !b) continue;
      const [r, w] = PLAN[i] === "ref" ? [a, b] : [b, a];
      pairs.push({ fps: w.fps - r.fps, dropped: w.dropped - r.dropped });
    }
    const pct = (x) => `${Math.round(100 * x)} %`;
    const sign = (x, d = 1) => {
      const r = Number(x.toFixed(d)) || 0; // no "-0.0"
      return (r >= 0 ? "+" : "") + r.toFixed(d);
    };
    const spread = (xs, f) => `${f(median(xs))} [${f(Math.min(...xs))} ${f(Math.max(...xs))}]`;
    console.log(`\n${ua}\nrefresh ${median(done.map((r) => r.refresh)).toFixed(1)} ms, ${pairs.length} pairs, ref = ${ref}`);
    console.log(`${"".padEnd(10)}${"fps".padEnd(22)}dropped frames`);
    for (const v of ["ref", "work"]) {
      if (done.some((r) => r.v === v)) console.log(`${v.padEnd(10)}${of(v, "fps").toFixed(1).padEnd(22)}${pct(of(v, "dropped"))}`);
    }
    if (pairs.length) {
      const fps = spread(pairs.map((p) => p.fps), (x) => sign(x));
      const dropped = spread(pairs.map((p) => 100 * p.dropped), (x) => sign(x, 0));
      console.log(`${"work-ref".padEnd(10)}${fps.padEnd(22)}${dropped} pts`);
    }
  }

  process.on("SIGINT", () => {
    report();
    process.exit(130);
  });
  server.on("error", (e) => {
    console.error(e.code === "EADDRINUSE" ? `port ${PORT} is taken on ${lan}` : e.message);
    process.exit(1);
  });
  server.listen(PORT, lan, () =>
    console.log(
      `on ${iface}: open http://${lan}:${PORT}/ on the phone (plugged in, battery saver off), ` +
        `tap "Start", then don't touch the screen`,
    ),
  );
}

// Refresh interval: 10th percentile of the intervals; a frame lasting k intervals dropped k-1.
function stats(dt) {
  const refresh = quantile(dt, 0.1);
  const slots = dt.map((d) => Math.max(1, Math.round(d / refresh)));
  const sum = (xs) => xs.reduce((a, b) => a + b, 0);
  return { fps: (1000 * dt.length) / sum(dt), refresh, dropped: sum(slots.map((s) => s - 1)) / sum(slots) };
}

function quantile(xs, q) {
  const s = [...xs].sort((a, b) => a - b);
  return s[Math.min(s.length - 1, Math.floor(q * s.length))];
}

function median(xs) {
  const s = [...xs].sort((a, b) => a - b);
  return s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2;
}
