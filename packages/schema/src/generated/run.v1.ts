/* Generated from schemas/run.v1.json — do not edit. Run `mise run codegen`. */

/**
 * A single aiMark benchmark run: one suite executed against one target with one parameter vector. This envelope is what `aimark submit` sends (raw samples upload separately to blob storage).
 */
export interface AimarkRunV1 {
  schema_version: "aimark.run.v1";
  /**
   * ULID assigned by the CLI at run start
   */
  run_id: string;
  /**
   * Shared ULID when this run is one cell of a parameter sweep
   */
  sweep_id?: string;
  /**
   * Shared ULID when this run is one cell of a zero-choice benchmark program
   */
  bench_id?: string;
  created_at: string;
  /**
   * Where the run originated. The CLI auto-detects CI environments (CI/GITHUB_ACTIONS env vars) and flags them; ci and dev runs are excluded from default leaderboards.
   */
  source: "user" | "ci" | "dev";
  cli: {
    version: string;
    commit?: string;
    os: "darwin" | "linux" | "windows";
    arch: "amd64" | "arm64";
  };
  suite: {
    /**
     * Suite family, e.g. "sprint"
     */
    id: string;
    version: number;
    /**
     * SHA-256 of the frozen suite manifest the CLI executed
     */
    protocol_hash?: string;
  };
  target: {
    kind: "local" | "hosted";
    /**
     * Local runtime id (ollama, llamacpp, vllm, lmstudio) — local kind only
     */
    runtime?: string;
    runtime_version?: string;
    /**
     * Hosted provider id (anthropic, openai, google, bedrock, openrouter) — hosted kind only
     */
    provider?: string;
    region?: string;
    /**
     * Model identifier as the target reports it
     */
    model: string;
    /**
     * Content digest of the model weights where the runtime exposes one
     */
    model_digest?: string;
    quantization?: string;
    /**
     * Declared parameter count in billions, when known
     */
    declared_params_b?: number;
    /**
     * Captured request parameter vector (num_ctx, concurrency, temperature, prompt_caching, ...)
     */
    params?: {
      [k: string]: string | number | boolean;
    };
  };
  environment?: {
    hardware_profile?: HardwareProfile;
    /**
     * Pre-flight RTT to the hosted endpoint, reported separately from TTFT
     */
    network_rtt_ms?: number;
  };
  /**
   * Aggregate metrics keyed by metric id (ttft_ms_p50, decode_tps_mean, ...)
   */
  metrics: {
    [k: string]: number;
  };
  /**
   * Scores the CLI computed locally; the server recompute is canonical
   */
  provisional_scores?: {
    performance?: number;
    quality?: number;
    cost_efficiency?: number;
    consistency?: number;
    composite?: number;
  };
  /**
   * Hosted track: provider pricing at run time
   */
  pricing_snapshot?: {
    currency?: string;
    input_per_mtok?: number;
    output_per_mtok?: number;
    captured_at?: string;
  };
  integrity?: {
    payload_sha256: string;
    hmac?: string;
    /**
     * CLI release key generation identifier
     */
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
