import { describe, expect, test } from "bun:test";
import {
  createTestApp,
  makeValidBenchmark,
  randomUlid,
  submitBenchmark,
  submitBenchmarkOk,
  submitOk,
  TEST_BASE_URL,
  type BenchmarkSubmitResponse,
} from "./helpers";

/** Weighted geomean matching the server recompute (weights from TEST_PROGRAM). */
function geomean(values: { value: number; weight: number }[]): number {
  const total = values.reduce((sum, v) => sum + v.weight, 0);
  return Math.exp(values.reduce((sum, v) => sum + (v.weight / total) * Math.log(v.value), 0));
}

describe("POST /v1/benchmarks", () => {
  test("happy path: signed cell runs + benchmark -> 201 accepted, recomputed composite", async () => {
    const app = await createTestApp();
    const { bench, cellRuns } = await submitBenchmarkOk(app);
    expect(bench.status).toBe("accepted");
    expect(bench.flag_reason).toBeNull();
    expect(bench.class).toBe("compact");
    expect(bench.claim_token).toMatch(/^[0-9a-f]{64}$/);
    expect(bench.public_url).toBe(`${TEST_BASE_URL}/bench/?id=${bench.bench_id}`);

    // Composite = weighted geomean of the SCORE cells' stored composites.
    // Compact has one score cell (sprint-class, weight 3) — composite must
    // equal that cell run's composite; the validity cell must NOT count.
    const sprintComposite = cellRuns[0]!.scores["composite"]!;
    expect(bench.scores["composite"]).toBeCloseTo(sprintComposite, 1);
    expect(bench.scores["performance"]).toBeCloseTo(cellRuns[0]!.scores["performance"]!, 1);
  });

  test("multi-cell recompute: weighted geomean across score cells", async () => {
    const app = await createTestApp();
    const benchId = randomUlid();
    // mainstream class: sprint-class (weight 3) + sprint-anchor (weight 1).
    const classRun = await submitOk(app, (env) => {
      env.bench_id = benchId;
    });
    const anchorRun = await submitOk(app, (env) => {
      env.bench_id = benchId;
      env.metrics = { ...env.metrics, decode_tps_mean: 250 }; // different speed
    });
    const gauntletRun = await submitOk(app, (env) => {
      env.bench_id = benchId;
      env.suite = { id: "gauntlet", version: 1, protocol_hash: "0".repeat(64) };
      env.metrics = { quality_accuracy: 0.9, decode_tps_mean: 80, latency_ms_p95: 1200 };
    });
    const bench = await makeValidBenchmark({
      benchId,
      class: "mainstream",
      cells: [
        { cell_id: "sprint-class", run_id: classRun.run_id, role: "score" },
        { cell_id: "sprint-anchor", run_id: anchorRun.run_id, role: "score" },
        { cell_id: "gauntlet-validity", run_id: gauntletRun.run_id, role: "validity" },
      ],
    });
    const res = await submitBenchmark(app, bench);
    expect(res.status).toBe(201);
    const body = (await res.json()) as BenchmarkSubmitResponse;
    expect(body.status).toBe("accepted");
    const expected = geomean([
      { value: classRun.scores["composite"]!, weight: 3 },
      { value: anchorRun.scores["composite"]!, weight: 1 },
    ]);
    expect(body.scores["composite"]).toBeCloseTo(expected, 1);
  });

  test("unknown program -> 422", async () => {
    const app = await createTestApp();
    const { cellRuns, bench } = await submitBenchmarkOk(app);
    void cellRuns;
    const envelope = await makeValidBenchmark({
      cells: [{ cell_id: "sprint-class", run_id: bench.bench_id, role: "score" }],
      mutate: (env) => {
        env.program = { id: "no-such-program", version: 9 };
      },
    });
    const res = await submitBenchmark(app, envelope);
    expect(res.status).toBe(422);
    expect(((await res.json()) as { error: string }).error).toContain("program");
  });

  test("unknown class -> 422 with known classes", async () => {
    const app = await createTestApp();
    const benchId = randomUlid();
    const run = await submitOk(app, (env) => {
      env.bench_id = benchId;
    });
    const envelope = await makeValidBenchmark({
      benchId,
      class: "hyperscale",
      cells: [{ cell_id: "sprint-class", run_id: run.run_id, role: "score" }],
    });
    const res = await submitBenchmark(app, envelope);
    expect(res.status).toBe(422);
    const body = (await res.json()) as { error: string; known_classes: string[] };
    expect(body.error).toContain("class");
    expect(body.known_classes).toEqual(["compact", "mainstream"]);
  });

  test("missing referenced cell runs -> 422 naming them", async () => {
    const app = await createTestApp();
    const ghost = randomUlid();
    const envelope = await makeValidBenchmark({
      cells: [{ cell_id: "sprint-class", run_id: ghost, role: "score" }],
    });
    const res = await submitBenchmark(app, envelope);
    expect(res.status).toBe(422);
    const body = (await res.json()) as { error: string; missing_run_ids: string[] };
    expect(body.missing_run_ids).toEqual([ghost]);
  });

  test("cell mismatch (wrong suite for the program cell) -> 422", async () => {
    const app = await createTestApp();
    const benchId = randomUlid();
    // gauntlet run referenced from the SPRINT cell — suite mismatch.
    const gauntletRun = await submitOk(app, (env) => {
      env.bench_id = benchId;
      env.suite = { id: "gauntlet", version: 1, protocol_hash: "0".repeat(64) };
      env.metrics = { quality_accuracy: 0.9, decode_tps_mean: 80, latency_ms_p95: 1200 };
    });
    const envelope = await makeValidBenchmark({
      benchId,
      cells: [{ cell_id: "sprint-class", run_id: gauntletRun.run_id, role: "score" }],
    });
    const res = await submitBenchmark(app, envelope);
    expect(res.status).toBe(422);
    const body = (await res.json()) as { error: string; cells: { problem: string }[] };
    expect(body.error).toBe("cell verification failed");
    expect(body.cells[0]!.problem).toContain("suite");
  });

  test("cell run with a different bench_id -> 422", async () => {
    const app = await createTestApp();
    const run = await submitOk(app, (env) => {
      env.bench_id = randomUlid(); // belongs to a DIFFERENT benchmark
    });
    const envelope = await makeValidBenchmark({
      cells: [{ cell_id: "sprint-class", run_id: run.run_id, role: "score" }],
    });
    const res = await submitBenchmark(app, envelope);
    expect(res.status).toBe(422);
    const body = (await res.json()) as { cells: { problem: string }[] };
    expect(body.cells[0]!.problem).toContain("bench_id");
  });

  test("unknown cell_id for the class -> 422", async () => {
    const app = await createTestApp();
    const benchId = randomUlid();
    const run = await submitOk(app, (env) => {
      env.bench_id = benchId;
    });
    // sprint-anchor exists only for class mainstream, not compact.
    const envelope = await makeValidBenchmark({
      benchId,
      class: "compact",
      cells: [{ cell_id: "sprint-anchor", run_id: run.run_id, role: "score" }],
    });
    const res = await submitBenchmark(app, envelope);
    expect(res.status).toBe(422);
    const body = (await res.json()) as { cells: { problem: string }[] };
    expect(body.cells[0]!.problem).toContain("cell_id");
  });

  test("flagged cell run -> benchmark flagged cell_flagged (not rejected)", async () => {
    const app = await createTestApp();
    const { bench } = await submitBenchmarkOk(app, {
      // Implausible decode speed flags the sprint cell run itself.
      mutateCellRun: (env) => {
        if (env.suite.id === "sprint") {
          env.metrics = { ...env.metrics, decode_tps_mean: 999999 };
        }
      },
    });
    expect(bench.status).toBe("flagged");
    expect(bench.flag_reason).toBe("cell_flagged");
  });

  test("validity cell below validity_min_accuracy -> flagged validity_failed", async () => {
    const app = await createTestApp();
    const { bench } = await submitBenchmarkOk(app, {
      mutateCellRun: (env) => {
        if (env.suite.id === "gauntlet") {
          env.metrics = { ...env.metrics, quality_accuracy: 0.2 }; // below 0.5 floor
        }
      },
    });
    expect(bench.status).toBe("flagged");
    expect(bench.flag_reason).toBe("validity_failed");
  });

  test("hmac mismatch -> flagged hmac_mismatch, never rejected", async () => {
    const app = await createTestApp();
    const benchId = randomUlid();
    const run = await submitOk(app, (env) => {
      env.bench_id = benchId;
    });
    const gauntletRun = await submitOk(app, (env) => {
      env.bench_id = benchId;
      env.suite = { id: "gauntlet", version: 1, protocol_hash: "0".repeat(64) };
      env.metrics = { quality_accuracy: 0.9, decode_tps_mean: 80, latency_ms_p95: 1200 };
    });
    const envelope = await makeValidBenchmark({
      benchId,
      cells: [
        { cell_id: "sprint-class", run_id: run.run_id, role: "score" },
        { cell_id: "gauntlet-validity", run_id: gauntletRun.run_id, role: "validity" },
      ],
    });
    envelope.integrity!.hmac = "0".repeat(64); // tamper after signing
    const res = await submitBenchmark(app, envelope);
    expect(res.status).toBe(201);
    const body = (await res.json()) as BenchmarkSubmitResponse;
    expect(body.status).toBe("flagged");
    expect(body.flag_reason).toBe("hmac_mismatch");
  });

  test("duplicate payload -> 409", async () => {
    const app = await createTestApp();
    const benchId = randomUlid();
    const run = await submitOk(app, (env) => {
      env.bench_id = benchId;
    });
    const gauntletRun = await submitOk(app, (env) => {
      env.bench_id = benchId;
      env.suite = { id: "gauntlet", version: 1, protocol_hash: "0".repeat(64) };
      env.metrics = { quality_accuracy: 0.9, decode_tps_mean: 80, latency_ms_p95: 1200 };
    });
    const envelope = await makeValidBenchmark({
      benchId,
      cells: [
        { cell_id: "sprint-class", run_id: run.run_id, role: "score" },
        { cell_id: "gauntlet-validity", run_id: gauntletRun.run_id, role: "validity" },
      ],
    });
    expect((await submitBenchmark(app, envelope)).status).toBe(201);
    const dup = await submitBenchmark(app, envelope);
    expect(dup.status).toBe(409);
    expect(((await dup.json()) as { bench_id: string }).bench_id).toBe(benchId);
  });

  test("schema-invalid envelope -> 422", async () => {
    const app = await createTestApp();
    const res = await submitBenchmark(app, { schema_version: "aimark.benchmark.v1" });
    expect(res.status).toBe(422);
  });
});

