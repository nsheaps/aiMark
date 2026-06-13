import type { BaseSQLiteDatabase } from "drizzle-orm/sqlite-core";
import type * as schema from "./db/schema";

/**
 * Platform services the Hono app depends on. Entrypoints construct concrete
 * implementations (bun:sqlite + in-memory locally, D1 + KV on Workers) and
 * pass them to createApp() — the app itself stays fetch-standard only.
 */

/** Works for both drizzle's bun:sqlite (sync) and D1 (async) drivers. */
export type Database = BaseSQLiteDatabase<"sync" | "async", unknown, typeof schema>;

export interface RateLimitResult {
  allowed: boolean;
  /** Seconds until the next request would be allowed (when not allowed). */
  retryAfterSeconds: number;
}

export interface RateLimiter {
  /** Records an attempt for `key` and reports whether it is allowed. */
  check(key: string): Promise<RateLimitResult>;
}

export interface BlobStore {
  put(key: string, data: Uint8Array): Promise<void>;
  get(key: string): Promise<Uint8Array | null>;
  /** Cheap existence check (Map lookup / R2 head) — guards re-uploads. */
  exists(key: string): Promise<boolean>;
}

export interface Deps {
  /** null when the platform binding is absent — data routes return 503. */
  db: Database | null;
  blobs: BlobStore;
  rateLimiter: RateLimiter;
  /** Clock override for tests; defaults to Date.now-based. */
  now?: () => Date;
  /** Public base URL used to build public_url in responses. */
  baseUrl: string;
  /** Token guarding POST /v1/admin/* routes; unset disables admin routes. */
  adminToken?: string;
  /** Cross-cohort outlier flagging: σ threshold (default 4.0). */
  outlierSigma?: number;
  /** Minimum cohort size before outlier flagging applies (default 8). */
  outlierMinCohort?: number;
}

export const DEFAULT_RATE_LIMIT = 20;
export const DAY_MS = 24 * 60 * 60 * 1000;
export const DEFAULT_OUTLIER_SIGMA = 4.0;
export const DEFAULT_OUTLIER_MIN_COHORT = 8;

/** Sliding-window limiter backed by an in-memory Map (bun dev/tests). */
export class InMemoryRateLimiter implements RateLimiter {
  private readonly hits = new Map<string, number[]>();

  constructor(
    private readonly limit: number = DEFAULT_RATE_LIMIT,
    private readonly windowMs: number = DAY_MS,
    private readonly now: () => number = () => Date.now(),
  ) {}

  check(key: string): Promise<RateLimitResult> {
    const nowMs = this.now();
    const cutoff = nowMs - this.windowMs;
    const recent = (this.hits.get(key) ?? []).filter((t) => t > cutoff);
    if (recent.length >= this.limit) {
      this.hits.set(key, recent);
      const oldest = recent[0] ?? nowMs;
      const retryAfterSeconds = Math.max(1, Math.ceil((oldest + this.windowMs - nowMs) / 1000));
      return Promise.resolve({ allowed: false, retryAfterSeconds });
    }
    recent.push(nowMs);
    this.hits.set(key, recent);
    return Promise.resolve({ allowed: true, retryAfterSeconds: 0 });
  }
}

/** In-memory blob store (bun dev/tests). R2 replaces this on Workers. */
export class InMemoryBlobStore implements BlobStore {
  private readonly blobs = new Map<string, Uint8Array>();

  put(key: string, data: Uint8Array): Promise<void> {
    this.blobs.set(key, data);
    return Promise.resolve();
  }

  get(key: string): Promise<Uint8Array | null> {
    return Promise.resolve(this.blobs.get(key) ?? null);
  }

  exists(key: string): Promise<boolean> {
    return Promise.resolve(this.blobs.has(key));
  }
}
