import { Hono } from "hono";
import { and, desc, eq, inArray, count } from "drizzle-orm";
import { validateRunV1, type AimarkRunV1, type AimarkSuiteManifestV1 } from "@aimark/schema";
import { runs, scores, suites, hardwareProfiles } from "./db/schema";
import { canonicalize, randomHex, sha256Hex } from "./canonical";
import { computeScores, hashIp, implausibleMetrics, payloadHash, verifyHmac } from "./pipeline";
import type { Database, Deps } from "./deps";

/**
 * The Hono app is host-agnostic: it only uses the fetch standard. Platform
 * services (DB, blobs, rate limiting) are passed in via createApp(deps) --
 * entrypoints/worker.ts (Cloudflare) and entrypoints/bun.ts (local dev)
 * construct their own implementations.
 */

type RunRow = typeof runs.$inferSelect;

function median(values: number[]): number | null {
  if (values.length === 0) return null;
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  const lower = sorted[mid - 1];
  const upper = sorted[mid];
  if (upper === undefined) return null;
  return sorted.length % 2 === 0 && lower !== undefined ? (lower + upper) / 2 : upper;
}

function parseJsonRecord(text: string | null): Record<string, unknown> | null {
  if (text === null) return null;
  try {
    return JSON.parse(text) as Record<string, unknown>;
  } catch {
    return null;
  }
}

function scoresToMap(rows: { name: string; value: number }[]): Record<string, number> {
  const map: Record<string, number> = {};
  for (const row of rows) map[row.name] = row.value;
  return map;
}

/** Public view of a run -- never exposes ip_hash, claim hash, or integrity. */
function publicRunDetail(run: RunRow, scoreRows: { name: string; value: number }[]) {
  const envelope = parseJsonRecord(run.envelopeJson) as Partial<AimarkRunV1> | null;
  const scoreMap = scoresToMap(scoreRows);
  const { composite, ...subScores } = scoreMap;
  return {
    run_id: run.runId,
    suite: { id: run.suiteId, version: run.suiteVersion },
    track: run.track,
    source: run.source,
    status: run.status,
    flag_reason: run.flagReason,
    hidden: run.hidden === 1,
    created_at: run.createdAt,
    submitted_at: run.submittedAt,
    model: run.model,
    runtime: run.runtime,
    provider: run.provider,
    quantization: run.quantization,
    hardware_profile_id: run.hardwareProfileId,
    hardware_profile: envelope?.environment?.hardware_profile ?? null,
    params: parseJsonRecord(run.paramsJson),
    metrics: envelope?.metrics ?? null,
    cli: envelope?.cli ?? null,
    scores: subScores,
    composite: composite ?? null,
  };
}

async function fetchScores(db: Database, runIds: string[]) {
  if (runIds.length === 0) return [];
  return db
    .select({ runId: scores.runId, name: scores.name, value: scores.value })
    .from(scores)
    .where(inArray(scores.runId, runIds));
}

function clientIp(headers: Headers): string {
  const cf = headers.get("cf-connecting-ip");
  if (cf) return cf.trim();
  const xff = headers.get("x-forwarded-for");
  if (xff) {
    const first = xff.split(",")[0]?.trim();
    if (first) return first;
  }
  return "local";
}

const NOT_IMPLEMENTED_NOTE =
  "Phase 2: GitHub OAuth is not implemented yet (blocked on OAuth app credentials).";