describe("GET /v1/benchmarks/:id", () => {
  test("public detail: class, scores, cells with per-cell composites", async () => {
    const app = await createTestApp();
    const { bench, cellRuns } = await submitBenchmarkOk(app);
    const res = await app.request(`/v1/benchmarks/${bench.bench_id}`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      bench_id: string;
      class: string;
      classification: { cpu_only: boolean } | null;
      hardware_profile: { cpu_model?: string } | null;
      composite: number;
      scores: Record<string, number>;
      cells: { cell_id: string; role: string; composite: number | null; run_id: string }[];
      status: string;
    };
    expect(body.bench_id).toBe(bench.bench_id);
    expect(body.class).toBe("compact");
    expect(body.classification?.cpu_only).toBe(true);
    expect(body.hardware_profile?.cpu_model).toBe("AMD Ryzen 9 7950X");
    expect(body.composite).toBeCloseTo(bench.scores["composite"]!, 1);
    expect(body.cells).toHaveLength(2);
    const scoreCell = body.cells.find((cell) => cell.role === "score")!;
    expect(scoreCell.run_id).toBe(cellRuns[0]!.run_id);
    expect(scoreCell.composite).toBeCloseTo(cellRuns[0]!.scores["composite"]!, 1);
    // No private fields leak.
    expect(JSON.stringify(body)).not.toContain("ip_hash");
    expect(JSON.stringify(body)).not.toContain("claim_token");
  });

  test("unknown id -> 404", async () => {
    const app = await createTestApp();
    const res = await app.request(`/v1/benchmarks/${randomUlid()}`);
    expect(res.status).toBe(404);
  });
});

