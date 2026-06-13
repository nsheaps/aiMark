/**
 * Seeds an aimark API with realistic zero-choice benchmark results across all
 * capability classes, so the systems leaderboards, /bench detail, model-fit
 * matrix, and home board have content for docs screenshots and local dev.
 *
 *   bun tools/seed/seed-benchmarks.ts --api http://localhost:8787
 *
 * For each benchmark it submits the program's cell runs (run.v1, sharing a
 * bench_id) and then the benchmark.v1 envelope — all HMAC-signed with the dev
 * key, exactly as the CLI does. Cell metrics are plausible and the gauntlet
 * validity cell clears its accuracy floor so benchmarks land accepted.
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

function sign(envelope: Record<string, unknown>): Record<string, unknown> {
  const canonical = canonicalize(envelope);
  envelope.integrity = {
    payload_sha256: createHash("sha256").update(canonical).digest("hex"),
    hmac: createHmac("sha256", DEV_KEY).update(canonical).digest("hex"),
    key_gen: "dev",
    nonce: ulid(),
  };
  return envelope;
}

const jit = (x: number) => x * (0.92 + Math.random() * 0.16);

// Metric vectors per suite, scaled by a rig `speed` factor (1 = reference).
function metricsFor(suite: string, speed: number): Record<string, number> {
  switch (suite) {
    case "sprint":
      return {
        decode_tps_mean: jit(100 * speed),
        ttft_ms_p50: jit(50 / speed),
        ttft_ms_p95: jit(80 / speed),
        ttft_ms_p99: jit(110 / speed),
        latency_ms_p50: jit(900 / speed),
        latency_ms_p95: jit(1500 / speed),
        latency_ms_p99: jit(2000 / speed),
        ttft_ms_cv: jit(0.08),
        latency_ms_cv: jit(0.07),
      };
    case "marathon":
      return {
        throughput_tps_c1: jit(100 * speed),
        throughput_tps_c16: jit(700 * speed),
        latency_ms_p99_c16: jit(8000 / speed),
        latency_ms_cv: jit(0.13),
        throughput_scaling: jit(0.7),
      };
    case "deepdive":
      return {
        quality_accuracy: Math.min(0.99, jit(0.85)),
        prefill_tps_mean: jit(500 * speed),
        ttft_ms_p95: jit(5000 / speed),
      };
    case "gauntlet":
      return {
        quality_accuracy: Math.min(0.99, jit(0.82)), // clears the 0.5 validity floor
        decode_tps_mean: jit(100 * speed),
        latency_ms_p95: jit(2800 / speed),
      };
    default:
      return {};
  }
}

interface Rig {
  cls: string;
  classModel: string;
  speed: number;
  hw: { cpu: string; ram: number; gpu?: string; vram?: number; os: string; arch: string; unified?: boolean };
  accelMem: number;
}

// Program: compact runs only class cells; mainstream/performance/ultra add a
// sprint-anchor cell (compact-model). gauntlet-validity is a validity cell.
const CLASS_CELLS: Record<string, Array<{ cell_id: string; suite: string; model: "class" | "anchor"; role: "score" | "validity" }>> = {
  compact: [
    { cell_id: "sprint-class", suite: "sprint", model: "class", role: "score" },
    { cell_id: "marathon-class", suite: "marathon", model: "class", role: "score" },
    { cell_id: "deepdive-class", suite: "deepdive", model: "class", role: "score" },
    { cell_id: "gauntlet-validity", suite: "gauntlet", model: "class", role: "validity" },
  ],
  mainstream: [],
  performance: [],
  ultra: [],
};
for (const cls of ["mainstream", "performance", "ultra"]) {
  CLASS_CELLS[cls] = [
    ...CLASS_CELLS.compact!.slice(0, 3),
    { cell_id: "sprint-anchor", suite: "sprint", model: "anchor", role: "score" },
    CLASS_CELLS.compact![3]!,
  ];
}

const CLASS_MODEL: Record<string, string> = {
  compact: "Qwen2.5-1.5B-Instruct-Q4_K_M",
  mainstream: "Qwen2.5-7B-Instruct-Q4_K_M",
  performance: "Qwen2.5-14B-Instruct-Q4_K_M",
  ultra: "Qwen2.5-32B-Instruct-Q4_K_M",
};
const ANCHOR_MODEL = "Qwen2.5-1.5B-Instruct-Q4_K_M";

const rigs: Rig[] = [
  { cls: "compact", classModel: CLASS_MODEL.compact!, speed: 0.55, accelMem: 0, hw: { cpu: "Intel Core i7-12700", ram: 32, os: "linux", arch: "amd64" } },
  { cls: "compact", classModel: CLASS_MODEL.compact!, speed: 0.78, accelMem: 0, hw: { cpu: "Apple M2", ram: 16, os: "darwin", arch: "arm64", unified: true } },
  { cls: "compact", classModel: CLASS_MODEL.compact!, speed: 0.42, accelMem: 0, hw: { cpu: "AMD Ryzen 5 5600X", ram: 32, os: "linux", arch: "amd64" } },
  { cls: "mainstream", classModel: CLASS_MODEL.mainstream!, speed: 1.4, accelMem: 12, hw: { cpu: "AMD Ryzen 7 7700X", ram: 32, gpu: "NVIDIA GeForce RTX 3060", vram: 12, os: "linux", arch: "amd64" } },
  { cls: "mainstream", classModel: CLASS_MODEL.mainstream!, speed: 1.65, accelMem: 16, hw: { cpu: "Apple M3 Pro", ram: 18, os: "darwin", arch: "arm64", unified: true } },
  { cls: "performance", classModel: CLASS_MODEL.performance!, speed: 2.3, accelMem: 16, hw: { cpu: "Intel Core i9-13900K", ram: 64, gpu: "NVIDIA GeForce RTX 4080", vram: 16, os: "linux", arch: "amd64" } },
  { cls: "performance", classModel: CLASS_MODEL.performance!, speed: 2.1, accelMem: 24, hw: { cpu: "Apple M3 Max", ram: 48, os: "darwin", arch: "arm64", unified: true } },
  { cls: "ultra", classModel: CLASS_MODEL.ultra!, speed: 3.4, accelMem: 24, hw: { cpu: "AMD Ryzen 9 7950X3D", ram: 128, gpu: "NVIDIA GeForce RTX 4090", vram: 24, os: "linux", arch: "amd64" } },
  { cls: "ultra", classModel: CLASS_MODEL.ultra!, speed: 4.1, accelMem: 48, hw: { cpu: "AMD Threadripper PRO 5975WX", ram: 256, gpu: "NVIDIA RTX A6000", vram: 48, os: "linux", arch: "amd64" } },
  { cls: "ultra", classModel: CLASS_MODEL.ultra!, speed: 3.0, accelMem: 90, hw: { cpu: "Apple M3 Ultra", ram: 128, os: "darwin", arch: "arm64", unified: true } },
];

function hardwareProfile(rig: Rig) {
  return {
    cpu_model: rig.hw.cpu,
    ram_gb: rig.hw.ram,
    unified_memory: rig.hw.unified ?? false,
    gpus: rig.hw.gpu ? [{ name: rig.hw.gpu, vram_gb: rig.hw.vram, vendor: "nvidia" }] : [],
    os: rig.hw.os,
    arch: rig.hw.arch,
  };
}

async function post(path: string, body: unknown): Promise<Response> {
  return fetch(`${api}${path}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

let ok = 0;
let fail = 0;
for (const rig of rigs) {
  const benchId = ulid();
  const cells = CLASS_CELLS[rig.cls]!;
  const cellRefs: Array<{ cell_id: string; run_id: string; role: "score" | "validity" }> = [];
  let cellFailed = false;

  for (const cell of cells) {
    const runId = ulid();
    const model = cell.model === "anchor" ? ANCHOR_MODEL : rig.classModel;
    // The anchor (1.5B) always runs fast; the class model scales with the rig.
    const speed = cell.model === "anchor" ? Math.max(rig.speed, 1.6) : rig.speed;
    const run = sign({
      schema_version: "aimark.run.v1",
      run_id: runId,
      bench_id: benchId,
      created_at: new Date(Date.now() - Math.floor(Math.random() * 96) * 3600_000)
        .toISOString()
        .replace(/\.\d{3}Z$/, "Z"),
      source: "user",
      cli: { version: "0.1.0", commit: "seed000", os: rig.hw.os, arch: rig.hw.arch },
      suite: { id: cell.suite, version: 1 },
      target: {
        kind: "local",
        runtime: "llamacpp",
        runtime_version: "b9616",
        model,
        quantization: "Q4_K_M",
        params: { num_ctx: 4096, concurrency: 1, temperature: 0 },
      },
      environment: { hardware_profile: hardwareProfile(rig) },
      metrics: metricsFor(cell.suite, speed),
    });
    const res = await post("/v1/runs", run);
    if (res.status !== 201) {
      console.error(`  cell ${cell.cell_id} (${rig.cls}) → HTTP ${res.status}: ${await res.text()}`);
      cellFailed = true;
      break;
    }
    cellRefs.push({ cell_id: cell.cell_id, run_id: runId, role: cell.role });
  }
  if (cellFailed) {
    fail++;
    continue;
  }

  const bench = sign({
    schema_version: "aimark.benchmark.v1",
    bench_id: benchId,
    created_at: new Date().toISOString().replace(/\.\d{3}Z$/, "Z"),
    source: "user",
    cli: { version: "0.1.0", commit: "seed000", os: rig.hw.os, arch: rig.hw.arch },
    program: { id: "bench", version: 1 },
    class: rig.cls,
    classification: {
      accel_mem_gb: rig.accelMem,
      cpu_only: !rig.hw.gpu && !rig.hw.unified,
      unified_memory: rig.hw.unified ?? false,
      detail: rig.hw.gpu
        ? `${rig.hw.gpu} with ${rig.hw.vram} GB VRAM`
        : rig.hw.unified
          ? `${rig.hw.ram} GB unified memory`
          : "CPU-only",
    },
    environment: { hardware_profile: hardwareProfile(rig) },
    cells: cellRefs,
  });
  const res = await post("/v1/benchmarks", bench);
  if (res.status === 201) ok++;
  else {
    fail++;
    console.error(`  benchmark ${rig.cls} → HTTP ${res.status}: ${await res.text()}`);
  }
}

console.log(`seeded ${ok}/${rigs.length} benchmarks to ${api} (${fail} failed)`);
