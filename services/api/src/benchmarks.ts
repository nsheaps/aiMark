import type { Hono } from "hono";
import { and, desc, eq, gte, inArray } from "drizzle-orm";
import {
  validateBenchmarkV1,
  type AimarkBenchmarkProgramV1,
  type AimarkBenchmarkV1,
  type AimarkRunV1,
} from "@aimark/schema";
import { benchmarkScores, benchmarks, hardwareProfiles, programs, runs, scores } from "./db/schema";
import {
  canonicalize,
  hmacSha256Hex,
  randomHex,
  sha256Hex,
  DEV_HMAC_KEY,
  DEV_KEY_GEN,
} from "./canonical";
import { hashIp, round2 } from "./pipeline";
import { clientIp, median, parseJsonRecord, weightedGeomean } from "./util";
import type { Database, Deps } from "./deps";

/**
 * Zero-choice benchmark routes ("3DMark mode"). A benchmark.v1 envelope ties
 * together cell runs (run.v1 rows sharing bench_id) into one System Score:
 *
 *   POST  /v1/benchmarks              submit pipeline (mirrors /v1/runs)
 *   GET   /v1/benchmarks/:id          public detail
 *   PATCH /v1/benchmarks/:id          anonymous claim (delete/hide/show)
 *   GET   /v1/leaderboard/systems     THE headline board: systems per class
 *   GET   /v1/model-fit               model x hardware buying-guidance matrix
 *   GET   /v1/monitor/series          hosted-API monitoring reference series
 */

type BenchmarkRow = typeof benchmarks.$inferSelect;
type ProgramCell = AimarkBenchmarkProgramV1["cells"][number];

const SYSTEM_SUB_SCORES = ["performance", "consistency"] as const;

/** Usability badge thresholds (median decode tok/s) for /v1/model-fit. */
export const FIT_INSTANT_TPS = 30;
export const FIT_USABLE_TPS = 10;

export function usabilityBadge(decodeTps: number): "instant" | "usable" | "painful" {
  if (decodeTps >= FIT_INSTANT_TPS) return "instant";
  if (decodeTps >= FIT_USABLE_TPS) return "usable";
  return "painful";
}

/** Same honest-integrity model as runs: HMAC over canonical JSON minus the
 * integrity block, dev key — mismatch flags, never rejects. */
async function verifyBenchHmac(envelope: AimarkBenchmarkV1): Promise<boolean> {
  const integrity = envelope.integrity;
  if (!integrity?.hmac || integrity.key_gen !== DEV_KEY_GEN) return false;
  const { integrity: _integrity, ...rest } = envelope;
  const expected = await hmacSha256Hex(DEV_HMAC_KEY, canonicalize(rest));
  return integrity.hmac === expected;
}

async function benchPayloadHash(envelope: AimarkBenchmarkV1): Promise<string> {
  return envelope.integrity?.payload_sha256 ?? (await sha256Hex(canonicalize(envelope)));
}

/** Program cells applicable to a class: class '*' plus exact matches. */
function cellsForClass(manifest: AimarkBenchmarkProgramV1, cls: string): Map<string, ProgramCell> {
  const map = new Map<string, ProgramCell>();
  for (const cell of manifest.cells) {
    if (cell.class === "*" || cell.class === cls) map.set(cell.id, cell);
  }
  return map;
}

interface HardwareSummary {
  cpu_model: string | null;
  gpu: string | null;
  gpu_vram_gb: number | null;
  ram_gb: number | null;
  unified_memory: boolean | null;
}

/** Compact hardware line for leaderboard rows, from the stored envelope. */
function hardwareSummary(envelope: Partial<AimarkBenchmarkV1> | null): HardwareSummary {
  const hw = envelope?.environment?.hardware_profile;
  const gpu = hw?.gpus?.[0];
  return {
    cpu_model: hw?.cpu_model ?? null,
    gpu: gpu?.name ?? null,
    gpu_vram_gb: gpu?.vram_gb ?? null,
    ram_gb: hw?.ram_gb ?? null,
    unified_memory: hw?.unified_memory ?? null,
  };
}

async function fetchBenchScores(db: Database, benchIds: string[]) {
  if (benchIds.length === 0) return [];
  return db
    .select({
      benchId: benchmarkScores.benchId,
      name: benchmarkScores.name,
      value: benchmarkScores.value,
    })
    .from(benchmarkScores)
    .where(inArray(benchmarkScores.benchId, benchIds));
}