describe("PATCH /v1/benchmarks/:id (claim)", () => {
  test("claim delete removes the benchmark (cell runs survive)", async () => {
    const app = await createTestApp();
    const { bench, cellRuns } = await submitBenchmarkOk(app);
    const res = await app.request(`/v1/benchmarks/${bench.bench_id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ claim_token: bench.claim_token, action: "delete" }),
    });
    expect(res.status).toBe(200);
    expect((await app.request(`/v1/benchmarks/${bench.bench_id}`)).status).toBe(404);
    // The cell runs are independently owned and stay.
    expect((await app.request(`/v1/runs/${cellRuns[0]!.run_id}`)).status).toBe(200);
  });

  test("wrong claim token -> 403; hide/show toggles board visibility", async () => {
    const app = await createTestApp();
    const { bench } = await submitBenchmarkOk(app);
    const bad = await app.request(`/v1/benchmarks/${bench.bench_id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ claim_token: "f".repeat(64), action: "delete" }),
    });
    expect(bad.status).toBe(403);
    const hide = await app.request(`/v1/benchmarks/${bench.bench_id}`, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ claim_token: bench.claim_token, action: "hide" }),
    });
    expect(hide.status).toBe(200);
    const board = (await (await app.request("/v1/leaderboard/systems?class=compact")).json()) as {
      rows: { bench_id: string }[];
    };
    expect(board.rows.some((r) => r.bench_id === bench.bench_id)).toBe(false);
  });
});

