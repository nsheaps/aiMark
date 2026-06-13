import { Database } from "bun:sqlite";
import { drizzle } from "drizzle-orm/bun-sqlite";
import type { AimarkRunV1 } from "@aimark/schema";
import fixture from "@aimark/schema/testdata/run-v1-valid-minimal.json";
import { createApp, type App } from "../src/app";
import * as schema from "../src/db/schema";
import { migrate } from "../src/db/migrate";
import { seedSuites } from "../src/db/seed";
import { canonicalize, hmacSha256Hex, sha256Hex, DEV_HMAC_KEY } from "../src/canonical";
import { InMemoryBlobStore, InMemoryRateLimiter } from "../src/deps";

export const TEST_ADMIN_TOKEN = "test-admin-token";
export const TEST_BASE_URL = "http://test.local";

export async function createTestApp(options?: {
  rateLimit?: number;
  now?: () => Date;
  outlierSigma?: number;
  outlierMinCohort?: number;
}): Promise<App> {
  const sqlite = new Database(":memory:");
  migrate(sqlite);
  const db = drizzle(sqlite, { schema });
  await seedSuites(db);
  return createApp({
    db,
    blobs: new InMemoryBlobStore(),
    rateLimiter: new InMemoryRateLimiter(options?.rateLimit ?? 1000),
    now: options?.now,
    baseUrl: TEST_BASE_URL,
    adminToken: TEST_ADMIN_TOKEN,
    outlierSigma: options?.outlierSigma,
    outlierMinCohort: options?.outlierMinCohort,
  });
}

const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

export function randomUlid(): string {
  let id = "";
  for (let i = 0; i < 26; i++) {
    id += ULID_ALPHABET[Math.floor(Math.random() * ULID_ALPHABET.length)];
  }
  return id;
}

/** Re-signs an envelope with the dev HMAC key (matches what the CLI does). */
export async function signRun(envelope: AimarkRunV1): Promise<void> {
  const { integrity, ...rest } = envelope;
  const message = canonicalize(rest);
  envelope.integrity = {
    payload_sha256: await sha256Hex(message),
    hmac: await hmacSha256Hex(DEV_HMAC_KEY, message),
    key_gen: "dev",
    nonce: integrity?.nonce ?? "01JXEXAMPLE0000000000NONCE",
  };
}

/**
 * Builds a valid, signed submission from the golden fixture. The fixture's
 * hmac is a placeholder, so we always re-sign after applying mutations.
 */
export async function makeValidRun(mutate?: (env: AimarkRunV1) => void): Promise<AimarkRunV1> {
  const envelope = structuredClone(fixture) as unknown as AimarkRunV1;
  envelope.run_id = randomUlid();
  mutate?.(envelope);
  await signRun(envelope);
  return envelope;
}

export function submit(app: App, body: unknown, headers?: Record<string, string>) {
  return app.request("/v1/runs", {
    method: "POST",
    body: JSON.stringify(body),
    headers: { "content-type": "application/json", ...headers },
  });
}

export interface SubmitResponse {
  run_id: string;
  status: string;
  flag_reason: string | null;
  scores: Record<string, number>;
  claim_token: string;
  public_url: string;
}

export async function submitOk(
  app: App,
  mutate?: (env: AimarkRunV1) => void,
): Promise<SubmitResponse> {
  const run = await makeValidRun(mutate);
  const res = await submit(app, run);
  if (res.status !== 201) {
    throw new Error(`expected 201, got ${res.status}: ${await res.text()}`);
  }
  return (await res.json()) as SubmitResponse;
}
