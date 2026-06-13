import { describe, expect, test } from "bun:test";
import { Glob } from "bun";
import { validateBenchmarkProgramV1 } from "@aimark/schema";

const programs = [
  ...new Glob("programs/*/program.json").scanSync({ cwd: `${import.meta.dir}/..` }),
];

describe("benchmark programs", () => {
  test("at least one program exists", () => {
    expect(programs.length).toBeGreaterThan(0);
  });

  for (const path of programs) {
    test(`${path} validates against benchmark-program.v1`, async () => {
      const program = await Bun.file(`${import.meta.dir}/../${path}`).json();
      validateBenchmarkProgramV1(program);
      expect(validateBenchmarkProgramV1.errors ?? []).toEqual([]);
      expect(`programs/${program.id}-${program.version}/program.json`).toBe(path);
    });

    test(`${path} is internally consistent`, async () => {
      const program = await Bun.file(`${import.meta.dir}/../${path}`).json();
      const modelIds = new Set((program.models ?? []).map((m: { id: string }) => m.id));
      const classIds = new Set(program.classes.map((c: { id: string }) => c.id));

      // Every class model and the anchor model must be declared.
      for (const cls of program.classes) {
        expect(modelIds.has(cls.model)).toBe(true);
      }
      if (program.anchor_model) {
        expect(modelIds.has(program.anchor_model)).toBe(true);
      }

      // Every cell must address a real class (or "*").
      for (const cell of program.cells) {
        if (cell.class !== "*") {
          expect(classIds.has(cell.class)).toBe(true);
        }
      }

      // Frozen programs must pin every asset hash.
      if (program.status === "frozen") {
        for (const asset of program.runtime.assets) {
          expect(asset.sha256).toMatch(/^[0-9a-f]{64}$/);
          expect(asset.server_path).toBeTruthy();
        }
      }
    });
  }
});
