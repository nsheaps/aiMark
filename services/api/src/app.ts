import { Hono } from "hono";
import { validateRunV1 } from "@aimark/schema";

/**
 * The Hono app is host-agnostic: it only uses the fetch standard.
 * Entrypoints (entrypoints/worker.ts for Cloudflare, entrypoints/bun.ts for
 * local dev) adapt it to their platform.
 */
export const app = new Hono();

app.get("/v1/health", (c) => c.json({ ok: true, service: "aimark-api", schema: "aimark.run.v1" }));

// Phase 1 replaces this stub with the full submission pipeline:
// schema validate → HMAC verify → dedup → plausibility → score recompute → persist.
app.post("/v1/runs", async (c) => {
  const body = await c.req.json().catch(() => null);
  if (body === null) {
    return c.json({ error: "invalid JSON body" }, 400);
  }
  if (!validateRunV1(body)) {
    return c.json({ error: "schema validation failed", details: validateRunV1.errors }, 422);
  }
  return c.json({ error: "submissions are not open yet" }, 501);
});
