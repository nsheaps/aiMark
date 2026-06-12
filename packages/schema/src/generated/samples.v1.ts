/* Generated from schemas/samples.v1.json — do not edit. Run `mise run codegen`. */

/**
 * Raw per-request samples for one run. Stored alongside the run envelope locally and uploaded to blob storage (R2) on submit — never to the relational DB.
 */
export interface AimarkSamplesV1 {
  schema_version: "aimark.samples.v1";
  run_id: string;
  samples: {
    task_id: string;
    repetition: number;
    concurrency?: number;
    /**
     * Warmup samples are recorded but excluded from metrics
     */
    warmup?: boolean;
    started_at: string;
    status: "ok" | "error" | "timeout";
    error?: string;
    /**
     * First content token, stream-event level, monotonic clock
     */
    ttft_ms?: number;
    latency_ms?: number;
    input_tokens?: number;
    output_tokens?: number;
    decode_tps?: number;
    inter_token_ms_mean?: number;
    /**
     * Quality suites: objective grading outcome
     */
    grade?: {
      passed?: boolean;
      detail?: string;
    };
  }[];
}
