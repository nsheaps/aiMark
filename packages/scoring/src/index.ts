/**
 * Canonical aiMark scoring.
 *
 * sub_score = K × weighted geometric mean of (metric / reference_metric),
 * with lower-is-better metrics inverted before normalization. Weights and
 * reference values come from the frozen suite manifest — they are data,
 * not code, so this module stays trivially simple and stable.
 */

export const SCORE_SCALE = 1000;

export interface MetricSpec {
  /** Metric key as it appears in run.metrics, e.g. "decode_tps_mean" */
  key: string;
  /** Frozen reference value from the suite manifest */
  reference: number;
  /** Relative weight (normalized internally) */
  weight: number;
  /** true when smaller raw values are better (latencies) */
  lowerIsBetter?: boolean;
}

export function subScore(metrics: Record<string, number>, specs: MetricSpec[]): number {
  if (specs.length === 0) {
    throw new Error("subScore requires at least one metric spec");
  }
  const totalWeight = specs.reduce((sum, s) => sum + s.weight, 0);
  let logSum = 0;
  for (const spec of specs) {
    const raw = metrics[spec.key];
    if (raw === undefined || raw <= 0) {
      throw new Error(`metric ${spec.key} missing or non-positive`);
    }
    const normalized = spec.lowerIsBetter ? spec.reference / raw : raw / spec.reference;
    logSum += (spec.weight / totalWeight) * Math.log(normalized);
  }
  return SCORE_SCALE * Math.exp(logSum);
}

export function compositeScore(
  subScores: Record<string, number>,
  weights: Record<string, number>,
): number {
  const entries = Object.entries(weights).filter(([key]) => subScores[key] !== undefined);
  if (entries.length === 0) {
    throw new Error("compositeScore requires at least one applicable sub-score");
  }
  const totalWeight = entries.reduce((sum, [, w]) => sum + w, 0);
  let logSum = 0;
  for (const [key, weight] of entries) {
    const value = subScores[key];
    if (value === undefined || value <= 0) {
      throw new Error(`sub-score ${key} missing or non-positive`);
    }
    logSum += (weight / totalWeight) * Math.log(value);
  }
  return Math.exp(logSum);
}
