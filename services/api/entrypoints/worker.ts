import type { D1Database, KVNamespace } from "@cloudflare/workers-types";
import { drizzle } from "drizzle-orm/d1";
import { createApp, type App } from "../src/app";
import * as schema from "../src/db/schema";
import { seedSuites } from "../src/db/seed";
import { DEFAULT_RATE_LIMIT, InMemoryBlobStore, InMemoryRateLimiter } from "../src/deps";
import { KvRateLimiter } from "../src/ratelimit-kv";

/**
 * Cloudflare Workers entrypoint. D1 migrations are applied at deploy time via
 * `wrangler d1 migrations apply` (see drizzle/README.md) — this entrypoint
 * only seeds suite manifests. If the DB binding is absent (resources not yet
 * provisioned) the worker still serves; data routes return 503.
 */

interface Env {
  DB?: D1Database;
  RATE_LIMITS?: KVNamespace;
  BASE_URL?: string;
  ADMIN_TOKEN?: string;
  RATE_LIMIT?: string;
}

let cached: { app: App; seeded: Promise<void> } | null = null;

function init(env: Env): { app: App; seeded: Promise<void> } {
  const db = env.DB ? drizzle(env.DB, { schema }) : null;
  const limit = Number(env.RATE_LIMIT ?? DEFAULT_RATE_LIMIT);
  const app = createApp({
    db,
    blobs: new InMemoryBlobStore(), // R2 binding replaces this when artifacts land
    rateLimiter: env.RATE_LIMITS
      ? new KvRateLimiter(env.RATE_LIMITS, limit)
      : new InMemoryRateLimiter(limit),
    baseUrl: env.BASE_URL ?? "https://aimark.dev",
    adminToken: env.ADMIN_TOKEN,
  });
  const seeded = db ? seedSuites(db) : Promise.resolve();
  return { app, seeded };
}

export default {
  async fetch(request: Request, env: Env, ctx: unknown): Promise<Response> {
    cached ??= init(env);
    await cached.seeded.catch(() => {
      // Migrations not applied yet — data routes will surface errors; keep serving.
    });
    return cached.app.fetch(request, env, ctx as Parameters<App["fetch"]>[2]);
  },
};
