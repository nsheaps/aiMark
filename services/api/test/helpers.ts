import { Database } from "bun:sqlite";
import { drizzle } from "drizzle-orm/bun-sqlite";
import type { AimarkBenchmarkV1, AimarkRunV1 } from "@aimark/schema";
import fixture from "@aimark/schema/testdata/run-v1-valid-minimal.json";
import { createApp, type App } from "../src/app";
import * as schema from "../src/db/schema";
import { migrate } from "../src/db/migrate";
import { seedPrograms, seedSuites } from "../src/db/seed";
import { canonicalize, hmacSha256Hex, sha256Hex, DEV_HMAC_KEY } from "../src/canonical";
import { InMemoryBlobStore, InMemoryRateLimiter } from "../src/deps";

export const TEST_ADMIN_TOKEN = "test-admin-token";
export const TEST_BASE_URL = "http://test.local";

/**
 * Minimal inline benchmark-program.v1 fixture. Tests seed THIS, never the
 * real packages/suites/programs/bench-1/program.json (which is owned by the
 * CLI side and may change independently). Two classes, three cells.
 */
export const TEST_PROGRAM = {
  id: "testbench",
  version: 1,
  status: "frozen",
  title: "Test Bench",
  runtime: {
    engine: "llama.cpp",
    build: "b0000",
    assets: [
      {
        os: "linux",
        arch: "amd64",
        accel: "cpu",
        url: "https://example.test/llama-cpu.tar.gz",
        sha256: "a".repeat(64),
      },
    ],
  },
  models: [
    {
      id: "tiny-model",
      family: "test",
      quant: "Q4_K_M",
      url: "https://example.test/tiny.gguf",
      sha256: "b".repeat(64),
    },
    {
      id: "mid-model",
      family: "test",
      quant: "Q4_K_M",
      url: "https://example.test/mid.gguf",
      sha256: "c".repeat(64),
    },
  ],
  classes: [
    { id: "compact", title: "Compact", cpu_only: true, accel_mem_max_gb: 6, model: "tiny-model" },
    {
      id: "mainstream",
      title: "Mainstream",
      accel_mem_min_gb: 6,
      accel_mem_max_gb: 16,
      model: "mid-model",
    },
  ],
  anchor_model: "tiny-model",
  cells: [
    {
      id: "sprint-class",
      class: "*",
      suite: "sprint",
      suite_version: 1,
      model: "class",
      role: "score",
      weight: 3,
    },
    {
      id: "sprint-anchor",
      class: "mainstream",
      suite: "sprint",
      suite_version: 1,
      model: "anchor",
      role: "score",
      weight: 1,
    },
    {
      id: "gauntlet-validity",
      class: "*",
      suite: "gauntlet",
      suite_version: 1,
      model: "class",
      role: "validity",
      validity_min_accuracy: 0.5,
    },
  ],
} as const;

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
  await seedPrograms(db, [TEST_PROGRAM as unknown as Record<string, unknown>]);
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

/** Re-signs an envelope with the dev HMAC key (matches what the CLI does).
 * Works for run.v1 and benchmark.v1 alike — the canonicalizer and key are
 * shared. */
export async function signRun(envelope: AimarkRunV1 | AimarkBenchmarkV1): Promise<void> {
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

// ------------------------------------------------------ benchmark.v1 helpers

/**
 * Builds a valid, signed benchmark.v1 envelope against TEST_PROGRAM. Callers
 * provide the cells (usually run ids from prior signed cell-run submissions
 * that carry the same bench_id) and may mutate before signing.
 */
export async function makeValidBenchmark(options: {
  benchId?: string;
  class?: string;
  cells: { cell_id: string; run_id: string; role: "score" | "validity" }[];
  mutate?: (env: AimarkBenchmarkV1) => void;
}): Promise<AimarkBenchmarkV1> {
  const envelope: AimarkBenchmarkV1 = {
    schema_version: "aimark.benchmark.v1",
    bench_id: options.benchId ?? randomUlid(),
    created_at: "2026-06-12T12:00:00Z",
    source: "user",
    cli: { version: "0.1.0", os: "linux", arch: "amd64" },
    program: { id: TEST_PROGRAM.id, version: TEST_PROGRAM.version },
    class: options.class ?? "compact",
    classification: {
      accel_mem_gb: 0,
      cpu_only: true,
      unified_memory: false,
      detail: "CPU-only machine — Compact regardless of RAM",
    },
    environment: {
      hardware_profile: {
        cpu_model: "AMD Ryzen 9 7950X",
        cpu_cores_physical: 16,
        ram_gb: 64,
        unified_memory: false,
        gpus: [{ name: "NVIDIA GeForce RTX 4090", vram_gb: 24, vendor: "nvidia" }],
        os: "linux",
        arch: "amd64",
      },
    },
    cells: options.cells as AimarkBenchmarkV1["cells"],
  };
  options.mutate?.(envelope);
  await signRun(envelope);
  return envelope;
}

export function submitBenchmark(app: App, body: unknown, headers?: Record<string, string>) {
  return app.request("/v1/benchmarks", {
    method: "POST",
    body: JSON.stringify(body),
    headers: { "content-type": "application/json", ...headers },
  });
}

export interface BenchmarkSubmitResponse {
  bench_id: string;
  status: string;
  flag_reason: string | null;
  class: string;
  scores: Record<string, number>;
  claim_token: string;
  public_url: string;
}

/**
 * Submits a full happy-path benchmark: signed cell runs (sprint score cell +
 * gauntlet validity cell for class compact) followed by the benchmark
 * envelope. Returns the responses so tests can assert on the recompute.
 */
export async function submitBenchmarkOk(
  app: App,
  options?: {
    benchId?: string;
    mutateBench?: (env: AimarkBenchmarkV1) => void;
    mutateCellRun?: (env: AimarkRunV1) => void;
  },
): Promise<{ bench: BenchmarkSubmitResponse; cellRuns: SubmitResponse[] }> {
  const benchId = options?.benchId ?? randomUlid();
  const sprintRun = await submitOk(app, (env) => {
    env.bench_id = benchId;
    options?.mutateCellRun?.(env);
  });
  const gauntletRun = await submitOk(app, (env) => {
    env.bench_id = benchId;
    env.suite = { id: "gauntlet", version: 1, protocol_hash: "0".repeat(64) };
    env.metrics = { quality_accuracy: 0.9, decode_tps_mean: 80, latency_ms_p95: 1200 };
    options?.mutateCellRun?.(env);
  });
  const bench = await makeValidBenchmark({
    benchId,
    class: "compact",
    cells: [
      { cell_id: "sprint-class", run_id: sprintRun.run_id, role: "score" },
      { cell_id: "gauntlet-validity", run_id: gauntletRun.run_id, role: "validity" },
    ],
    mutate: options?.mutateBench,
  });
  const res = await submitBenchmark(app, bench);
  if (res.status !== 201) {
    throw new Error(`expected 201, got ${res.status}: ${await res.text()}`);
  }
  return {
    bench: (await res.json()) as BenchmarkSubmitResponse,
    cellRuns: [sprintRun, gauntletRun],
  };
}