describe("GET /v1/leaderboard/systems", () => {
  test("requires class; ranks by composite desc with hardware summary", async () => {
    const app = await createTestApp();
    expect((await app.request("/v1/leaderboard/systems")).status).toBe(400);

    const slow = await submitBenchmarkOk(app, {
      mutateCellRun: (env) => {
        if (env.suite.id === "sprint") env.metrics = { ...env.metrics, decode_tps_mean: 40 };
      },
    });
    const fast = await submitBenchmarkOk(app, {
      mutateCellRun: (env) => {
        if (env.suite.id === "sprint") env.metrics = { ...env.metrics, decode_tps_mean: 400 };
      },
    });
    const res = await app.request(
      "/v1/leaderboard/systems?program=testbench&version=1&class=compact",
    );
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      class: string;
      rows: {
        bench_id: string;
        composite: number;
        scores: Record<string, number>;
        hardware: { cpu_model: string | null; gpu: string | null; ram_gb: number | null };
      }[];
    };
    expect(body.class).toBe("compact");
    expect(body.rows).toHaveLength(2);
    expect(body.rows[0]!.bench_id).toBe(fast.bench.bench_id);
    expect(body.rows[1]!.bench_id).toBe(slow.bench.bench_id);
    expect(body.rows[0]!.composite).toBeGreaterThan(body.rows[1]!.composite);
    expect(body.rows[0]!.hardware.gpu).toBe("NVIDIA GeForce RTX 4090");
    expect(body.rows[0]!.hardware.cpu_model).toBe("AMD Ryzen 9 7950X");
    expect(body.rows[0]!.hardware.ram_gb).toBe(64);
    expect(body.rows[0]!.scores["performance"]).toBeGreaterThan(0);
  });

  test("ci-source benchmarks hidden by default, visible with include_ci", async () => {
    const app = await createTestApp();
    const { bench } = await submitBenchmarkOk(app, {
      mutateBench: (env) => {
        env.source = "ci";
      },
      mutateCellRun: (env) => {
        env.source = "ci";
      },
    });
    const def = (await (await app.request("/v1/leaderboard/systems?class=compact")).json()) as {
      rows: { bench_id: string }[];
    };
    expect(def.rows.some((r) => r.bench_id === bench.bench_id)).toBe(false);
    const withCi = (await (
      await app.request("/v1/leaderboard/systems?class=compact&include_ci=true")
    ).json()) as { rows: { bench_id: string; source: string }[] };
    const row = withCi.rows.find((r) => r.bench_id === bench.bench_id);
    expect(row?.source).toBe("ci");
  });

  test("flagged benchmarks hidden by default, visible with include_flagged", async () => {
    const app = await createTestApp();
    const { bench } = await submitBenchmarkOk(app, {
      mutateCellRun: (env) => {
        if (env.suite.id === "gauntlet") env.metrics = { ...env.metrics, quality_accuracy: 0.1 };
      },
    });
    expect(bench.status).toBe("flagged");
    const def = (await (await app.request("/v1/leaderboard/systems?class=compact")).json()) as {
      rows: { bench_id: string }[];
    };
    expect(def.rows.some((r) => r.bench_id === bench.bench_id)).toBe(false);
    const withFlagged = (await (
      await app.request("/v1/leaderboard/systems?class=compact&include_flagged=true")
    ).json()) as { rows: { bench_id: string; status: string }[] };
    expect(withFlagged.rows.find((r) => r.bench_id === bench.bench_id)?.status).toBe("flagged");
  });
});

