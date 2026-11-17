// Driven by verify-ui.sh: visits each screen at 390 px, runs each screen's own
// check, then at 390 and 768 px (phone, narrowest desktop layout) checks for
// horizontal overflow and runs axe-core in light and dark mode; saves the server
// HTML for vnu and checks the group page weight.
import { createRequire } from "node:module";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { gzipSync } from "node:zlib";

const [tools, groupURL, htmlDir, maxWeight] = process.argv.slice(2);
const require = createRequire(`${tools}/node_modules/`);
const { chromium } = require("playwright-core");
const axeSource = readFileSync(require.resolve("axe-core/axe.min.js"), "utf8");

// Each screen gets the page and returns the navigation response to check, or null
// for an in-page change (htmx swap), whose DOM is then checked as is.
// Screens run in order and share cookies, so later ones can rely on earlier steps.
const submit = (p, selector) => Promise.all([p.waitForNavigation(), p.click(selector)]).then(([r]) => r);
const screens = [
  { name: "picker", go: (p) => p.goto(groupURL) },
  {
    name: "picker-duplicate",
    go: async (p) => {
      await p.fill("#name", "marc");
      return submit(p, "text=Rejoindre");
    },
  },
  {
    name: "group",
    go: (p) => submit(p, 'button:has-text("Léa")'),
    // A card's answer buttons once painted over the fixed « Ajouter » bar and took its clicks.
    check: async (p) => {
      const top = await p.$eval(".mine button", (b) => b.getBoundingClientRect().top);
      await p.setViewportSize({ width: 390, height: Math.round(top) + 30 });
      const hit = await p.$eval(".mine button", (b) => {
        const r = b.getBoundingClientRect();
        return document.elementFromPoint(r.left + r.width / 2, r.top + 5)?.closest(".add-bar") !== null;
      });
      if (!hit) return "an answer button covers the « Ajouter » bar";
      // From 48 rem the answer buttons sit on the right of the ticket's info.
      await p.setViewportSize({ width: 768, height: 844 });
      const beside = await p.$eval(".card", (c) =>
        c.querySelector(".mine").getBoundingClientRect().left >= c.querySelector(".artist").getBoundingClientRect().right,
      );
      await p.setViewportSize({ width: 390, height: 844 });
      return beside ? null : "at 768 px the answer buttons are not beside the ticket's info";
    },
  },
  {
    name: "status-htmx",
    go: async (p) => {
      await p.click("#c1-going");
      await p.waitForSelector('#c1-going[aria-pressed="true"]');
      return null;
    },
  },
  {
    name: "past-open",
    go: async (p) => {
      await p.click(".past summary");
      return null;
    },
  },
  { name: "mine", go: (p) => p.goto(`${groupURL}?mine=1`) },
  { name: "concert", go: (p) => p.goto(`${groupURL}/concerts/1`) },
  {
    // axe skips the content of a closed <details>.
    name: "about-open",
    go: async (p) => {
      await p.click(".about summary");
      return null;
    },
  },
  // Concert 1 is edited twice by the demo (revisions 1 and 2).
  { name: "history", go: (p) => p.goto(`${groupURL}/concerts/1/history`) },
  { name: "saved", go: (p) => p.goto(`${groupURL}/concerts/1?saved=2`) },
  { name: "edit", go: (p) => p.goto(`${groupURL}/concerts/1/edit`) },
  {
    name: "refresh-failed",
    go: async (p) => {
      await p.fill("#ticket_url", "https://concert.invalid/billets");
      await p.click('button:has-text("Relire la billetterie")');
      await p.waitForSelector(".prefill-msg");
      return null;
    },
  },
  { name: "add", go: (p) => p.goto(`${groupURL}/concerts/new`) },
  {
    name: "prefill-failed",
    go: async (p) => {
      await p.fill("#url", "https://concert.invalid/billets");
      await p.click('button:has-text("Pré-remplir")');
      await p.waitForSelector(".prefill-msg");
      return null;
    },
  },
  {
    name: "add-errors",
    go: async (p) => {
      await p.fill("#ticket_url", "ftp://nope");
      // Skip the browser's own checks to reach the server's error page.
      await p.$eval("form.stack", (f) => (f.noValidate = true));
      return submit(p, "text=Enregistrer");
    },
  },
  {
    name: "added",
    go: async (p) => {
      await p.fill("#artist", "Nouveau groupe");
      await p.fill("#date", "2031-05-17");
      await p.fill("#venue", "Le Trabendo");
      await p.fill("#ticket_url", "");
      return submit(p, "text=Enregistrer");
    },
  },
  {
    // The concert added above, again, submitted with Enter: warned, not added.
    name: "add-duplicate",
    go: async (p) => {
      await p.goto(`${groupURL}/concerts/new`);
      await p.fill("#artist", "nouveau groupe");
      await p.fill("#date", "2031-05-17");
      await p.fill("#venue", "Le Trabendo");
      return Promise.all([p.waitForNavigation(), p.press("#venue", "Enter")]).then(([r]) => r);
    },
    check: async (p) =>
      (await p.evaluate(() => document.activeElement?.textContent)) === "Voir ce concert"
        ? null
        : "focus is not on « Voir ce concert »",
  },
  {
    name: "deleted",
    go: async (p) => {
      await p.goto(`${groupURL}/concerts/1`);
      return submit(p, "text=Supprimer");
    },
  },
  // The concert deleted above, as reached from a shared link: 404 page.
  { name: "not-found", go: (p) => p.goto(`${groupURL}/concerts/1`) },
];

