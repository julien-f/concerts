// Driven by verify-ui.sh: visits each screen at 390 px, runs axe-core in light
// and dark mode, saves the server HTML for vnu and checks the group page weight.
import { createRequire } from "node:module";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { gzipSync } from "node:zlib";

const [tools, groupURL, htmlDir, maxWeight] = process.argv.slice(2);
const require = createRequire(`${tools}/node_modules/`);
const { chromium } = require("playwright-core");
const axeSource = readFileSync(require.resolve("axe-core/axe.min.js"), "utf8");

// Each screen gets the page and returns the navigation response to check.
// Screens run in order and share cookies, so later ones can rely on earlier steps.
const screens = [
  { name: "group", go: (p) => p.goto(groupURL) },
];

mkdirSync(htmlDir, { recursive: true });
const browser = await chromium.launch();
const context = await browser.newContext({ viewport: { width: 390, height: 844 }, bypassCSP: true });
const page = await context.newPage();
let failures = 0;

for (const s of screens) {
  const resp = await s.go(page);
  if (!resp || !resp.ok()) throw new Error(`${s.name}: HTTP ${resp && resp.status()}`);
  writeFileSync(`${htmlDir}/${s.name}.html`, await resp.text());
  for (const colorScheme of ["light", "dark"]) {
    await page.emulateMedia({ colorScheme });
    await page.evaluate(axeSource);
    const { violations } = await page.evaluate(() =>
      window.axe.run(document, { resultTypes: ["violations"] }),
    );
    for (const v of violations) {
      failures++;
      console.log(`axe ${s.name} (${colorScheme}): ${v.id}: ${v.help}`);
      for (const n of v.nodes) console.log(`  ${n.target.join(" ")}: ${n.failureSummary.split("\n").join(" ")}`);
    }
  }
  console.log(`axe ${s.name}: checked light and dark`);
}

// Page weight: everything the group page loads with an empty cache, each body gzip -9'd.
const fresh = await browser.newContext({ storageState: await context.storageState() });
const weightPage = await fresh.newPage();
const sizes = {};
const pending = [];
weightPage.on("response", (r) => {
  if (r.request().resourceType() === "document" || /\.(css|js|svg|png|ico)$/.test(new URL(r.url()).pathname)) {
    pending.push(r.body().then((b) => (sizes[r.url()] = gzipSync(b, { level: 9 }).length)));
  }
});
await weightPage.goto(groupURL, { waitUntil: "networkidle" });
await Promise.all(pending);
await browser.close();
const weight = Object.values(sizes).reduce((a, b) => a + b, 0);
console.log(`page weight: ${weight} bytes gzip -9 (${Object.keys(sizes).length} files, limit ${maxWeight})`);
if (weight >= Number(maxWeight)) failures++;

if (failures) {
  console.log(`${failures} failure(s)`);
  process.exit(1);
}
