// Renders the link-preview PNGs from their SVG sources into internal/web/static/; rerun after editing a source.
// Usage: node scripts/og-image.mjs <main checkout>/bin/tools (installed by `make verify-ui`).
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";

const tools = process.argv[2];
process.env.PLAYWRIGHT_BROWSERS_PATH ??= `${tools}/browsers`;
const { chromium } = createRequire(`${tools}/node_modules/`)("playwright-core");
const dir = new URL("..", import.meta.url).pathname;

const browser = await chromium.launch();
for (const [name, width, height] of [["og", 1200, 630], ["apple-touch-icon", 180, 180]]) {
  const page = await browser.newPage({ viewport: { width, height }, deviceScaleFactor: 1 });
  await page.setContent(`<body style="margin:0">${readFileSync(`${dir}scripts/${name}.svg`, "utf8")}`);
  await page.screenshot({ path: `${dir}internal/web/static/${name}.png` });
}
await browser.close();
