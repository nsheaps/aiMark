import { describe, expect, test } from "bun:test";
import { Glob } from "bun";
import { validateSuiteManifestV1 } from "@aimark/schema";

const manifests = [...new Glob("*/manifest.json").scanSync({ cwd: `${import.meta.dir}/..` })];

describe("suite manifests", () => {
  test("at least one suite exists", () => {
    expect(manifests.length).toBeGreaterThan(0);
  });

  for (const path of manifests) {
    test(`${path} validates against suite-manifest.v1`, async () => {
      const manifest = await Bun.file(`${import.meta.dir}/../${path}`).json();
      validateSuiteManifestV1(manifest);
      expect(validateSuiteManifestV1.errors ?? []).toEqual([]);
      expect(`${manifest.id}-${manifest.version}/manifest.json`).toBe(path);
    });
  }
});
