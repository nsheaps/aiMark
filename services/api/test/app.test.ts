import { describe, expect, test } from "bun:test";
import { app } from "../src/app";
import validRun from "@aimark/schema/testdata/run-v1-valid-minimal.json";

describe("api", () => {
  test("GET /v1/health", async () => {
    const res = await app.request("/v1/health");
    expect(res.status).toBe(200);
    const body = (await res.json()) as { ok: boolean };
    expect(body.ok).toBe(true);
  });

  test("POST /v1/runs rejects invalid payloads with 422", async () => {
    const res = await app.request("/v1/runs", {
      method: "POST",
      body: JSON.stringify({ nope: true }),
      headers: { "content-type": "application/json" },
    });
    expect(res.status).toBe(422);
  });

  test("POST /v1/runs accepts a schema-valid payload (501 until pipeline lands)", async () => {
    const res = await app.request("/v1/runs", {
      method: "POST",
      body: JSON.stringify(validRun),
      headers: { "content-type": "application/json" },
    });
    expect(res.status).toBe(501);
  });
});
