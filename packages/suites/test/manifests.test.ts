import { describe, expect, test } from "bun:test";
import { Glob } from "bun";

const manifests = [...new Glob("*/manifest.json").scanSync({ cwd: `${import.meta.dir}/..` })];

describe("suite manifests", () => {
  test("at least one suite exists", () => {
    expect(manifests.length).toBeGreaterThan(0);
  });

  for (const path of manifests) {
    test(`${path} has the required shape`, async () => {
      const manifest = await Bun.file(`${import.meta.dir}/../${path}`).json();
      expect(typeof manifest.id).toBe("string");
      expect(Number.isInteger(manifest.version)).toBe(true);
      expect(`${manifest.id}-${manifest.version}/manifest.json`).toBe(path);
      expect(Array.isArray(manifest.tracks)).toBe(true);
      expect(manifest.protocol).toBeDefined();
      expect(manifest.scoring).toBeDefined();
    });
  }
});
