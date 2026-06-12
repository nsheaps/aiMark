import { describe, expect, test } from "bun:test";
import { validateRunV1 } from "../src/index";
import validMinimal from "../testdata/run-v1-valid-minimal.json";

describe("aimark.run.v1 schema", () => {
  test("accepts the minimal valid fixture", () => {
    const ok = validateRunV1(validMinimal);
    expect(validateRunV1.errors ?? []).toEqual([]);
    expect(ok).toBe(true);
  });

  test("rejects a payload missing required fields", () => {
    expect(validateRunV1({ schema_version: "aimark.run.v1" })).toBe(false);
  });

  test("rejects unknown top-level properties", () => {
    expect(validateRunV1({ ...validMinimal, bogus: true })).toBe(false);
  });
});
