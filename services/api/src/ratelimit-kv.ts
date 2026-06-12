import {
  DAY_MS,
  DEFAULT_RATE_LIMIT,
  type RateLimiter,
  type RateLimitResult,
} from "./deps";

/** Structural subset of a Workers KVNamespace, so src stays host-agnostic. */
export interface KvLike {
  get(key: string): Promise<string | null>;
  put(key: string, value: string, options?: { expirationTtl?: number }): Promise<void>;
}

/**
 * Sliding-window rate limiter on Workers KV. Same interface as the in-memory
 * implementation; stores the timestamp window as JSON per key. KV is
 * eventually consistent, so this is best-effort — acceptable for abuse
 * throttling, not for billing.
 */
export class KvRateLimiter implements RateLimiter {
  constructor(
    private readonly kv: KvLike,
    private readonly limit: number = DEFAULT_RATE_LIMIT,
    private readonly windowMs: number = DAY_MS,
    private readonly now: () => number = () => Date.now(),
  ) {}

  async check(key: string): Promise<RateLimitResult> {
    const nowMs = this.now();
    const cutoff = nowMs - this.windowMs;
    const storageKey = `rl:${key}`;
    const raw = await this.kv.get(storageKey);
    let recent: number[] = [];
    if (raw !== null) {
      try {
        const parsed: unknown = JSON.parse(raw);
        if (Array.isArray(parsed)) {
          recent = parsed.filter((t): t is number => typeof t === "number" && t > cutoff);
        }
      } catch {
        recent = [];
      }
    }
    if (recent.length >= this.limit) {
      const oldest = recent[0] ?? nowMs;
      const retryAfterSeconds = Math.max(1, Math.ceil((oldest + this.windowMs - nowMs) / 1000));
      return { allowed: false, retryAfterSeconds };
    }
    recent.push(nowMs);
    await this.kv.put(storageKey, JSON.stringify(recent), {
      expirationTtl: Math.ceil(this.windowMs / 1000),
    });
    return { allowed: true, retryAfterSeconds: 0 };
  }
}