describe("GET /v1/model-fit", () => {
  test("groups by model/quant/cohort with medians and usability badges", async () => {
    const app = await createTestApp();
    // 3 fast runs on the fixture's GPU rig (cohort = RTX 4090).
    for (const tps of [100, 120, 140]) {
      await submitOk(app, (env) => {
        env.metrics = { ...env.metrics, decode_tps_mean: tps };
      });
    }
    // 3 painful CPU-only runs of another model.
    for (const tps of [4, 5, 6]) {
      await submitOk(app, (env) => {
        env.target = { ...env.target, model: "slowpoke-70b" };
        env.metrics = { ...env.metrics, decode_tps_mean: tps };
        env.environment = {
          hardware_profile: {
            cpu_model: "Intel Core i5-8250U",
            ram_gb: 16,
            os: "linux",
            arch: "amd64",
          },
        };
      });
    }
    // Below-min_n group: must not appear.
    await submitOk(app, (env) => {
      env.target = { ...env.target, model: "rare-model" };
    });

    const res = await app.request("/v1/model-fit?min_n=3");
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      rows: {
        model: string;
        cohort: string;
        cohort_kind: string;
        n: number;
        decode_tps_median: number;
        ttft_ms_p50_median: number | null;
        usability: string;
      }[];
    };
    expect(body.rows).toHaveLength(2);
    const gpuRow = body.rows.find((r) => r.cohort_kind === "gpu")!;
    expect(gpuRow.cohort).toBe("NVIDIA GeForce RTX 4090");
    expect(gpuRow.n).toBe(3);
    expect(gpuRow.decode_tps_median).toBe(120);
    expect(gpuRow.usability).toBe("instant");
    expect(gpuRow.ttft_ms_p50_median).toBeGreaterThan(0);
    const cpuRow = body.rows.find((r) => r.cohort_kind === "cpu")!;
    expect(cpuRow.model).toBe("slowpoke-70b");
    expect(cpuRow.cohort).toBe("Intel Core i5-8250U");
    expect(cpuRow.decode_tps_median).toBe(5);
    expect(cpuRow.usability).toBe("painful");

    // Substring filters.
    const filtered = (await (await app.request("/v1/model-fit?min_n=3&model=slowpoke")).json()) as {
      rows: { model: string }[];
    };
    expect(filtered.rows).toHaveLength(1);
    expect(filtered.rows[0]!.model).toBe("slowpoke-70b");
    const byCohort = (await (await app.request("/v1/model-fit?min_n=3&cohort=4090")).json()) as {
      rows: { cohort: string }[];
    };
    expect(byCohort.rows).toHaveLength(1);
  });

  test("usable badge between 10 and 30 tok/s", async () => {
    const app = await createTestApp();
    for (const tps of [12, 15, 18]) {
      await submitOk(app, (env) => {
        env.metrics = { ...env.metrics, decode_tps_mean: tps };
      });
    }
    const body = (await (await app.request("/v1/model-fit")).json()) as {
      rows: { usability: string }[];
    };
    expect(body.rows[0]!.usability).toBe("usable");
  });
});

