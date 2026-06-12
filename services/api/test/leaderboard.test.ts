import { describe, expect, test } from "bun:test";
import { createTestApp, submitOk } from "./helpers";

interface BoardRow {
  run_id: string;
  model: string;
  source: string;
  status: string;
  composite: number;
}

async function board(app: Awaited<ReturnType<typeof createTestApp>>, query: string) {
  const res = await app.request(`/v1/leaderboard?${query}`);
  expect(res.status).toBe(200);
  return (await res.json()) as { rows: BoardRow[] };
}

describe("leaderboard", () => {
  test("requires suite", async () => {
    const app = await createTestApp();
    const res = await app.request("/v1/leaderboard");
    expect(res.status).toBe(400);
  });

  test("orders by composite desc and respects limit/offset", async () => {
    const app = await createTestApp();
    const slow = await submitOk(app, (env) => {
      env.metrics["decode_tps_mean"] = 50;
    });
    const fast = await submitOk(app, (env) => {
      env.metrics["decode_tps_mean"] = 400;
    });
    const mid = await submitOk(app, (env) => {
      env.metrics["decode_tps_mean"] = 150;
    });

    const { rows } = await board(app, "suite=sprint&version=1&track=local");
    expect(rows.map((r) => r.run_id)).toEqual([fast.run_id, mid.run_id, slow.run_id]);

    const page = await board(app, "suite=sprint&version=1&track=local&limit=1&offset=1");
    expect(page.rows.map((r) => r.run_id)).toEqual([mid.run_id]);
  });

  test("ci and dev runs are scored but excluded by default; include_ci=true includes them", async () => {
    const app = await createTestApp();
    const userRun = await submitOk(app);
    const ciRun = await submitOk(app, (env) => {
      env.source = "ci";
    });
    const devRun = await submitOk(app, (env) => {
      env.source = "dev";
    });
    // ci/dev runs are accepted and scored...
    expect(ciRun.status).toBe("accepted");
    expect(ciRun.scores["composite"]).toBeGreaterThan(0);
    expect(devRun.status).toBe("accepted");

    // ...but the default leaderboard only shows user runs.
    const defaults = await board(app, "suite=sprint&version=1&track=local");
    expect(defaults.rows.map((r) => r.run_id)).toEqual([userRun.run_id]);

    const withCi = await board(app, "suite=sprint&version=1&track=local&include_ci=true");
    const ids = withCi.rows.map((r) => r.run_id).sort();
    expect(ids).toEqual([userRun.run_id, ciRun.run_id, devRun.run_id].sort());
  });

  test("flagged runs excluded by default; include_flagged=true includes them", async () => {
    const app = await createTestApp();
    const good = await submitOk(app);
    const flagged = await submitOk(app, (env) => {
      env.metrics["decode_tps_mean"] = 999999; // implausible -> flagged
    });
    expect(flagged.status).toBe("flagged");

    const defaults = await board(app, "suite=sprint&version=1&track=local");
    expect(defaults.rows.map((r) => r.run_id)).toEqual([good.run_id]);

    const withFlagged = await board(
      app,
      "suite=sprint&version=1&track=local&include_flagged=true",
    );
    expect(withFlagged.rows.map((r) => r.run_id).sort()).toEqual(
      [good.run_id, flagged.run_id].sort(),
    );
    const flaggedRow = withFlagged.rows.find((r) => r.run_id === flagged.run_id);
    expect(flaggedRow?.status).toBe("flagged");
  });

  test("filters by model and other dimensions", async () => {
    const app = await createTestApp();
    const llama = await submitOk(app);
    const qwen = await submitOk(app, (env) => {
      env.target.model = "qwen2.5:7b";
    });

    const llamaBoard = await board(
      app,
      "suite=sprint&version=1&track=local&model=llama3.1:8b-instruct-q4_K_M",
    );
    expect(llamaBoard.rows.map((r) => r.run_id)).toEqual([llama.run_id]);

    const qwenBoard = await board(app, "suite=sprint&version=1&track=local&model=qwen2.5:7b");
    expect(qwenBoard.rows.map((r) => r.run_id)).toEqual([qwen.run_id]);

    const vllmBoard = await board(app, "suite=sprint&version=1&track=local&runtime=vllm");
    expect(vllmBoard.rows.length).toBe(0);
  });
});

