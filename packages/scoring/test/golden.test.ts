import { describe, expect, test } from "bun:test";
import { compositeScore, subScore, type MetricSpec } from "../src/index";
import goldenJson from "@aimark/schema/testdata/scoring-golden.json";

interface GoldenFile {
  tolerance: number;
  sub_score_cases: Array<{
    name: string;
    metrics: Record<string, number>;
    specs: Array<{ key: string; reference: number; weight: number; lower_is_better?: boolean }>;
    expected: number;
  }>;
  composite_cases: Array<{
    name: string;
    sub_scores: Record<string, number>;
    weights: Record<string, number>;
    expected: number;
  }>;
}

const golden = goldenJson as unknown as GoldenFile;

/**
 * Cross-language contract: the Go provisional scorer (apps/cli/internal/score)
 * runs these exact vectors. If a case fails here, fix the implementation —
 * never the golden file — unless the formula itself is being versioned.
 */
describe("golden score vectors", () => {
  for (const c of golden.sub_score_cases) {
    test(`subScore: ${c.name}`, () => {
      const specs: MetricSpec[] = c.specs.map((s) => ({
        key: s.key,
        reference: s.reference,
        weight: s.weight,
        lowerIsBetter: s.lower_is_better,
      }));
      expect(Math.abs(subScore(c.metrics, specs) - c.expected)).toBeLessThan(golden.tolerance);
    });
  }

  for (const c of golden.composite_cases) {
    test(`composite: ${c.name}`, () => {
      expect(Math.abs(compositeScore(c.sub_scores, c.weights) - c.expected)).toBeLessThan(
        golden.tolerance,
      );
    });
  }
});
