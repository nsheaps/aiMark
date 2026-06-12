/* Generated from schemas/suite-manifest.v1.json — do not edit. Run `mise run codegen`. */

/**
 * A frozen benchmark suite definition. Weights, reference baselines, prompts, and protocol are data — bumping any of them requires a new suite version (new leaderboard).
 */
export interface AimarkSuiteManifestV1 {
  id: string;
  version: number;
  /**
   * Only frozen suites produce comparable leaderboard scores
   */
  status: "draft" | "frozen" | "retired";
  title: string;
  description?: string;
  /**
   * @minItems 1
   */
  tracks: ["local" | "hosted", ...("local" | "hosted")[]];
  /**
   * Provenance of the reference baseline that anchors this suite's 1000-point scale
   */
  reference?: {
    label?: string;
    notes?: string;
  };
  protocol: {
    warmup_requests: number;
    repetitions: number;
    request_timeout_ms: number;
    streaming: boolean;
    /**
     * Marathon-style suites run the task set at each level
     */
    concurrency_levels?: number[];
    decoding: {
      temperature: number;
      max_tokens: number;
    };
  };
  /**
   * @minItems 1
   */
  tasks: [
    {
      id: string;
      prompt: string;
      system?: string;
      /**
       * Objective grading spec (quality suites). Absent for pure-latency suites.
       */
      grading?: {
        kind: "exact" | "numeric_tolerance" | "json_schema" | "contains_all" | "js_tests";
        expected?: string | number;
        tolerance?: number;
        schema?: {
          [k: string]: unknown;
        };
        required_substrings?: string[];
        /**
         * JS test source for goja (Forge)
         */
        tests?: string;
      };
    },
    ...{
      id: string;
      prompt: string;
      system?: string;
      /**
       * Objective grading spec (quality suites). Absent for pure-latency suites.
       */
      grading?: {
        kind: "exact" | "numeric_tolerance" | "json_schema" | "contains_all" | "js_tests";
        expected?: string | number;
        tolerance?: number;
        schema?: {
          [k: string]: unknown;
        };
        required_substrings?: string[];
        /**
         * JS test source for goja (Forge)
         */
        tests?: string;
      };
    }[],
  ];
  scoring: {
    performance?: SubScoreSpec;
    quality?: SubScoreSpec;
    cost_efficiency?: SubScoreSpec;
    consistency?: SubScoreSpec;
    composite_weights: {
      [k: string]: number;
    };
  };
  /**
   * Hard physical bounds per metric; submissions outside are auto-flagged
   */
  plausibility?: {
    [k: string]: {
      min?: number;
      max?: number;
    };
  };
}
export interface SubScoreSpec {
  /**
   * @minItems 1
   */
  metrics: [
    {
      key: string;
      reference: number;
      weight: number;
      lower_is_better?: boolean;
    },
    ...{
      key: string;
      reference: number;
      weight: number;
      lower_is_better?: boolean;
    }[],
  ];
}
