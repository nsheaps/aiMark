/**
 * Captures website screenshots for the docs (used by scripts/capture-docs-images.sh).
 *
 *   CAPTURE_WEB_URL=http://localhost:4322 CAPTURE_API_URL=http://localhost:8789 \
 *     bun tools/screenshots/capture.ts --out docs/images
 *
 * Requires `bunx playwright install chromium` beforehand.
 */
import { chromium } from "playwright";
import { mkdirSync } from "node:fs";

const out = process.argv.includes("--out")
  ? process.argv[process.argv.indexOf("--out") + 1]!
  : "docs/images";
const webUrl = process.env.CAPTURE_WEB_URL ?? "http://localhost:4321";
const apiUrl = process.env.CAPTURE_API_URL ?? "http://localhost:8787";

mkdirSync(out, { recursive: true });

// Find a seeded benchmark so the system-detail page has content.
const systems = (await (await fetch(
  `${apiUrl}/v1/leaderboard/systems?program=bench&version=1&class=ultra`,
)).json().catch(() => ({ rows: [] }))) as { rows: Array<{ bench_id: string }> };
const benchId = systems.rows[0]?.bench_id;

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });

const shots: Array<[string, string]> = [
  ["web-home", `${webUrl}/`],
  ["web-leaderboard-systems", `${webUrl}/leaderboard/systems/ultra/`],
  ["web-model-fit", `${webUrl}/model-fit/`],
  ["web-glossary", `${webUrl}/docs/glossary/`],
];
if (benchId) shots.push(["web-bench-detail", `${webUrl}/bench/?id=${encodeURIComponent(benchId)}`]);

for (const [name, url] of shots) {
  await page.goto(url, { waitUntil: "networkidle" });
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: false });
  console.log(`captured ${name}.png ← ${url}`);
}

await browser.close();
