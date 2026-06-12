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

// Find a seeded run so the detail page has content.
const board = (await (
  await fetch(`${apiUrl}/v1/leaderboard?suite=sprint&version=1&track=local`)
).json()) as { rows: Array<{ run_id: string }> };
const runId = board.rows[0]?.run_id;

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });

const shots: Array<[string, string]> = [
  ["web-home", `${webUrl}/`],
  ["web-leaderboard", `${webUrl}/leaderboard/sprint/1`],
  ["web-glossary", `${webUrl}/docs/glossary`],
];
if (runId) shots.push(["web-run-detail", `${webUrl}/runs/${runId}`]);

for (const [name, url] of shots) {
  await page.goto(url, { waitUntil: "networkidle" });
  await page.screenshot({ path: `${out}/${name}.png`, fullPage: false });
  console.log(`captured ${name}.png ← ${url}`);
}

await browser.close();