describe("GET /v1/monitor/series", () => {
  test("daily buckets of median decode/ttft per provider+model, ci included", async () => {
    const now = () => new Date("2026-06-13T00:00:00Z");
    const app = await createTestApp({ now });
    // Monitor probes: hosted runs with params.monitor=true, ci-sourced (the
    // central cron runs in CI — that's the point here).
    const probe = (day: string, tps: number) => async () =>
      submitOk(app, (env) => {
        env.source = "ci";
        env.created_at = `${day}T06:00:00Z`;
        env.target = {
          kind: "hosted",
          provider: "anthropic",
          model: "claude-test-1",
          params: { monitor: true },
        };
        env.metrics = { ttft_ms_p50: 300, decode_tps_mean: tps, latency_ms_p95: 900 };
      });
    await probe("2026-06-10", 50)();
    await probe("2026-06-10", 70)();
    await probe("2026-06-11", 90)();
    // A normal (non-monitor) run must NOT appear in the series.
    await submitOk(app, (env) => {
      env.created_at = "2026-06-10T08:00:00Z";
    });

    const res = await app.request("/v1/monitor/series?days=30");
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      note: string;
      series: {
        provider: string;
        model: string;
        points: { date: string; n: number; decode_tps_median: number | null }[];
      }[];
    };
    expect(body.note).toContain("ci");
    expect(body.series).toHaveLength(1);
    const s = body.series[0]!;
    expect(s.provider).toBe("anthropic");
    expect(s.model).toBe("claude-test-1");
    expect(s.points).toEqual([
      { date: "2026-06-10", n: 2, decode_tps_median: 60, ttft_ms_p50_median: 300 },
      { date: "2026-06-11", n: 1, decode_tps_median: 90, ttft_ms_p50_median: 300 },
    ] as never);
  });

  test("days window excludes old probes; provider filter applies", async () => {
    const now = () => new Date("2026-06-13T00:00:00Z");
    const app = await createTestApp({ now });
    await submitOk(app, (env) => {
      env.source = "ci";
      env.created_at = "2026-01-01T06:00:00Z"; // far outside 30 days
      env.target = {
        kind: "hosted",
        provider: "anthropic",
        model: "claude-test-1",
        params: { monitor: true },
      };
      env.metrics = { ttft_ms_p50: 300, decode_tps_mean: 50, latency_ms_p95: 900 };
    });
    await submitOk(app, (env) => {
      env.source = "ci";
      env.created_at = "2026-06-12T06:00:00Z";
      env.target = {
        kind: "hosted",
        provider: "openai",
        model: "gpt-test",
        params: { monitor: true },
      };
      env.metrics = { ttft_ms_p50: 200, decode_tps_mean: 80, latency_ms_p95: 700 };
    });
    const body = (await (await app.request("/v1/monitor/series?days=30")).json()) as {
      series: { provider: string }[];
    };
    expect(body.series).toHaveLength(1);
    expect(body.series[0]!.provider).toBe("openai");
    const filtered = (await (
      await app.request("/v1/monitor/series?days=30&provider=anthropic")
    ).json()) as { series: unknown[] };
    expect(filtered.series).toHaveLength(0);
  });
});
