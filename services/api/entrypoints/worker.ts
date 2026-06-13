import type { D1Database, KVNamespace, R2Bucket } from "@cloudflare/workers-types";
import { drizzle } from "drizzle-orm/d1";
import { createApp, type App } from "../src/app";
import * as schema from "../src/db/schema";
import { seedPrograms, seedSuites } from "../src/db/seed";
import { DEFAULT_RATE_LIMIT, InMemoryBlobStore, InMemoryRateLimiter } from "../src/deps";
import { KvRateLimiter } from "../src/ratelimit-kv";
import { R2BlobStore } from "../src/blobs-r2";

/**
 * Cloudflare Workers entrypoint. D1 migrations are applied at deploy time via
 * `wrangler d1 migrations apply` (see drizzle/README.md) — this entrypoint
 * only seeds suite manifests. If the DB binding is absent (resources not yet
 * provisioned) the worker still serves; data routes return 503.
 */

interface Env {
  DB?: D1Database;
  RATE_LIMITS?: KVNamespace;
  ARTIFACTS?: R2Bucket;
  BASE_URL?: string;
  ADMIN_TOKEN?: string;
  RATE_LIMIT?: string;
  OUTLIER_SIGMA?: string;
  OUTLIER_MIN_COHORT?: string;
}

let cached: { app: App; seeded: Promise<void> } | null = null;

function init(env: Env): { app: App; seeded: Promise<void> } {
  const db = env.DB ? drizzle(env.DB, { schema }) : null;
  const limit = Number(env.RATE_LIMIT ?? DEFAULT_RATE_LIMIT);
  const app = createApp({
    db,
    // With the ARTIFACTS R2 binding present, artifact bytes persist in R2.
    // Uploads still PUT through the worker's /v1/artifacts/:key route — true
    // presigned R2 URLs need account-scoped S3 API tokens we don't provision;
    // see src/blobs-r2.ts. Without the binding, the in-memory store keeps the
    // routes functional (artifacts don't survive worker restarts).
    blobs: env.ARTIFACTS ? new R2BlobStore(env.ARTIFACTS) : new InMemoryBlobStore(),
    rateLimiter: env.RATE_LIMITS
      ? new KvRateLimiter(env.RATE_LIMITS, limit)
      : new InMemoryRateLimiter(limit),
    baseUrl: env.BASE_URL ?? "https://aimark.dev",
    adminToken: env.ADMIN_TOKEN,
    outlierSigma: env.OUTLIER_SIGMA ? Number(env.OUTLIER_SIGMA) : undefined,
    outlierMinCohort: env.OUTLIER_MIN_COHORT ? Number(env.OUTLIER_MIN_COHORT) : undefined,
  });
  const seeded = db ? seedSuites(db).then(() => seedPrograms(db)) : Promise.resolve();
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
