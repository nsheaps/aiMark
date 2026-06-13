/** Small pure helpers shared by the run routes (app.ts) and the benchmark
 * routes (benchmarks.ts). */

export function median(values: number[]): number | null {
  if (values.length === 0) return null;
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  const lower = sorted[mid - 1];
  const upper = sorted[mid];
  if (upper === undefined) return null;
  return sorted.length % 2 === 0 && lower !== undefined ? (lower + upper) / 2 : upper;
}

export function parseJsonRecord(text: string | null): Record<string, unknown> | null {
  if (text === null) return null;
  try {
    return JSON.parse(text) as Record<string, unknown>;
  } catch {
    return null;
  }
}

export function clientIp(headers: Headers): string {
  const cf = headers.get("cf-connecting-ip");
  if (cf) return cf.trim();
  const xff = headers.get("x-forwarded-for");
  if (xff) {
    const first = xff.split(",")[0]?.trim();
    if (first) return first;
  }
  return "local";
}

/** Weighted geometric mean; returns null when no usable (positive) values. */
export function weightedGeomean(values: { value: number; weight: number }[]): number | null {
  const usable = values.filter((v) => v.value > 0 && v.weight > 0);
  if (usable.length === 0) return null;
  const totalWeight = usable.reduce((sum, v) => sum + v.weight, 0);
  let logSum = 0;
  for (const v of usable) logSum += (v.weight / totalWeight) * Math.log(v.value);
  return Math.exp(logSum);
}