export function registerBenchmarkRoutes(app: Hono, deps: Deps, now: () => Date): void {
  const requireDb = (): Database | null => deps.db;
  const dbUnavailable = { error: "database unavailable" } as const;

  // ---------------------------------------------------------- POST /benchmarks
  // Pipeline (ordered, mirrors /v1/runs): rate limit -> schema validate ->
  // known frozen program -> class exists -> HMAC (flag, never reject) ->
  // payload dedup -> cell verification -> server recompute -> persist.
  app.post("/v1/benchmarks", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);

    // 1. Rate limit — same limiter + daily-salted ip hash as /v1/runs.
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
    if (!validateBenchmarkV1(body)) {
      return c.json(
        { error: "schema validation failed", details: validateBenchmarkV1.errors },
        422,
      );
    }
    const envelope = body as unknown as AimarkBenchmarkV1;

    // 3. Known program/version, and it must be frozen.
    const programRows = await db
      .select()
      .from(programs)
      .where(
        and(eq(programs.id, envelope.program.id), eq(programs.version, envelope.program.version)),
      )
      .limit(1);
    const programRow = programRows[0];
    if (!programRow || programRow.status !== "frozen") {
      return c.json(
        {
          error: "unknown or non-frozen program",
          program: { id: envelope.program.id, version: envelope.program.version },
        },
        422,
      );
    }
    const manifest = JSON.parse(programRow.manifestJson) as AimarkBenchmarkProgramV1;

    // 4. The claimed class must exist in the program.
    if (!manifest.classes.some((cls) => cls.id === envelope.class)) {
      return c.json(
        {
          error: "unknown class for program",
          class: envelope.class,
          known_classes: manifest.classes.map((cls) => cls.id),
        },
        422,
      );
    }

    // 5. HMAC verification (mismatch flags, never rejects).
    let status: "accepted" | "flagged" = "accepted";
    let flagReason: string | null = null;
    if (!(await verifyBenchHmac(envelope))) {
      status = "flagged";
      flagReason = "hmac_mismatch";
    }

    // 6. Payload-hash dedup (+ bench_id uniqueness).
    const payloadSha = await benchPayloadHash(envelope);
    const duplicates = await db
      .select({ benchId: benchmarks.benchId })
      .from(benchmarks)
      .where(eq(benchmarks.payloadSha256, payloadSha))
      .limit(1);
    if (duplicates[0]) {
      return c.json({ error: "duplicate submission", bench_id: duplicates[0].benchId }, 409);
    }
    const sameId = await db
      .select({ benchId: benchmarks.benchId })
      .from(benchmarks)
      .where(eq(benchmarks.benchId, envelope.bench_id))
      .limit(1);
    if (sameId[0]) {
      return c.json({ error: "bench_id already exists", bench_id: envelope.bench_id }, 409);
    }

    // 7. Cell verification. Every referenced run must (a) exist, (b) carry
    // this bench_id in its envelope, (c) match the program cell's suite, and
    // (d) match the cell's role. Structural mismatches are 422s — the
    // envelope describes a program that wasn't run. Quality problems
    // (flagged cells, failed validity checks) flag instead: the data is
    // real, it just doesn't belong on the default board.
    const programCells = cellsForClass(manifest, envelope.class);
    const cellErrors: Record<string, unknown>[] = [];

    const runIds = envelope.cells.map((cell) => cell.run_id);
    const runRows = await db.select().from(runs).where(inArray(runs.runId, runIds));
    const runById = new Map(runRows.map((r) => [r.runId, r]));
    const missing = runIds.filter((id) => !runById.has(id));
    if (missing.length > 0) {
      return c.json({ error: "referenced cell runs not found", missing_run_ids: missing }, 422);
    }

    for (const cell of envelope.cells) {
      const programCell = programCells.get(cell.cell_id);
      if (!programCell) {
        cellErrors.push({
          cell_id: cell.cell_id,
          problem: "cell_id not in program for this class",
        });
        continue;
      }
      const run = runById.get(cell.run_id)!;
      const runEnvelope = parseJsonRecord(run.envelopeJson) as Partial<AimarkRunV1> | null;
      if (runEnvelope?.bench_id !== envelope.bench_id) {
        cellErrors.push({
          cell_id: cell.cell_id,
          run_id: cell.run_id,
          problem: "run bench_id does not match this benchmark",
        });
        continue;
      }
      if (run.suiteId !== programCell.suite || run.suiteVersion !== programCell.suite_version) {
        cellErrors.push({
          cell_id: cell.cell_id,
          run_id: cell.run_id,
          problem: "run suite does not match the program cell",
          expected: { suite: programCell.suite, version: programCell.suite_version },
          actual: { suite: run.suiteId, version: run.suiteVersion },
        });
        continue;
      }
      if (cell.role !== programCell.role) {
        cellErrors.push({
          cell_id: cell.cell_id,
          run_id: cell.run_id,
          problem: "cell role does not match the program cell",
          expected: programCell.role,
          actual: cell.role,
        });
      }
    }
    if (cellErrors.length > 0) {
      return c.json({ error: "cell verification failed", cells: cellErrors }, 422);
    }

    // 7b. Flag-level cell checks: any flagged cell run taints the benchmark;
    // validity cells (graded quality) must clear their accuracy floor —
    // quality is a class constant, so a miss means broken/cheated, not slow.
    for (const cell of envelope.cells) {
      const run = runById.get(cell.run_id)!;
      if (run.status !== "accepted" && status === "accepted") {
        status = "flagged";
        flagReason = "cell_flagged";
      }
      const programCell = programCells.get(cell.cell_id)!;
      if (programCell.role === "validity" && programCell.validity_min_accuracy !== undefined) {
        const runEnvelope = parseJsonRecord(run.envelopeJson) as Partial<AimarkRunV1> | null;
        const accuracy = runEnvelope?.metrics?.["quality_accuracy"];
        if (
          (typeof accuracy !== "number" || accuracy < programCell.validity_min_accuracy) &&
          status === "accepted"
        ) {
          status = "flagged";
          flagReason = "validity_failed";
        }
      }
    }

    // 8. Server recompute (client provisional_scores are ignored). The
    // System Score composite is the weighted geomean of the score-role
    // cells' SERVER-stored composite scores, weights from the program
    // manifest. Performance/consistency sub-scores aggregate the same way
    // over the cells that produced them.
    const scoreCells = envelope.cells.filter(
      (cell) => programCells.get(cell.cell_id)!.role === "score",
    );
    const cellScoreRows =
      scoreCells.length > 0
        ? await db
            .select({ runId: scores.runId, name: scores.name, value: scores.value })
            .from(scores)
            .where(
              inArray(
                scores.runId,
                scoreCells.map((cell) => cell.run_id),
              ),
            )
        : [];
    const cellScores = new Map<string, Record<string, number>>();
    for (const row of cellScoreRows) {
      const entry = cellScores.get(row.runId) ?? {};
      entry[row.name] = row.value;
      cellScores.set(row.runId, entry);
    }
    const pick = (name: string) =>
      scoreCells.flatMap((cell) => {
        const value = cellScores.get(cell.run_id)?.[name];
        if (value === undefined) return [];
        const weight = programCells.get(cell.cell_id)!.weight ?? 1;
        return [{ value, weight }];
      });
    const scoreMap: Record<string, number> = {};
    for (const name of SYSTEM_SUB_SCORES) {
      const value = weightedGeomean(pick(name));
      if (value !== null) scoreMap[name] = round2(value);
    }
    const composite = weightedGeomean(pick("composite"));
    if (composite !== null) scoreMap["composite"] = round2(composite);
    if (composite === null && status === "accepted") {
      status = "flagged";
      flagReason = "no_scorable_cells";
    }

    // 9. Persist hardware profile (content-addressed), benchmark, scores.
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

    await db.insert(benchmarks).values({
      benchId: envelope.bench_id,
      programId: envelope.program.id,
      programVersion: envelope.program.version,
      class: envelope.class,
      source: envelope.source,
      status,
      flagReason,
      payloadSha256: payloadSha,
      createdAt: envelope.created_at,
      submittedAt: now().toISOString(),
      ipHash,
      claimTokenHash,
      hidden: 0,
      hardwareProfileId,
      envelopeJson: JSON.stringify(envelope),
    });
    for (const [name, value] of Object.entries(scoreMap)) {
      await db.insert(benchmarkScores).values({ benchId: envelope.bench_id, name, value });
    }

    return c.json(
      {
        bench_id: envelope.bench_id,
        status,
        flag_reason: flagReason,
        class: envelope.class,
        scores: scoreMap,
        claim_token: claimToken,
        public_url: `${deps.baseUrl}/bench/?id=${envelope.bench_id}`,
      },
      201,
    );
  });

  // ------------------------------------------------------- GET /benchmarks/:id
  // Public detail — never exposes ip_hash, claim hash, or integrity.
  app.get("/v1/benchmarks/:id", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const id = c.req.param("id");
    const rows = await db.select().from(benchmarks).where(eq(benchmarks.benchId, id)).limit(1);
    const bench = rows[0];
    if (!bench) return c.json({ error: "benchmark not found" }, 404);

    const envelope = parseJsonRecord(bench.envelopeJson) as Partial<AimarkBenchmarkV1> | null;
    const scoreRows = await fetchBenchScores(db, [id]);
    const scoreMap: Record<string, number> = {};
    for (const row of scoreRows) scoreMap[row.name] = row.value;
    const { composite, ...subScores } = scoreMap;

    // Per-cell drill-down: each cell links to its run with that run's
    // server-stored composite alongside.
    const cellList = envelope?.cells ?? [];
    const runIds = cellList.map((cell) => cell.run_id);
    const cellRuns =
      runIds.length > 0 ? await db.select().from(runs).where(inArray(runs.runId, runIds)) : [];
    const runById = new Map(cellRuns.map((r) => [r.runId, r]));
    const compositeRows =
      runIds.length > 0
        ? await db
            .select({ runId: scores.runId, value: scores.value })
            .from(scores)
            .where(and(inArray(scores.runId, runIds), eq(scores.name, "composite")))
        : [];
    const compositeByRun = new Map(compositeRows.map((r) => [r.runId, r.value]));

    return c.json({
      bench_id: bench.benchId,
      program: { id: bench.programId, version: bench.programVersion },
      class: bench.class,
      classification: envelope?.classification ?? null,
      source: bench.source,
      status: bench.status,
      flag_reason: bench.flagReason,
      hidden: bench.hidden === 1,
      created_at: bench.createdAt,
      submitted_at: bench.submittedAt,
      cli: envelope?.cli ?? null,
      hardware_profile_id: bench.hardwareProfileId,
      hardware_profile: envelope?.environment?.hardware_profile ?? null,
      scores: subScores,
      composite: composite ?? null,
      cells: cellList.map((cell) => {
        const run = runById.get(cell.run_id);
        return {
          cell_id: cell.cell_id,
          run_id: cell.run_id,
          role: cell.role,
          suite: run ? { id: run.suiteId, version: run.suiteVersion } : null,
          model: run?.model ?? null,
          status: run?.status ?? null,
          composite: compositeByRun.get(cell.run_id) ?? null,
        };
      }),
    });
  });

  // ----------------------------------------------------- PATCH /benchmarks/:id
  // Anonymous claim flow, same as runs: delete (benchmark row + its scores —
  // cell runs stay, they have their own claim tokens), hide, show.
  app.patch("/v1/benchmarks/:id", async (c) => {
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
    const rows = await db.select().from(benchmarks).where(eq(benchmarks.benchId, id)).limit(1);
    const bench = rows[0];
    if (!bench) return c.json({ error: "benchmark not found" }, 404);
    if ((await sha256Hex(claimToken)) !== bench.claimTokenHash) {
      return c.json({ error: "invalid claim token" }, 403);
    }
    if (action === "delete") {
      await db.delete(benchmarkScores).where(eq(benchmarkScores.benchId, id));
      await db.delete(benchmarks).where(eq(benchmarks.benchId, id));
    } else {
      await db
        .update(benchmarks)
        .set({ hidden: action === "hide" ? 1 : 0 })
        .where(eq(benchmarks.benchId, id));
    }
    return c.json({ ok: true, bench_id: id, action });
  });

  // ------------------------------------------------ GET /leaderboard/systems
  // THE headline board: whole systems ranked by System Score within one
  // capability class. Scores are comparable only within (program, version,
  // class) — classes run different test models, like 3DMark presets.
  app.get("/v1/leaderboard/systems", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const q = c.req.query();
    const cls = q["class"];
    if (!cls) return c.json({ error: "class query parameter is required" }, 400);

    const conds = [eq(benchmarks.class, cls), eq(benchmarks.hidden, 0)];
    if (q["program"]) conds.push(eq(benchmarks.programId, q["program"]));
    if (q["version"] !== undefined) conds.push(eq(benchmarks.programVersion, Number(q["version"])));
    // Default board: user-sourced, accepted benchmarks only.
    if (q["include_ci"] !== "true") conds.push(eq(benchmarks.source, "user"));
    if (q["include_flagged"] !== "true") conds.push(eq(benchmarks.status, "accepted"));

    const limit = Math.min(Math.max(Number(q["limit"] ?? 50) || 50, 1), 200);
    const offset = Math.max(Number(q["offset"] ?? 0) || 0, 0);

    const rows = await db
      .select({ bench: benchmarks, composite: benchmarkScores.value })
      .from(benchmarks)
      .innerJoin(
        benchmarkScores,
        and(eq(benchmarkScores.benchId, benchmarks.benchId), eq(benchmarkScores.name, "composite")),
      )
      .where(and(...conds))
      .orderBy(desc(benchmarkScores.value))
      .limit(limit)
      .offset(offset);

    const scoreRows = await fetchBenchScores(
      db,
      rows.map((r) => r.bench.benchId),
    );
    const byBench = new Map<string, Record<string, number>>();
    for (const s of scoreRows) {
      const entry = byBench.get(s.benchId) ?? {};
      if (s.name !== "composite") entry[s.name] = s.value;
      byBench.set(s.benchId, entry);
    }

    return c.json({
      program: q["program"] ?? null,
      version: q["version"] !== undefined ? Number(q["version"]) : null,
      class: cls,
      rows: rows.map(({ bench, composite }) => ({
        bench_id: bench.benchId,
        class: bench.class,
        hardware: hardwareSummary(
          parseJsonRecord(bench.envelopeJson) as Partial<AimarkBenchmarkV1> | null,
        ),
        hardware_profile_id: bench.hardwareProfileId,
        scores: byBench.get(bench.benchId) ?? {},
        composite,
        source: bench.source,
        status: bench.status,
        created_at: bench.createdAt,
      })),
    });
  });

  // ---------------------------------------------------------- GET /model-fit
  // Buying-guidance matrix: over ALL accepted user runs (zero-choice cells
  // AND advanced-mode runs), group by (model, quantization, hardware cohort)
  // and report median decode tok/s + TTFT with a plain-language usability
  // badge. Cohort = first GPU name when present, else CPU model.
  app.get("/v1/model-fit", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const q = c.req.query();
    const minN = Math.max(Number(q["min_n"] ?? 3) || 3, 1);
    const modelFilter = (q["model"] ?? "").trim().toLowerCase();
    const cohortFilter = (q["cohort"] ?? "").trim().toLowerCase();

    const rows = await db
      .select()
      .from(runs)
      .where(and(eq(runs.status, "accepted"), eq(runs.hidden, 0), eq(runs.source, "user")));

    interface Group {
      model: string;
      quantization: string | null;
      cohort: string;
      cohortKind: "gpu" | "cpu";
      decode: number[];
      ttft: number[];
      n: number;
    }
    const groups = new Map<string, Group>();
    for (const run of rows) {
      const envelope = parseJsonRecord(run.envelopeJson) as Partial<AimarkRunV1> | null;
      const hw = envelope?.environment?.hardware_profile;
      const gpuName = hw?.gpus?.[0]?.name;
      const cohort = gpuName ?? hw?.cpu_model;
      if (!cohort) continue; // no hardware identity (hosted runs) — not fit data
      const cohortKind: "gpu" | "cpu" = gpuName ? "gpu" : "cpu";
      const key = `${run.model} ${run.quantization ?? ""} ${cohort}`;
      const entry = groups.get(key) ?? {
        model: run.model,
        quantization: run.quantization,
        cohort,
        cohortKind,
        decode: [],
        ttft: [],
        n: 0,
      };
      entry.n += 1;
      const decode = envelope?.metrics?.["decode_tps_mean"];
      if (typeof decode === "number" && decode > 0) entry.decode.push(decode);
      const ttft = envelope?.metrics?.["ttft_ms_p50"];
      if (typeof ttft === "number" && ttft > 0) entry.ttft.push(ttft);
      groups.set(key, entry);
    }

    const out = Array.from(groups.values())
      .filter((g) => g.n >= minN)
      .filter((g) => !modelFilter || g.model.toLowerCase().includes(modelFilter))
      .filter((g) => !cohortFilter || g.cohort.toLowerCase().includes(cohortFilter))
      .flatMap((g) => {
        const decodeMedian = median(g.decode);
        if (decodeMedian === null) return []; // tok/s is the point of this matrix
        return [
          {
            model: g.model,
            quantization: g.quantization,
            cohort: g.cohort,
            cohort_kind: g.cohortKind,
            n: g.n,
            decode_tps_median: round2(decodeMedian),
            ttft_ms_p50_median: median(g.ttft) !== null ? round2(median(g.ttft)!) : null,
            usability: usabilityBadge(decodeMedian),
          },
        ];
      })
      .sort((a, b) => b.decode_tps_median - a.decode_tps_median);

    return c.json({
      min_n: minN,
      note:
        "Observational community data: medians of accepted user-submitted runs grouped by " +
        "(model, quantization, hardware cohort). Not a controlled experiment.",
      rows: out,
    });
  });

  // ----------------------------------------------------- GET /monitor/series
  // Cloud-API monitoring reference series. Monitor runs are ordinary run.v1
  // submissions whose params contain {"monitor": true} — submitted on a
  // schedule by the central GitHub Actions cron (which runs in CI, so they
  // carry source=ci) or by users probing with their own keys. UNLIKE every
  // leaderboard endpoint, ci-sourced data is INCLUDED here by design:
  // monitoring is the one place where CI-origin probes are the point, not
  // noise — the probes measure the provider, not the machine they run on.
  app.get("/v1/monitor/series", async (c) => {
    const db = requireDb();
    if (!db) return c.json(dbUnavailable, 503);
    const q = c.req.query();
    const days = Math.min(Math.max(Number(q["days"] ?? 30) || 30, 1), 365);
    const providerFilter = (q["provider"] ?? "").trim().toLowerCase();
    const modelFilter = (q["model"] ?? "").trim().toLowerCase();
    const cutoff = new Date(now().getTime() - days * 24 * 60 * 60 * 1000)
      .toISOString()
      .slice(0, 10);

    const rows = await db
      .select()
      .from(runs)
      .where(and(eq(runs.status, "accepted"), eq(runs.hidden, 0), gte(runs.createdAt, cutoff)));

    interface Bucket {
      n: number;
      decode: number[];
      ttft: number[];
    }
    interface Series {
      provider: string;
      model: string;
      buckets: Map<string, Bucket>;
    }
    const series = new Map<string, Series>();
    for (const run of rows) {
      const params = parseJsonRecord(run.paramsJson);
      if (params?.["monitor"] !== true) continue;
      const provider = run.provider ?? run.runtime ?? "unknown";
      if (providerFilter && provider.toLowerCase() !== providerFilter) continue;
      if (modelFilter && !run.model.toLowerCase().includes(modelFilter)) continue;
      const day = run.createdAt.slice(0, 10);
      const key = `${provider} ${run.model}`;
      const entry = series.get(key) ?? { provider, model: run.model, buckets: new Map() };
      const bucket = entry.buckets.get(day) ?? { n: 0, decode: [], ttft: [] };
      bucket.n += 1;
      const envelope = parseJsonRecord(run.envelopeJson) as Partial<AimarkRunV1> | null;
      const decode = envelope?.metrics?.["decode_tps_mean"];
      if (typeof decode === "number" && decode > 0) bucket.decode.push(decode);
      const ttft = envelope?.metrics?.["ttft_ms_p50"];
      if (typeof ttft === "number" && ttft > 0) bucket.ttft.push(ttft);
      entry.buckets.set(day, bucket);
      series.set(key, entry);
    }

    return c.json({
      days,
      note:
        "Monitoring reference series — how hosted APIs perform over time, for context next to " +
        "local scores. Includes ci-sourced probes from the scheduled monitor cron by design; " +
        "these probes measure the provider, not the CI machine.",
      series: Array.from(series.values())
        .map((s) => ({
          provider: s.provider,
          model: s.model,
          points: Array.from(s.buckets.entries())
            .map(([date, bucket]) => ({
              date,
              n: bucket.n,
              decode_tps_median:
                median(bucket.decode) !== null ? round2(median(bucket.decode)!) : null,
              ttft_ms_p50_median:
                median(bucket.ttft) !== null ? round2(median(bucket.ttft)!) : null,
            }))
            .sort((a, b) => a.date.localeCompare(b.date)),
        }))
        .sort((a, b) =>
          a.provider === b.provider
            ? a.model.localeCompare(b.model)
            : a.provider.localeCompare(b.provider),
        ),
    });
  });
}
