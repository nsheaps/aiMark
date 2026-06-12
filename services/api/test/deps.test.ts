import { describe, expect, test } from "bun:test";
import { createApp } from "../src/app";
import { InMemoryBlobStore, InMemoryRateLimiter } from "../src/deps";
import { KvRateLimiter, type KvLike } from "../src/ratelimit-kv";

describe("db-less app (worker without a D1 binding)", () => {
  const app = createApp({
    db: null,
    blobs: new InMemoryBlobStore(),
    rateLimiter: new InMemoryRateLimiter(),
    baseUrl: "http://test.local",
  });

  test("health still serves with db:false", async () => {
    const res = await app.request("/v1/health");
    expect(res.status).toBe(200);
    const body = (await res.json()) as { ok: boolean; db: boolean };
    expect(body.ok).toBe(true);
    expect(body.db).toBe(false);
  });

  test("data routes return 503", async () => {
    for (const [path, method] of [
      ["/v1/runs", "POST"],
      ["/v1/runs/01AAAAAAAAAAAAAAAAAAAAAAAA", "GET"],
      ["/v1/leaderboard?suite=sprint", "GET"],
      ["/v1/suites", "GET"],
      ["/v1/compare?ids=a", "GET"],
      ["/v1/models", "GET"],
      ["/v1/hardware", "GET"],
      ["/v1/params/impact?suite=sprint&param=num_ctx", "GET"],
      ["/v1/admin/refresh-aggregates", "POST"],
    ] as const) {
      const res = await app.request(path, { method });
      expect(res.status).toBe(503);
    }
  });
});

describe("InMemoryRateLimiter", () => {
  test("sliding window frees slots as old hits expire", async () => {
    let nowMs = 1_000_000;
    const limiter = new InMemoryRateLimiter(2, 1000, () => nowMs);

    expect((await limiter.check("k")).allowed).toBe(true);
    nowMs += 400;
    expect((await limiter.check("k")).allowed).toBe(true);
    const denied = await limiter.check("k");
    expect(denied.allowed).toBe(false);
    expect(denied.retryAfterSeconds).toBeGreaterThan(0);

    nowMs += 700; // first hit (t=1_000_000) now outside the 1s window
    expect((await limiter.check("k")).allowed).toBe(true);
  });

  test("keys are independent", async () => {
    const limiter = new InMemoryRateLimiter(1, 1000, () => 0);
    expect((await limiter.check("a")).allowed).toBe(true);
    expect((await limiter.check("a")).allowed).toBe(false);
    expect((await limiter.check("b")).allowed).toBe(true);
  });
});

describe("KvRateLimiter", () => {
  test("same semantics on a KV-like store", async () => {
    const store = new Map<string, string>();
    const kv: KvLike = {
      get: (key) => Promise.resolve(store.get(key) ?? null),
      put: (key, value) => {
        store.set(key, value);
        return Promise.resolve();
      },
    };
    let nowMs = 5_000_000;
    const limiter = new KvRateLimiter(kv, 2, 1000, () => nowMs);

    expect((await limiter.check("k")).allowed).toBe(true);
    expect((await limiter.check("k")).allowed).toBe(true);
    expect((await limiter.check("k")).allowed).toBe(false);
    nowMs += 1100;
    expect((await limiter.check("k")).allowed).toBe(true);
  });
});
