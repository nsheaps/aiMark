import { describe, expect, test } from "bun:test";
import { canonicalize, hmacSha256Hex, sha256Hex, DEV_HMAC_KEY } from "../src/canonical";

describe("canonicalize", () => {
  test("is stable across key insertion order", () => {
    const a = { b: 1, a: { y: true, x: "s" }, c: [1, 2] };
    const b = { c: [1, 2], a: { x: "s", y: true }, b: 1 };
    expect(canonicalize(a)).toBe(canonicalize(b));
    expect(canonicalize(a)).toBe('{"a":{"x":"s","y":true},"b":1,"c":[1,2]}');
  });

  test("preserves array order", () => {
    expect(canonicalize([3, 1, 2])).toBe("[3,1,2]");
    expect(canonicalize({ k: ["b", "a"] })).toBe('{"k":["b","a"]}');
  });

  test("handles primitives, null, and nesting without extra whitespace", () => {
    expect(canonicalize(null)).toBe("null");
    expect(canonicalize(1.5)).toBe("1.5");
    expect(canonicalize("héllo")).toBe('"héllo"');
    expect(canonicalize(true)).toBe("true");
    expect(canonicalize({ a: { b: { c: [] } } })).toBe('{"a":{"b":{"c":[]}}}');
  });

  test("skips undefined object values like JSON.stringify", () => {
    expect(canonicalize({ a: 1, b: undefined })).toBe('{"a":1}');
  });

  test("nested objects inside arrays get sorted keys", () => {
    expect(canonicalize([{ b: 1, a: 2 }])).toBe('[{"a":2,"b":1}]');
  });
});

describe("crypto helpers", () => {
  test("sha256Hex matches a known vector", async () => {
    expect(await sha256Hex("abc")).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
  });

  test("hmacSha256Hex is deterministic for the dev key", async () => {
    const one = await hmacSha256Hex(DEV_HMAC_KEY, "message");
    const two = await hmacSha256Hex(DEV_HMAC_KEY, "message");
    expect(one).toBe(two);
    expect(one).toMatch(/^[0-9a-f]{64}$/);
    expect(await hmacSha256Hex(DEV_HMAC_KEY, "other")).not.toBe(one);
  });
});
