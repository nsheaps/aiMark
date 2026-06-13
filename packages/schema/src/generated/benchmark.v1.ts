/* Generated from schemas/benchmark.v1.json — do not edit. Run `mise run codegen`. */

/**
 * A zero-choice benchmark result: one machine, one capability class, the fixed program's cells, one aiMark System Score. Cells are submitted first as run.v1 envelopes sharing bench_id; this envelope ties them together. The server recomputes the composite from the cell scores it verified.
 */
export interface AimarkBenchmarkV1 {
  schema_version: "aimark.benchmark.v1";
  bench_id: string;
  created_at: string;
  source: "user" | "ci" | "dev";
  cli: {
    version: string;
    commit?: string;
    os: "darwin" | "linux" | "windows";
    arch: "amd64" | "arm64";
  };
  program: {
    id: string;
    version: number;
  };
  class: string;
  /**
   * Why the machine landed in its class — shown on the detail page
   */
  classification?: {
    accel_mem_gb?: number;
    cpu_only?: boolean;
    unified_memory?: boolean;
    detail?: string;
  };
  environment?: {
    hardware_profile?: HardwareProfile;
  };
  /**
   * @minItems 1
   */
  cells: [
    {
      cell_id: string;
      run_id: string;
      role: "score" | "validity";
    },
    ...{
      cell_id: string;
      run_id: string;
      role: "score" | "validity";
    }[],
  ];
  /**
   * CLI-computed; the server recompute from verified cells is canonical
   */
  provisional_scores?: {
    performance?: number;
    consistency?: number;
    composite?: number;
  };
  integrity?: {
    payload_sha256: string;
    hmac?: string;
    key_gen?: string;
    nonce: string;
  };
}
export interface HardwareProfile {
  /**
   * Hash of the canonical hardware fields, so identical rigs cluster
   */
  id?: string;
  cpu_model?: string;
  cpu_cores_physical?: number;
  cpu_cores_logical?: number;
  ram_gb?: number;
  unified_memory?: boolean;
  gpus?: {
    name: string;
    vram_gb?: number;
    vendor?: string;
  }[];
  os: string;
  os_version?: string;
  arch: string;
}
