import { describe, expect, test } from "bun:test";
import {
  createTestApp,
  makeValidRun,
  submit,
  submitOk,
  TEST_ADMIN_TOKEN,
  TEST_BASE_URL,
} from "./helpers";

describe("health", () => {
  test("GET /v1/health reports db ok", async () => {
    const app = await createTestApp();
    const res = await app.request("/v1/health");
    expect(res.status).toBe(200);
    const body = (await res.json()) as { ok: boolean; db: boolean };
    expect(body.ok).toBe(true);
    expect(body.db).toBe(true);
  });
});

describe("submission pipeline", () => {
  test("happy path: valid signed run -> 201 accepted with canonical scores", async () => {
    const app = await createTestApp();
    const result = await submitOk(app);
    expect(result.status).toBe("accepted");
    expect(result.flag_reason).toBeNull();
    expect(result.run_id).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
    // Fixture has decode_tps_mean + ttft_ms_p50 -> performance computable;
    // no CV metrics -> consistency sub-score skipped.
    expect(result.scores["performance"]).toBeGreaterThan(0);
    expect(result.scores["composite"]).toBeGreaterThan(0);
    expect(result.scores["consistency"]).toBeUndefined();
    expect(result.claim_token).toMatch(/^[0-9a-f]{64}$/);
    expect(result.public_url).toBe(`${TEST_BASE_URL}/runs/${result.run_id}`);
  });

  test("server recompute ignores client provisional_scores", async () => {
    const app = await createTestApp();
    const result = await submitOk(app, (env) => {
      env.provisional_scores = { performance: 999999, composite: 999999 };
    });
    expect(result.scores["performance"]).toBeLessThan(999999);
    expect(result.scores["composite"]).toBeLessThan(999999);
  });

  test("invalid JSON body -> 400", async () => {
    const app = await createTestApp();
    const res = await app.request("/v1/runs", {
      method: "POST",
      body: "{not json",
      headers: { "content-type": "application/json" },
    });
    expect(res.status).toBe(400);
  });

  test("schema-invalid payload -> 422 with details", async () => {
    const app = await createTestApp();
    const res = await submit(app, { nope: true });
    expect(res.status).toBe(422);
    const body = (await res.json()) as { details: unknown[] };
    expect(Array.isArray(body.details)).toBe(true);
  });

  test("unknown suite/version -> 422", async () => {
    const app = await createTestApp();
    const unknownSuite = await makeValidRun((env) => {
      env.suite.id = "nonexistent";
    });
    const res = await submit(app, unknownSuite);
    expect(res.status).toBe(422);

    const unknownVersion = await makeValidRun((env) => {
      env.suite.version = 99;
    });
    const res2 = await submit(app, unknownVersion);
    expect(res2.status).toBe(422);
  });

  test("duplicate payload_sha256 -> 409 with existing run_id", async () => {
    const app = await createTestApp();
    const run = await makeValidRun();
    const first = await submit(app, run);
    expect(first.status).toBe(201);
    const second = await submit(app, run);
    expect(second.status).toBe(409);
    const body = (await second.json()) as { run_id: string };
    expect(body.run_id).toBe(run.run_id);
  });

  test("hmac mismatch -> accepted into db but flagged, not rejected", async () => {
    const app = await createTestApp();
    const run = await makeValidRun();
    run.metrics["decode_tps_mean"] = 200; // tamper after signing
    const res = await submit(app, run);
    expect(res.status).toBe(201);
    const body = (await res.json()) as { status: string; flag_reason: string };
    expect(body.status).toBe("flagged");
    expect(body.flag_reason).toBe("hmac_mismatch");
  });

  test("missing integrity block -> flagged hmac_mismatch", async () => {
    const app = await createTestApp();
    const run = await makeValidRun();
    delete run.integrity;
    const res = await submit(app, run);
    expect(res.status).toBe(201);
    const body = (await res.json()) as { status: string; flag_reason: string };
    expect(body.status).toBe("flagged");
    expect(body.flag_reason).toBe("hmac_mismatch");
  });

  test("implausible metrics -> flagged implausible_metrics", async () => {
    const app = await createTestApp();
    // Signed correctly, but decode_tps_mean exceeds the manifest max of 50000.
    const result = await submitOk(app, (env) => {
      env.metrics["decode_tps_mean"] = 999999;
    });
    expect(result.status).toBe("flagged");
    expect(result.flag_reason).toBe("implausible_metrics");
  });

  test("rate limit: 429 with Retry-After once the window is full", async () => {
    const app = await createTestApp({ rateLimit: 3 });
    for (let i = 0; i < 3; i++) {
      const res = await submit(app, await makeValidRun());
      expect(res.status).toBe(201);
    }
    const res = await submit(app, await makeValidRun());
    expect(res.status).toBe(429);
    expect(Number(res.headers.get("retry-after"))).toBeGreaterThan(0);
  });
});

