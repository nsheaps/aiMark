/**
 * Seeds an aimark API instance with plausible, properly-signed user runs so
 * leaderboards and screenshots have realistic content.
 *
 *   bun tools/seed/seed.ts --api http://localhost:8787
 *
 * Signing matches the CLI/API dev integrity scheme: HMAC-SHA256 over
 * canonical JSON (sorted keys, no whitespace) of the envelope minus the
 * integrity block, key_gen "dev".
 */
import { createHmac, createHash, randomBytes } from "node:crypto";

const api = process.argv.includes("--api")
  ? process.argv[process.argv.indexOf("--api") + 1]
  : "http://localhost:8787";

const DEV_KEY = "aimark-dev-integrity-key-v0";
const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

function ulid(): string {
  let ts = Date.now();
  let out = "";
  for (let i = 0; i < 10; i++) {
    out = ULID_ALPHABET[ts % 32] + out;
    ts = Math.floor(ts / 32);
  }
  const rand = randomBytes(16);
  for (let i = 0; i < 16; i++) out += ULID_ALPHABET[rand[i]! % 32];
  return out;
}

function canonicalize(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonicalize).join(",")}]`;
  const entries = Object.entries(value as Record<string, unknown>)
    .filter(([, v]) => v !== undefined)
    .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
  return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${canonicalize(v)}`).join(",")}}`;
}

interface Rig {
  model: string;
  runtime?: string;
  provider?: string;
  kind: "local" | "hosted";
  quantization?: string;
  hw: { cpu: string; ram: number; gpu?: string; vram?: number; os: string; arch: string };
  perf: { tps: number; ttft: number; p95: number; cv: number };
}

const rigs: Rig[] = [
  { model: "llama3.1:8b-instruct-q4_K_M", runtime: "ollama", kind: "local", quantization: "Q4_K_M", hw: { cpu: "AMD Ryzen 9 7950X", ram: 64, gpu: "NVIDIA GeForce RTX 4090", vram: 24, os: "linux", arch: "amd64" }, perf: { tps: 118, ttft: 42, p95: 950, cv: 0.07 } },
  { model: "llama3.1:8b-instruct-q4_K_M", runtime: "ollama", kind: "local", quantization: "Q4_K_M", hw: { cpu: "Apple M3 Max", ram: 64, os: "darwin", arch: "arm64" }, perf: { tps: 71, ttft: 65, p95: 1400, cv: 0.09 } },
  { model: "llama3.1:70b-instruct-q4_K_M", runtime: "ollama", kind: "local", quantization: "Q4_K_M", hw: { cpu: "AMD Ryzen 9 7950X", ram: 64, gpu: "NVIDIA GeForce RTX 4090", vram: 24, os: "linux", arch: "amd64" }, perf: { tps: 19, ttft: 180, p95: 6400, cv: 0.12 } },
  { model: "qwen2.5:7b-instruct-q5_K_M", runtime: "ollama", kind: "local", quantization: "Q5_K_M", hw: { cpu: "Intel Core i9-14900K", ram: 96, gpu: "NVIDIA GeForce RTX 4080", vram: 16, os: "linux", arch: "amd64" }, perf: { tps: 96, ttft: 55, p95: 1100, cv: 0.08 } },
  { model: "mistral:7b-instruct-q4_0", runtime: "llamacpp", kind: "local", quantization: "Q4_0", hw: { cpu: "Apple M2 Pro", ram: 32, os: "darwin", arch: "arm64" }, perf: { tps: 52, ttft: 90, p95: 2100, cv: 0.11 } },
  { model: "gpt-4o-mini", provider: "openai", kind: "hosted", hw: { cpu: "n/a", ram: 0, os: "linux", arch: "amd64" }, perf: { tps: 140, ttft: 310, p95: 2400, cv: 0.21 } },
  { model: "claude-haiku-4-5", provider: "anthropic", kind: "hosted", hw: { cpu: "n/a", ram: 0, os: "darwin", arch: "arm64" }, perf: { tps: 165, ttft: 280, p95: 2100, cv: 0.18 } },
];

let submitted = 0;
for (const rig of rigs) {
  const jitter = () => 0.92 + Math.random() * 0.16;
  const metrics: Record<string, number> = {
    decode_tps_mean: rig.perf.tps * jitter(),
    ttft_ms_p50: rig.perf.ttft * jitter(),
    ttft_ms_p95: rig.perf.ttft * 1.6 * jitter(),
    ttft_ms_p99: rig.perf.ttft * 2.1 * jitter(),
    latency_ms_p50: rig.perf.p95 * 0.7 * jitter(),
    latency_ms_p95: rig.perf.p95 * jitter(),
    latency_ms_p99: rig.perf.p95 * 1.3 * jitter(),
    ttft_ms_cv: rig.perf.cv * jitter(),
    latency_ms_cv: rig.perf.cv * 0.9 * jitter(),
  };

  const envelope: Record<string, unknown> = {
    schema_version: "aimark.run.v1",
    run_id: ulid(),
    created_at: new Date(Date.now() - Math.floor(Math.random() * 72) * 3600_000).toISOString().replace(/\.\d{3}Z$/, "Z"),
    source: "user",
    cli: { version: "0.1.0", commit: "seed000", os: rig.hw.os, arch: rig.hw.arch },
    suite: { id: "sprint", version: 1 },
    target: {
      kind: rig.kind,
      ...(rig.runtime ? { runtime: rig.runtime, runtime_version: "0.6.0" } : {}),
      ...(rig.provider ? { provider: rig.provider, region: "us-east-1" } : {}),
      model: rig.model,
      ...(rig.quantization ? { quantization: rig.quantization } : {}),
      params: { num_ctx: 4096, concurrency: 1, temperature: 0 },
    },
    environment: {
      hardware_profile: {
        cpu_model: rig.hw.cpu,
        ram_gb: rig.hw.ram,
        unified_memory: rig.hw.os === "darwin",
        gpus: rig.hw.gpu ? [{ name: rig.hw.gpu, vram_gb: rig.hw.vram, vendor: "nvidia" }] : [],
        os: rig.hw.os,
        arch: rig.hw.arch,
      },
    },
    metrics,
  };

  const canonical = canonicalize(envelope);
  envelope.integrity = {
    payload_sha256: createHash("sha256").update(canonical).digest("hex"),
    hmac: createHmac("sha256", DEV_KEY).update(canonical).digest("hex"),
    key_gen: "dev",
    nonce: ulid(),
  };

  const res = await fetch(`${api}/v1/runs`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(envelope),
  });
  if (res.status === 201) submitted++;
  else console.error(`seed: ${rig.model} → HTTP ${res.status}: ${await res.text()}`);
}

console.log(`seeded ${submitted}/${rigs.length} runs to ${api}`);
