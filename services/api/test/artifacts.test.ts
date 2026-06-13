import { describe, expect, test } from "bun:test";
import { sha256HexBytes } from "../src/canonical";
import { createTestApp, submitOk, TEST_BASE_URL } from "./helpers";
import type { App } from "../src/app";

const encoder = new TextEncoder();

interface PresignResponse {
  upload_url: string;
  key: string;
  expires_at: string;
}

interface ArtifactListResponse {
  run_id: string;
  artifacts: {
    key: string;
    kind: string;
    sha256: string;
    size_bytes: number;
    created_at: string;
    uploaded: boolean;
  }[];
}

function presign(app: App, runId: string, body: unknown) {
  return app.request(`/v1/runs/${runId}/artifacts/presign`, {
    method: "POST",
    body: JSON.stringify(body),
    headers: { "content-type": "application/json" },
  });
}

async function presignOk(
  app: App,
  runId: string,
  claimToken: string,
  data: Uint8Array,
): Promise<PresignResponse> {
  const res = await presign(app, runId, {
    kind: "samples",
    content_sha256: await sha256HexBytes(data),
    size_bytes: data.byteLength,
    claim_token: claimToken,
  });
  expect(res.status).toBe(201);
  return (await res.json()) as PresignResponse;
}

function upload(app: App, uploadUrl: string, data: Uint8Array) {
  return app.request(new URL(uploadUrl).pathname, { method: "PUT", body: data });
}

describe("artifact upload flow", () => {
  test("presign -> upload -> list happy path", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);
    const data = encoder.encode('{"samples":[{"task":"greet","output":"Hello!"}]}');

    const presigned = await presignOk(app, submitted.run_id, submitted.claim_token, data);
    expect(presigned.upload_url).toBe(`${TEST_BASE_URL}/v1/artifacts/${presigned.key}`);
    expect(Date.parse(presigned.expires_at)).toBeGreaterThan(Date.now());

    const put = await upload(app, presigned.upload_url, data);
    expect(put.status).toBe(201);
    const putBody = (await put.json()) as { ok: boolean; key: string; run_id: string };
    expect(putBody.ok).toBe(true);
    expect(putBody.key).toBe(presigned.key);
    expect(putBody.run_id).toBe(submitted.run_id);

    const list = await app.request(`/v1/runs/${submitted.run_id}/artifacts`);
    expect(list.status).toBe(200);
    const listBody = (await list.json()) as ArtifactListResponse;
    expect(listBody.run_id).toBe(submitted.run_id);
    expect(listBody.artifacts.length).toBe(1);
    const artifact = listBody.artifacts[0];
    expect(artifact?.key).toBe(presigned.key);
    expect(artifact?.kind).toBe("samples");
    expect(artifact?.sha256).toBe(await sha256HexBytes(data));
    expect(artifact?.size_bytes).toBe(data.byteLength);
    expect(artifact?.uploaded).toBe(true);
  });

  test("uploaded content not matching the declared sha256 -> 422", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);
    const declared = encoder.encode("the bytes I promised");

    const presigned = await presignOk(app, submitted.run_id, submitted.claim_token, declared);
    const res = await upload(app, presigned.upload_url, encoder.encode("different bytes"));
    expect(res.status).toBe(422);
    const body = (await res.json()) as { error: string };
    expect(body.error).toContain("sha256 mismatch");

    // The failed upload stored nothing — the key is still usable once.
    const list = (await (
      await app.request(`/v1/runs/${submitted.run_id}/artifacts`)
    ).json()) as ArtifactListResponse;
    expect(list.artifacts[0]?.uploaded).toBe(false);
  });

  test("wrong claim token -> 403; unknown run -> 404", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);
    const data = encoder.encode("samples");

    const wrongToken = await presign(app, submitted.run_id, {
      kind: "samples",
      content_sha256: await sha256HexBytes(data),
      size_bytes: data.byteLength,
      claim_token: "0".repeat(64),
    });
    expect(wrongToken.status).toBe(403);

    const unknownRun = await presign(app, "01AAAAAAAAAAAAAAAAAAAAAAAA", {
      kind: "samples",
      content_sha256: await sha256HexBytes(data),
      size_bytes: data.byteLength,
      claim_token: submitted.claim_token,
    });
    expect(unknownRun.status).toBe(404);
  });

  test("re-upload of the same key -> 409", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);
    const data = encoder.encode("upload me once");

    const presigned = await presignOk(app, submitted.run_id, submitted.claim_token, data);
    expect((await upload(app, presigned.upload_url, data)).status).toBe(201);
    const again = await upload(app, presigned.upload_url, data);
    expect(again.status).toBe(409);
  });

  test("declared size above the 10MB cap -> 413", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);
    const res = await presign(app, submitted.run_id, {
      kind: "samples",
      content_sha256: "a".repeat(64),
      size_bytes: 10 * 1024 * 1024 + 1,
      claim_token: submitted.claim_token,
    });
    expect(res.status).toBe(413);
  });

  test("malformed presign bodies -> 400; unknown upload key -> 404", async () => {
    const app = await createTestApp();
    const submitted = await submitOk(app);
    for (const bad of [
      { kind: "weights", content_sha256: "a".repeat(64), size_bytes: 1, claim_token: "t" },
      { kind: "samples", content_sha256: "not-hex", size_bytes: 1, claim_token: "t" },
      { kind: "samples", content_sha256: "a".repeat(64), size_bytes: 0, claim_token: "t" },
      { kind: "samples", content_sha256: "a".repeat(64), size_bytes: 1.5, claim_token: "t" },
      { kind: "samples", content_sha256: "a".repeat(64), size_bytes: 1 },
    ]) {
      expect((await presign(app, submitted.run_id, bad)).status).toBe(400);
    }

    const res = await app.request("/v1/artifacts/nonexistent-key", {
      method: "PUT",
      body: "data",
    });
    expect(res.status).toBe(404);
  });
});