describe("suites, compare, models, hardware, params", () => {
  test("GET /v1/suites lists seeded suites", async () => {
    const app = await createTestApp();
    const res = await app.request("/v1/suites");
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      suites: { id: string; version: number; status: string; title: string; task_count: number }[];
    };
    const sprint = body.suites.find((s) => s.id === "sprint" && s.version === 1);
    expect(sprint).toBeDefined();
    expect(sprint?.status).toBe("frozen");
    expect(sprint?.title).toBe("Sprint 1");
    expect(sprint?.task_count).toBe(10);
  });

  test("GET /v1/compare returns detail objects for up to 5 ids", async () => {
    const app = await createTestApp();
    const a = await submitOk(app);
    const b = await submitOk(app, (env) => {
      env.target.model = "qwen2.5:7b";
    });

    const res = await app.request(`/v1/compare?ids=${a.run_id},${b.run_id}`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { runs: { run_id: string; model: string }[] };
    expect(body.runs.length).toBe(2);
    expect(body.runs[0]?.run_id).toBe(a.run_id);
    expect(body.runs[1]?.model).toBe("qwen2.5:7b");

    const tooMany = await app.request(`/v1/compare?ids=a,b,c,d,e,f`);
    expect(tooMany.status).toBe(400);
    const empty = await app.request(`/v1/compare`);
    expect(empty.status).toBe(400);
  });

  test("GET /v1/models aggregates run counts + median composite per (suite,version,track)", async () => {
    const app = await createTestApp();
    await submitOk(app, (env) => {
      env.metrics["decode_tps_mean"] = 100;
    });
    await submitOk(app, (env) => {
      env.metrics["decode_tps_mean"] = 200;
    });
    await submitOk(app, (env) => {
      env.source = "ci"; // excluded from aggregates
    });

    const res = await app.request("/v1/models");
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      models: { model: string; suite: string; track: string; n: number; median_composite: number }[];
    };
    expect(body.models.length).toBe(1);
    const entry = body.models[0];
    expect(entry?.model).toBe("llama3.1:8b-instruct-q4_K_M");
    expect(entry?.suite).toBe("sprint");
    expect(entry?.track).toBe("local");
    expect(entry?.n).toBe(2);
    expect(entry?.median_composite).toBeGreaterThan(0);
  });

  test("GET /v1/hardware aggregates by hardware_profile_id", async () => {
    const app = await createTestApp();
    await submitOk(app);
    const res = await app.request("/v1/hardware");
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      hardware: { hardware_profile_id: string; n: number; median_composite: number }[];
    };
    expect(body.hardware.length).toBe(1);
    expect(body.hardware[0]?.hardware_profile_id).toMatch(/^[0-9a-f]{64}$/);
    expect(body.hardware[0]?.n).toBe(1);
  });

  test("GET /v1/params/impact groups accepted user runs by param value", async () => {
    const app = await createTestApp();
    for (const [numCtx, tps] of [
      [2048, 120],
      [2048, 140],
      [8192, 80],
    ] as const) {
      await submitOk(app, (env) => {
        env.target.params = { ...env.target.params, num_ctx: numCtx };
        env.metrics["decode_tps_mean"] = tps;
      });
    }

    const res = await app.request("/v1/params/impact?suite=sprint&version=1&track=local&param=num_ctx");
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      param: string;
      groups: { value: string; n: number; median_composite: number }[];
    };
    expect(body.param).toBe("num_ctx");
    expect(body.groups.length).toBe(2);
    const small = body.groups.find((g) => g.value === "2048");
    const large = body.groups.find((g) => g.value === "8192");
    expect(small?.n).toBe(2);
    expect(large?.n).toBe(1);
    expect(small!.median_composite).toBeGreaterThan(large!.median_composite);

    const missing = await app.request("/v1/params/impact?suite=sprint");
    expect(missing.status).toBe(400);
  });
});
