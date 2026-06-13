import { describe, expect, test } from "bun:test";
import type { AimarkRunV1 } from "@aimark/schema";
import { createTestApp, submitOk } from "./helpers";
import type { App } from "../src/app";

/**
 * Cross-cohort outlier flagging (the 4σ tier): once enough accepted user runs
 * exist on a (suite, version, track) board, a composite far outside the
 * cohort's distribution is flagged even though every metric passed the
 * manifest's plausibility bounds.
 */

/** Plausible spread: decode 100-136 tok/s at ~40ms TTFT — like real rigs. */
function plausibleMetrics(i: number): (env: AimarkRunV1) => void {
  return (env) => {
    env.metrics["decode_tps_mean"] = 100 + i * 4;
    env.metrics["ttft_ms_p50"] = 40;
  };
}

async function buildCohort(app: App, size: number): Promise<number[]> {
  const composites: number[] = [];
  for (let i = 0; i < size; i++) {
    const result = await submitOk(app, plausibleMetrics(i));
    expect(result.status).toBe("accepted");
    composites.push(result.scores["composite"] ?? 0);
  }
  return composites;
}

/** Absurd but inside plausibility bounds (decode max 50000, ttft min 0.05). */
const absurd = (env: AimarkRunV1) => {
  env.metrics["decode_tps_mean"] = 40000;
  env.metrics["ttft_ms_p50"] = 0.06;
};

describe("outlier flagging", () => {
  test("absurd-but-plausible run against a 10-run cohort -> flagged outlier_4sigma", async () => {
    const app = await createTestApp();
    await buildCohort(app, 10);

    const result = await submitOk(app, absurd);
    expect(result.status).toBe("flagged");
    expect(result.flag_reason).toBe("outlier_4sigma");
    // Still stored, with canonical scores.
    expect(result.scores["composite"]).toBeGreaterThan(0);
    const detail = (await (await app.request(`/v1/runs/${result.run_id}`)).json()) as {
      status: string;
      flag_reason: string;
      composite: number;
    };
    expect(detail.status).toBe("flagged");
    expect(detail.flag_reason).toBe("outlier_4sigma");
    expect(detail.composite).toBeGreaterThan(0);
  });

  test("normal run against the same cohort -> accepted", async () => {
    const app = await createTestApp();
    await buildCohort(app, 10);

    const result = await submitOk(app, (env) => {
      env.metrics["decode_tps_mean"] = 115;
      env.metrics["ttft_ms_p50"] = 41;
    });
    expect(result.status).toBe("accepted");
    expect(result.flag_reason).toBeNull();
  });

  test("cohort below the minimum size never flags", async () => {
    const app = await createTestApp();
    await buildCohort(app, 3); // default min cohort is 8

    const result = await submitOk(app, absurd);
    expect(result.status).toBe("accepted");
  });

  test("threshold and min cohort are deps-overridable", async () => {
    const app = await createTestApp({ outlierSigma: 2.0, outlierMinCohort: 3 });
    await buildCohort(app, 3);

    const result = await submitOk(app, absurd);
    expect(result.status).toBe("flagged");
    expect(result.flag_reason).toBe("outlier_4sigma");
  });

  test("flagged outliers don't poison the cohort: next normal run still accepted", async () => {
    const app = await createTestApp();
    await buildCohort(app, 10);
    const outlier = await submitOk(app, absurd);
    expect(outlier.status).toBe("flagged");

    // The flagged run is excluded from the cohort (status=accepted only), so
    // a normal follow-up isn't dragged toward the outlier's composite.
    const normal = await submitOk(app, (env) => {
      env.metrics["decode_tps_mean"] = 110;
      env.metrics["ttft_ms_p50"] = 39;
    });
    expect(normal.status).toBe("accepted");
  });
});
