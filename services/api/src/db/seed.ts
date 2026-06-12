import sprint1Manifest from "@aimark/suites/sprint-1/manifest.json" with { type: "json" };
import { suites } from "./schema";
import type { Database } from "../deps";

/**
 * Known suite manifests, imported directly from packages/suites — bundlers
 * (bun, wrangler/esbuild) inline the JSON. Add new suite versions here.
 */
export const KNOWN_SUITE_MANIFESTS: Record<string, unknown>[] = [sprint1Manifest];

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