export function createApp(deps: Deps) {
  const app = new Hono();
  const now = deps.now ?? (() => new Date());

  /** Data routes guard on this -- null means the platform binding is absent. */
  const requireDb = (): Database | null => deps.db;
  const dbUnavailable = { error: "database unavailable" } as const;

  app.get("/v1/health", async (c) => {
    let dbOk = false;
    if (deps.db) {
      try {
        await deps.db.select({ n: count() }).from(suites);
        dbOk = true;
      } catch {
        dbOk = false;
      }
    }
    return c.json({ ok: true, service: "aimark-api", schema: "aimark.run.v1", db: dbOk });
  });

  // ---------------------------------------------------------------- POST /runs
  // Submission pipeline (ordered): rate limit -> schema validate -> known suite
  // -> HMAC -> dedup -> plausibility -> canonical score recompute -> persist.
  app.post("/v1/runs", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);

    // 1. Rate limit per (daily-salted) ip hash.
    const ip = clientIp(c.req.raw.headers);
    const ipHash = await hashIp(ip, now());
    const limit = await deps.rateLimiter.check(ipHash);
    if (!limit.allowed) {
      c.header("Retry-After", String(limit.retryAfterSeconds));
      return c.json({ error: "rate limit exceeded", retry_after: limit.retryAfterSeconds }, 429);
    }

    // 2. JSON-Schema validation.
    const body: unknown = await c.req.json().catch(() => null);
    if (body === null || typeof body !== "object") {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    if (!validateRunV1(body)) {
      return c.json({ error: "schema validation failed", details: validateRunV1.errors }, 422);
    }
    const envelope = body as unknown as AimarkRunV1;

    // 3. Known suite/version, and it must be frozen.
    const suiteRows = await db
      .select()
      .from(suites)
      .where(and(eq(suites.id, envelope.suite.id), eq(suites.version, envelope.suite.version)))
      .limit(1);
    const suiteRow = suiteRows[0];
    if (!suiteRow || suiteRow.status !== "frozen") {
      return c.json(
        {
          error: "unknown or non-frozen suite",
          suite: { id: envelope.suite.id, version: envelope.suite.version },
        },
        422,
      );
    }
    const manifest = JSON.parse(suiteRow.manifestJson) as AimarkSuiteManifestV1;

    // 4. HMAC verification (honest-integrity model: mismatch flags, never rejects).
    let status: "accepted" | "flagged" = "accepted";
    let flagReason: string | null = null;
    const hmacOk = await verifyHmac(envelope);
    if (!hmacOk) {
      status = "flagged";
      flagReason = "hmac_mismatch";
    }

    // 5. Payload-hash dedup.
    const payloadSha = await payloadHash(envelope);
    const duplicates = await db
      .select({ runId: runs.runId })
      .from(runs)
      .where(eq(runs.payloadSha256, payloadSha))
      .limit(1);
    const duplicate = duplicates[0];
    if (duplicate) {
      return c.json({ error: "duplicate submission", run_id: duplicate.runId }, 409);
    }
    const sameId = await db
      .select({ runId: runs.runId })
      .from(runs)
      .where(eq(runs.runId, envelope.run_id))
      .limit(1);
    if (sameId[0]) {
      return c.json({ error: "run_id already exists", run_id: envelope.run_id }, 409);
    }

    // 6. Plausibility bounds from the frozen manifest.
    const violations = implausibleMetrics(manifest, envelope.metrics);
    if (violations.length > 0 && status === "accepted") {
      status = "flagged";
      flagReason = "implausible_metrics";
    }

    // 7. Canonical score recompute (client provisional_scores are ignored).
    const scoreMap = computeScores(manifest, envelope.metrics);
    if (scoreMap === null && status === "accepted") {
      status = "flagged";
      flagReason = "no_scorable_metrics";
    }

    // 8. Persist hardware profile (content-addressed), run, and scores.
    const hardwareProfile = envelope.environment?.hardware_profile;
    let hardwareProfileId: string | null = null;
    if (hardwareProfile) {
      hardwareProfileId = hardwareProfile.id ?? (await sha256Hex(canonicalize(hardwareProfile)));
      await db
        .insert(hardwareProfiles)
        .values({ id: hardwareProfileId, profileJson: JSON.stringify(hardwareProfile) })
        .onConflictDoNothing();
    }

    const claimToken = randomHex(32);
    const claimTokenHash = await sha256Hex(claimToken);
    const submittedAt = now().toISOString();

    await db.insert(runs).values({
      runId: envelope.run_id,
      suiteId: envelope.suite.id,
      suiteVersion: envelope.suite.version,
      track: envelope.target.kind,
      source: envelope.source,
      status,
      flagReason,
      payloadSha256: payloadSha,
      createdAt: envelope.created_at,
      submittedAt,
      ipHash,
      claimTokenHash,
      userId: null,
      model: envelope.target.model,
      runtime: envelope.target.runtime ?? null,
      provider: envelope.target.provider ?? null,
      quantization: envelope.target.quantization ?? null,
      hardwareProfileId,
      paramsJson: envelope.target.params ? JSON.stringify(envelope.target.params) : null,
      envelopeJson: JSON.stringify(envelope),
      hidden: 0,
    });
    if (scoreMap) {
      for (const [name, value] of Object.entries(scoreMap)) {
        await db.insert(scores).values({ runId: envelope.run_id, name, value });
      }
    }

    return c.json(
      {
        run_id: envelope.run_id,
        status,
        flag_reason: flagReason,
        scores: scoreMap ?? {},
        claim_token: claimToken,
        public_url: `${deps.baseUrl}/runs/${envelope.run_id}`,
      },
      201,
    );
  });

  // ------------------------------------------------------------- GET /runs/:id
  app.get("/v1/runs/:id", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const id = c.req.param("id");
    const rows = await db.select().from(runs).where(eq(runs.runId, id)).limit(1);
    const run = rows[0];
    if (!run) return c.json({ error: "run not found" }, 404);
    const scoreRows = await fetchScores(db, [id]);
    return c.json(publicRunDetail(run, scoreRows));
  });

  // ----------------------------------------------------------- PATCH /runs/:id
  // Anonymous claim flow: the claim_token returned once at submission proves
  // ownership. Actions: delete (removes the row), hide, show.
  app.patch("/v1/runs/:id", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const id = c.req.param("id");
    const body = (await c.req.json().catch(() => null)) as {
      claim_token?: unknown;
      action?: unknown;
    } | null;
    const claimToken = typeof body?.claim_token === "string" ? body.claim_token : null;
    const action = typeof body?.action === "string" ? body.action : null;
    if (!claimToken || !action || !["delete", "hide", "show"].includes(action)) {
      return c.json({ error: "body must be {claim_token, action: delete|hide|show}" }, 400);
    }
    const rows = await db.select().from(runs).where(eq(runs.runId, id)).limit(1);
    const run = rows[0];
    if (!run) return c.json({ error: "run not found" }, 404);
    const tokenHash = await sha256Hex(claimToken);
    if (tokenHash !== run.claimTokenHash) {
      return c.json({ error: "invalid claim token" }, 403);
    }
    if (action === "delete") {
      await db.delete(scores).where(eq(scores.runId, id));
      await db.delete(runs).where(eq(runs.runId, id));
    } else {
      await db
        .update(runs)
        .set({ hidden: action === "hide" ? 1 : 0 })
        .where(eq(runs.runId, id));
    }
    return c.json({ ok: true, run_id: id, action });
  });

  // ------------------------------------------------------------ GET /leaderboard
  app.get("/v1/leaderboard", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const q = c.req.query();
    const suiteId = q["suite"];
    if (!suiteId) return c.json({ error: "suite query parameter is required" }, 400);

    const conds = [eq(runs.suiteId, suiteId), eq(runs.hidden, 0)];
    if (q["version"] !== undefined) conds.push(eq(runs.suiteVersion, Number(q["version"])));
    if (q["track"]) conds.push(eq(runs.track, q["track"]));
    if (q["model"]) conds.push(eq(runs.model, q["model"]));
    if (q["runtime"]) conds.push(eq(runs.runtime, q["runtime"]));
    if (q["provider"]) conds.push(eq(runs.provider, q["provider"]));
    if (q["quantization"]) conds.push(eq(runs.quantization, q["quantization"]));
    if (q["hardware_profile_id"]) conds.push(eq(runs.hardwareProfileId, q["hardware_profile_id"]));
    // Default leaderboard: user-sourced, accepted runs only.
    if (q["include_ci"] !== "true") conds.push(eq(runs.source, "user"));
    if (q["include_flagged"] !== "true") conds.push(eq(runs.status, "accepted"));

    const limit = Math.min(Math.max(Number(q["limit"] ?? 50) || 50, 1), 200);
    const offset = Math.max(Number(q["offset"] ?? 0) || 0, 0);

    const rows = await db
      .select({ run: runs, composite: scores.value })
      .from(runs)
      .innerJoin(scores, and(eq(scores.runId, runs.runId), eq(scores.name, "composite")))
      .where(and(...conds))
      .orderBy(desc(scores.value))
      .limit(limit)
      .offset(offset);

    const scoreRows = await fetchScores(
      db,
      rows.map((r) => r.run.runId),
    );
    const byRun = new Map<string, Record<string, number>>();
    for (const s of scoreRows) {
      const entry = byRun.get(s.runId) ?? {};
      if (s.name !== "composite") entry[s.name] = s.value;
      byRun.set(s.runId, entry);
    }

    return c.json({
      suite: suiteId,
      version: q["version"] !== undefined ? Number(q["version"]) : null,
      track: q["track"] ?? null,
      rows: rows.map(({ run, composite }) => ({
        run_id: run.runId,
        model: run.model,
        runtime: run.runtime,
        provider: run.provider,
        quantization: run.quantization,
        hardware_profile_id: run.hardwareProfileId,
        params: parseJsonRecord(run.paramsJson),
        scores: byRun.get(run.runId) ?? {},
        composite,
        created_at: run.createdAt,
        source: run.source,
        status: run.status,
      })),
    });
  });

  // ----------------------------------------------------------------- GET /suites
  app.get("/v1/suites", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const rows = await db.select().from(suites);
    return c.json({
      suites: rows.map((row) => {
        const manifest = JSON.parse(row.manifestJson) as AimarkSuiteManifestV1;
        return {
          id: row.id,
          version: row.version,
          status: row.status,
          title: manifest.title,
          tracks: manifest.tracks,
          task_count: manifest.tasks.length,
        };
      }),
    });
  });

  // ---------------------------------------------------------------- GET /compare
  app.get("/v1/compare", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const idsParam = c.req.query("ids") ?? "";
    const ids = idsParam
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
    if (ids.length === 0) return c.json({ error: "ids query parameter is required" }, 400);
    if (ids.length > 5) return c.json({ error: "compare supports at most 5 runs" }, 400);
    const rows = await db.select().from(runs).where(inArray(runs.runId, ids));
    const scoreRows = await fetchScores(
      db,
      rows.map((r) => r.runId),
    );
    const byId = new Map(rows.map((r) => [r.runId, r]));
    const details = ids
      .map((id) => byId.get(id))
      .filter((r): r is RunRow => r !== undefined)
      .map((r) =>
        publicRunDetail(
          r,
          scoreRows.filter((s) => s.runId === r.runId),
        ),
      );
    return c.json({ runs: details });
  });

  // ------------------------------------------------- GET /models, GET /hardware
  // Default leaderboard population (accepted, user-sourced, visible) grouped
  // by dimension with run counts + median composite per (suite, version, track).
  const aggregateBy = async (db: Database, pick: (run: RunRow) => string | null) => {
    const rows = await db
      .select({ run: runs, composite: scores.value })
      .from(runs)
      .innerJoin(scores, and(eq(scores.runId, runs.runId), eq(scores.name, "composite")))
      .where(and(eq(runs.hidden, 0), eq(runs.source, "user"), eq(runs.status, "accepted")));
    interface Group {
      key: string;
      suite: string;
      version: number;
      track: string;
      composites: number[];
    }
    const groups = new Map<string, Group>();
    for (const { run, composite } of rows) {
      const dim = pick(run);
      if (dim === null) continue;
      const mapKey = `${dim}:${run.suiteId}:${run.suiteVersion}:${run.track}`;
      const entry = groups.get(mapKey) ?? {
        key: dim,
        suite: run.suiteId,
        version: run.suiteVersion,
        track: run.track,
        composites: [],
      };
      entry.composites.push(composite);
      groups.set(mapKey, entry);
    }
    return Array.from(groups.values())
      .map(({ key, suite, version, track, composites }) => ({
        key,
        suite,
        version,
        track,
        n: composites.length,
        median_composite: median(composites),
      }))
      .sort((a, b) => a.key.localeCompare(b.key));
  };

  app.get("/v1/models", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const agg = await aggregateBy(db, (run) => run.model);
    return c.json({ models: agg.map(({ key, ...rest }) => ({ model: key, ...rest })) });
  });

  app.get("/v1/hardware", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const agg = await aggregateBy(db, (run) => run.hardwareProfileId);
    return c.json({
      hardware: agg.map(({ key, ...rest }) => ({ hardware_profile_id: key, ...rest })),
    });
  });

  // ----------------------------------------------------------- GET /params/impact
  // Simple, honest effect summary: group accepted user runs by one captured
  // parameter's value and report n + median composite per value. No causal
  // claims -- co-variates are not controlled.
  app.get("/v1/params/impact", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const q = c.req.query();
    const suiteId = q["suite"];
    const param = q["param"];
    if (!suiteId || !param) {
      return c.json({ error: "suite and param query parameters are required" }, 400);
    }
    const conds = [
      eq(runs.suiteId, suiteId),
      eq(runs.hidden, 0),
      eq(runs.source, "user"),
      eq(runs.status, "accepted"),
    ];
    if (q["version"] !== undefined) conds.push(eq(runs.suiteVersion, Number(q["version"])));
    if (q["track"]) conds.push(eq(runs.track, q["track"]));
    const rows = await db
      .select({ run: runs, composite: scores.value })
      .from(runs)
      .innerJoin(scores, and(eq(scores.runId, runs.runId), eq(scores.name, "composite")))
      .where(and(...conds));
    const groups = new Map<string, number[]>();
    for (const { run, composite } of rows) {
      const params = parseJsonRecord(run.paramsJson);
      const value = params?.[param];
      if (value === undefined || value === null) continue;
      const key = String(value);
      const list = groups.get(key) ?? [];
      list.push(composite);
      groups.set(key, list);
    }
    return c.json({
      suite: suiteId,
      version: q["version"] !== undefined ? Number(q["version"]) : null,
      track: q["track"] ?? null,
      param,
      groups: Array.from(groups.entries())
        .map(([value, composites]) => ({
          value,
          n: composites.length,
          median_composite: median(composites),
        }))
        .sort((a, b) => a.value.localeCompare(b.value)),
    });
  });

  // ------------------------------------------------ POST /admin/refresh-aggregates
  // Leaderboard queries are computed live (data volume is tiny at this phase),
  // so this validates the admin token and returns row counts. The endpoint is
  // the seam where a precomputed leaderboard_agg refresh plugs in later.
  app.post("/v1/admin/refresh-aggregates", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    if (!deps.adminToken) {
      return c.json({ error: "admin token not configured" }, 503);
    }
    const auth = c.req.header("authorization");
    const bearer = auth?.startsWith("Bearer ") ? auth.slice("Bearer ".length) : null;
    const provided = bearer ?? c.req.header("x-admin-token") ?? null;
    if (provided !== deps.adminToken) {
      return c.json({ error: "unauthorized" }, 401);
    }
    const [runCount] = await db.select({ n: count() }).from(runs);
    const [scoreCount] = await db.select({ n: count() }).from(scores);
    const [suiteCount] = await db.select({ n: count() }).from(suites);
    return c.json({
      ok: true,
      refreshed: false,
      note: "leaderboard queries are live; no aggregate table to refresh yet",
      counts: {
        runs: runCount?.n ?? 0,
        scores: scoreCount?.n ?? 0,
        suites: suiteCount?.n ?? 0,
      },
    });
  });

  // -------------------------------------------------------------- Phase 2 stubs
  app.get("/v1/me", (c) => c.json({ error: "not_implemented", note: NOT_IMPLEMENTED_NOTE }, 501));
  app.get("/v1/auth/github", (c) =>
    c.json({ error: "not_implemented", note: NOT_IMPLEMENTED_NOTE }, 501),
  );
  app.get("/v1/auth/github/callback", (c) =>
    c.json({ error: "not_implemented", note: NOT_IMPLEMENTED_NOTE }, 501),
  );

  return app;
}

export type App = ReturnType<typeof createApp>;