describe("run detail + claim flow", () => {
  test("GET /v1/runs/:id returns public subset, never secrets", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);
    const res = await app.request(`/v1/runs/${submitted.run_id}`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as Record<string, unknown>;
    expect(body["run_id"]).toBe(submitted.run_id);
    expect(body["status"]).toBe("accepted");
    expect(body["model"]).toBe("llama3.1:8b-instruct-q4_K_M");
    expect(body["scores"]).toBeDefined();
    expect(body["metrics"]).toBeDefined();
    const serialized = JSON.stringify(body);
    expect(serialized).not.toContain("ip_hash");
    expect(serialized).not.toContain("ipHash");
    expect(serialized).not.toContain("claim");
    expect(serialized).not.toContain("integrity");
    expect(serialized).not.toContain("hmac");
  });

  test("GET /v1/runs/:id -> 404 for unknown run", async () => {
    const app = await createTestApp();
    const res = await app.request("/v1/runs/01AAAAAAAAAAAAAAAAAAAAAAAA");
    expect(res.status).toBe(404);
  });

  test("PATCH delete with the claim token removes the run; wrong token -> 403", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);

    const wrong = await app.request(`/v1/runs/${submitted.run_id}`, {
      method: "PATCH",
      body: JSON.stringify({ claim_token: "0".repeat(64), action: "delete" }),
      headers: { "content-type": "application/json" },
    });
    expect(wrong.status).toBe(403);

    const right = await app.request(`/v1/runs/${submitted.run_id}`, {
      method: "PATCH",
      body: JSON.stringify({ claim_token: submitted.claim_token, action: "delete" }),
      headers: { "content-type": "application/json" },
    });
    expect(right.status).toBe(200);

    const gone = await app.request(`/v1/runs/${submitted.run_id}`);
    expect(gone.status).toBe(404);
  });

  test("PATCH hide removes from leaderboard, show restores", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);

    const boardUrl = "/v1/leaderboard?suite=sprint&version=1&track=local";
    const before = (await (await app.request(boardUrl)).json()) as { rows: unknown[] };
    expect(before.rows.length).toBe(1);

    const hide = await app.request(`/v1/runs/${submitted.run_id}`, {
      method: "PATCH",
      body: JSON.stringify({ claim_token: submitted.claim_token, action: "hide" }),
      headers: { "content-type": "application/json" },
    });
    expect(hide.status).toBe(200);
    const hidden = (await (await app.request(boardUrl)).json()) as { rows: unknown[] };
    expect(hidden.rows.length).toBe(0);

    const show = await app.request(`/v1/runs/${submitted.run_id}`, {
      method: "PATCH",
      body: JSON.stringify({ claim_token: submitted.claim_token, action: "show" }),
      headers: { "content-type": "application/json" },
    });
    expect(show.status).toBe(200);
    const restored = (await (await app.request(boardUrl)).json()) as { rows: unknown[] };
    expect(restored.rows.length).toBe(1);
  });

  test("PATCH with bad action -> 400", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);
    const res = await app.request(`/v1/runs/${submitted.run_id}`, {
      method: "PATCH",
      body: JSON.stringify({ claim_token: submitted.claim_token, action: "promote" }),
      headers: { "content-type": "application/json" },
    });
    expect(res.status).toBe(400);
  });
});

describe("admin + phase 2 stubs", () => {
  test("POST /v1/admin/refresh-aggregates requires the admin token", async () => {
    const app = await createTestApp();
    await submitOk(app);

    const unauthorized = await app.request("/v1/admin/refresh-aggregates", { method: "POST" });
    expect(unauthorized.status).toBe(401);

    const wrong = await app.request("/v1/admin/refresh-aggregates", {
      method: "POST",
      headers: { authorization: "Bearer wrong" },
    });
    expect(wrong.status).toBe(401);

    const ok = await app.request("/v1/admin/refresh-aggregates", {
      method: "POST",
      headers: { authorization: `Bearer ${TEST_ADMIN_TOKEN}` },
    });
    expect(ok.status).toBe(200);
    const body = (await ok.json()) as { counts: { runs: number; suites: number } };
    expect(body.counts.runs).toBe(1);
    expect(body.counts.suites).toBeGreaterThanOrEqual(1);
  });

  test("GET /v1/me and OAuth routes are honest 501 stubs", async () => {
    const app = await createTestApp();
    for (const path of ["/v1/me", "/v1/auth/github", "/v1/auth/github/callback"]) {
      const res = await app.request(path);
      expect(res.status).toBe(501);
      const body = (await res.json()) as { error: string; note: string };
      expect(body.error).toBe("not_implemented");
      expect(body.note).toContain("OAuth");
    }
  });
});
