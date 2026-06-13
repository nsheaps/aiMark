import deepdive1Manifest from "@aimark/suites/deepdive-1/manifest.json" with { type: "json" };
import forge1Manifest from "@aimark/suites/forge-1/manifest.json" with { type: "json" };
import gauntlet1Manifest from "@aimark/suites/gauntlet-1/manifest.json" with { type: "json" };
import marathon1Manifest from "@aimark/suites/marathon-1/manifest.json" with { type: "json" };
import sprint1Manifest from "@aimark/suites/sprint-1/manifest.json" with { type: "json" };
import { validateBenchmarkProgramV1 } from "@aimark/schema";
import { programs, suites } from "./schema";
import type { Database } from "../deps";

/**
 * Known suite manifests, imported directly from packages/suites — bundlers
 * (bun, wrangler/esbuild) inline the JSON. Add new suite versions here.
 */
export const KNOWN_SUITE_MANIFESTS: Record<string, unknown>[] = [
  deepdive1Manifest,
  forge1Manifest,
  gauntlet1Manifest,
  marathon1Manifest,
  sprint1Manifest,
];

/** Upserts the known suite manifests. Idempotent; runs at startup. */
export async function seedSuites(db: Database): Promise<void> {
  for (const manifest of KNOWN_SUITE_MANIFESTS) {
    await db
      .insert(suites)
      .values({
        id: String(manifest["id"]),
        version: Number(manifest["version"]),
        status: String(manifest["status"]),
        manifestJson: JSON.stringify(manifest),
      })
      .onConflictDoUpdate({
        target: [suites.id, suites.version],
        set: {
          status: String(manifest["status"]),
          manifestJson: JSON.stringify(manifest),
        },
      });
  }
}

/**
 * Known benchmark-program.v1 manifests. The bench-1 program file is authored
 * in packages/suites/programs/ and may land independently of this service, so
 * it is loaded lazily and defensively: a missing or schema-invalid file means
 * "no programs seeded" (POST /v1/benchmarks then 422s with "unknown program"),
 * never a startup crash or build break.
 */
export async function loadKnownPrograms(): Promise<Record<string, unknown>[]> {
  let manifest: unknown;
  try {
    const mod = (await import("@aimark/suites/programs/bench-1/program.json", {
      with: { type: "json" },
    })) as { default: unknown };
    manifest = mod.default;
  } catch {
    return []; // program file not present (yet)
  }
  if (!validateBenchmarkProgramV1(manifest)) {
    console.warn(
      "bench-1 program.json does not validate against benchmark-program.v1 — not seeding it",
      validateBenchmarkProgramV1.errors,
    );
    return [];
  }
  return [manifest as Record<string, unknown>];
}

/**
 * Upserts benchmark program manifests. Idempotent; runs at startup alongside
 * seedSuites. Tests pass their own minimal fixture instead of the real file.
 */
export async function seedPrograms(
  db: Database,
  programManifests?: Record<string, unknown>[],
): Promise<void> {
  const manifests = programManifests ?? (await loadKnownPrograms());
  for (const manifest of manifests) {
    await db
      .insert(programs)
      .values({
        id: String(manifest["id"]),
        version: Number(manifest["version"]),
        status: String(manifest["status"]),
        manifestJson: JSON.stringify(manifest),
      })
      .onConflictDoUpdate({
        target: [programs.id, programs.version],
        set: {
          status: String(manifest["status"]),
          manifestJson: JSON.stringify(manifest),
        },
      });
  }
}
