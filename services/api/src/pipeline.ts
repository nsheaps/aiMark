import { subScore, compositeScore, type MetricSpec } from "@aimark/scoring";
import type { AimarkRunV1, AimarkSuiteManifestV1 } from "@aimark/schema";
import { canonicalize, hmacSha256Hex, sha256Hex, DEV_HMAC_KEY, DEV_KEY_GEN } from "./canonical";

/**
 * Pure pieces of the submission pipeline: integrity verification,
 * plausibility bounds, and the canonical score recompute. The route handler
 * in app.ts sequences these and owns persistence.
 */

const SUB_SCORE_KEYS = ["performance", "quality", "cost_efficiency", "consistency"] as const;

export function round2(value: number): number {
  return Math.round(value * 100) / 100;
}

/** Canonical JSON of the envelope minus its integrity block — the HMAC message. */
export function integrityMessage(envelope: AimarkRunV1): string {
  const { integrity: _integrity, ...rest } = envelope;
  return canonicalize(rest);
}

/**
 * Honest-integrity model: an HMAC mismatch never rejects, it flags. Returns
 * true when the envelope carries a valid dev-key HMAC.
 */
export async function verifyHmac(envelope: AimarkRunV1): Promise<boolean> {
  const integrity = envelope.integrity;
  if (!integrity?.hmac || integrity.key_gen !== DEV_KEY_GEN) return false;
  const expected = await hmacSha256Hex(DEV_HMAC_KEY, integrityMessage(envelope));
  return integrity.hmac === expected;
}

/** payload hash used for dedup; falls back to hashing the canonical envelope. */
export async function payloadHash(envelope: AimarkRunV1): Promise<string> {
  return envelope.integrity?.payload_sha256 ?? (await sha256Hex(canonicalize(envelope)));
}

/** Returns the keys of metrics violating the manifest's plausibility bounds. */
export function implausibleMetrics(
  manifest: AimarkSuiteManifestV1,
  metrics: Record<string, number>,
): string[] {
  const bounds = manifest.plausibility ?? {};
  const violations: string[] = [];
  for (const [key, value] of Object.entries(metrics)) {
    const bound = bounds[key];
    if (!bound) continue;
    if ((bound.min !== undefined && value < bound.min) || (bound.max !== undefined && value > bound.max)) {
      violations.push(key);
    }
  }
  return violations;
}

/**
 * Canonical score recompute from the frozen manifest. Client
 * provisional_scores are ignored. Metric specs whose metric is absent (or
 * non-positive, which the geomean cannot take) are skipped; a sub-score with
 * no usable metrics is skipped entirely. Returns null when nothing is
 * scorable.
 */
export function computeScores(
  manifest: AimarkSuiteManifestV1,
  metrics: Record<string, number>,
): Record<string, number> | null {
  const subScores: Record<string, number> = {};
  for (const name of SUB_SCORE_KEYS) {
    const spec = manifest.scoring[name];
    if (!spec) continue;
    const usable: MetricSpec[] = spec.metrics
      .filter((m) => {
        const raw = metrics[m.key];
        return raw !== undefined && raw > 0;
      })
      .map((m) => ({
        key: m.key,
        reference: m.reference,
        weight: m.weight,
        lowerIsBetter: m.lower_is_better,
      }));
    if (usable.length === 0) continue;
    subScores[name] = round2(subScore(metrics, usable));
  }
  if (Object.keys(subScores).length === 0) return null;
  const composite = compositeScore(subScores, manifest.scoring.composite_weights);
  return { ...subScores, composite: round2(composite) };
}

/** sha256(ip + daily salt) — rotates daily so IPs are not linkable long-term. */
export async function hashIp(ip: string, now: Date): Promise<string> {
  const day = now.toISOString().slice(0, 10);
  return sha256Hex(`${ip}:${day}`);
}
