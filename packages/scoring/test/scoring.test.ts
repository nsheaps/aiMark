import { describe, expect, test } from "bun:test";
import { compositeScore, subScore } from "../src/index";

describe("subScore", () => {
  test("reference setup scores exactly 1000", () => {
    const score = subScore({ decode_tps_mean: 100, ttft_ms_p50: 50 }, [
      { key: "decode_tps_mean", reference: 100, weight: 1 },
      { key: "ttft_ms_p50", reference: 50, weight: 1, lowerIsBetter: true },
    ]);
    expect(score).toBeCloseTo(1000, 6);
  });

  test("twice as fast on every metric scores 2000", () => {
    const score = subScore({ decode_tps_mean: 200, ttft_ms_p50: 25 }, [
      { key: "decode_tps_mean", reference: 100, weight: 1 },
      { key: "ttft_ms_p50", reference: 50, weight: 1, lowerIsBetter: true },
    ]);
    expect(score).toBeCloseTo(2000, 6);
  });

  test("throws on missing metric", () => {
    expect(() => subScore({}, [{ key: "x", reference: 1, weight: 1 }])).toThrow();
  });
});

describe("compositeScore", () => {
  test("equal sub-scores yield that score", () => {
    expect(
      compositeScore({ performance: 1500, quality: 1500 }, { performance: 2, quality: 1 }),
    ).toBeCloseTo(1500, 6);
  });

  test("ignores weights for absent sub-scores", () => {
    expect(
      compositeScore({ performance: 1200 }, { performance: 1, quality: 1, cost_efficiency: 1 }),
    ).toBeCloseTo(1200, 6);
  });
});
