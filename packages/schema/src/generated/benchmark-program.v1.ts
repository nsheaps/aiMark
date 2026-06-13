/* Generated from schemas/benchmark-program.v1.json — do not edit. Run `mise run codegen`. */

/**
 * A frozen zero-choice benchmark program: capability classes, pinned runtime builds, pinned model assets, and the (suite, model) cells each class runs. The user never chooses any of this — aimark classifies the machine and executes its class program.
 */
export interface AimarkBenchmarkProgramV1 {
  id: string;
  version: number;
  status: "draft" | "frozen" | "retired";
  title: string;
  description?: string;
  /**
   * The pinned inference engine — one engine version per program version, like 3DMark shipping its renderer
   */
  runtime: {
    engine: string;
    build: string;
    /**
     * @minItems 1
     */
    assets: [
      {
        os: "darwin" | "linux" | "windows";
        arch: "amd64" | "arm64";
        accel: "metal" | "cuda" | "vulkan" | "cpu";
        url: string;
        /**
         * null = unverified at freeze time; the CLI warns (trust-on-first-use) instead of failing
         */
        sha256?: string | null;
        size_bytes?: number;
        /**
         * Path of the server binary inside the archive
         */
        server_path?: string;
      },
      ...{
        os: "darwin" | "linux" | "windows";
        arch: "amd64" | "arm64";
        accel: "metal" | "cuda" | "vulkan" | "cpu";
        url: string;
        /**
         * null = unverified at freeze time; the CLI warns (trust-on-first-use) instead of failing
         */
        sha256?: string | null;
        size_bytes?: number;
        /**
         * Path of the server binary inside the archive
         */
        server_path?: string;
      }[],
    ];
  };
  /**
   * Pinned model assets referenced by cells
   *
   * @minItems 1
   */
  models?: [
    {
      id: string;
      family?: string;
      params_b?: number;
      quant?: string;
      url: string;
      sha256: string;
      size_bytes?: number;
    },
    ...{
      id: string;
      family?: string;
      params_b?: number;
      quant?: string;
      url: string;
      sha256: string;
      size_bytes?: number;
    }[],
  ];
  /**
   * @minItems 1
   */
  classes: [
    {
      id: string;
      title: string;
      description?: string;
      accel_mem_min_gb?: number;
      accel_mem_max_gb?: number;
      /**
       * true = this class accepts CPU-only machines (they classify here regardless of RAM)
       */
      cpu_only?: boolean;
      /**
       * models[].id of the class test model
       */
      model: string;
      /**
       * num_ctx used for class cells
       */
      context_budget?: number;
    },
    ...{
      id: string;
      title: string;
      description?: string;
      accel_mem_min_gb?: number;
      accel_mem_max_gb?: number;
      /**
       * true = this class accepts CPU-only machines (they classify here regardless of RAM)
       */
      cpu_only?: boolean;
      /**
       * models[].id of the class test model
       */
      model: string;
      /**
       * num_ctx used for class cells
       */
      context_budget?: number;
    }[],
  ];
  /**
   * models[].id every class also runs (cross-class reference cell)
   */
  anchor_model?: string;
  /**
   * The fixed test program. class '*' applies to every class; model 'class' resolves to the class model, 'anchor' to anchor_model.
   *
   * @minItems 1
   */
  cells: [
    {
      id: string;
      class: string;
      suite: string;
      suite_version: number;
      model: "class" | "anchor";
      /**
       * score cells feed the composite; validity cells (graded quality) only flag broken/cheated runs
       */
      role: "score" | "validity";
      weight?: number;
      params?: {
        [k: string]: string | number | boolean;
      };
      reps_override?: number;
      /**
       * validity cells: quality_accuracy below this flags the benchmark
       */
      validity_min_accuracy?: number;
    },
    ...{
      id: string;
      class: string;
      suite: string;
      suite_version: number;
      model: "class" | "anchor";
      /**
       * score cells feed the composite; validity cells (graded quality) only flag broken/cheated runs
       */
      role: "score" | "validity";
      weight?: number;
      params?: {
        [k: string]: string | number | boolean;
      };
      reps_override?: number;
      /**
       * validity cells: quality_accuracy below this flags the benchmark
       */
      validity_min_accuracy?: number;
    }[],
  ];
}