mkdirSync(htmlDir, { recursive: true });
const browser = await chromium.launch();
const context = await browser.newContext({ viewport: { width: 390, height: 844 }, bypassCSP: true });
const page = await context.newPage();
let failures = 0;

for (const s of screens) {
  const resp = await s.go(page);
  if (resp && resp.status() >= 500) throw new Error(`${s.name}: HTTP ${resp.status()}`);
  writeFileSync(`${htmlDir}/${s.name}.html`, resp ? await resp.text() : await page.content());
  const problem = await s.check?.(page);
  if (problem) {
    failures++;
    console.log(`check ${s.name}: ${problem}`);
  }
  for (const width of [390, 768]) {
    // Whole page in view: a fixed bar then covers only the bottom padding, unless content
    // really sits under it (axe would otherwise report the bar's colors as a contrast issue).
    await page.setViewportSize({ width, height: 844 });
    const height = await page.evaluate(() => document.documentElement.scrollHeight);
    await page.setViewportSize({ width, height: Math.max(844, height) });
    if (await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)) {
      failures++;
      console.log(`check ${s.name} (${width} px): the page scrolls horizontally`);
    }
    for (const colorScheme of ["light", "dark"]) {
      await page.emulateMedia({ colorScheme });
      await page.evaluate(axeSource);
      const { violations } = await page.evaluate(() =>
        window.axe.run(document, { resultTypes: ["violations"] }),
      );
      for (const v of violations) {
        failures++;
        console.log(`axe ${s.name} (${width} px, ${colorScheme}): ${v.id}: ${v.help}`);
        for (const n of v.nodes) console.log(`  ${n.target.join(" ")}: ${n.failureSummary.split("\n").join(" ")}`);
      }
    }
  }
  await page.setViewportSize({ width: 390, height: 844 });
  console.log(`axe ${s.name}: checked 390 and 768 px, light and dark`);
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
// Headless Chromium skips favicons; fetch the declared icons explicitly.
for (const href of await weightPage.$$eval("link[rel=icon]", (ls) => ls.map((l) => l.href))) {
  sizes[href] = gzipSync(await (await weightPage.request.get(href)).body(), { level: 9 }).length;
}
await browser.close();
const weight = Object.values(sizes).reduce((a, b) => a + b, 0);
console.log(`page weight: ${weight} bytes gzip -9 (${Object.keys(sizes).length} files, limit ${maxWeight})`);
if (weight >= Number(maxWeight)) failures++;

if (failures) {
  console.log(`${failures} failure(s)`);
  process.exit(1);
}
