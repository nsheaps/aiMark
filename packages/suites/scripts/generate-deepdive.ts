#!/usr/bin/env bun
/**
 * Generates packages/suites/deepdive-1/manifest.json — the literal, frozen
 * Deep Dive suite: needle-in-haystack tasks over deterministic filler prose.
 *
 * The filler is produced by a seeded PRNG (mulberry32, SEED below) over a
 * fixed vocabulary, so re-running this script always reproduces the same
 * manifest byte-for-byte (modulo prettier formatting). The manifest is
 * checked in as the frozen artifact; this script documents its provenance.
 *
 * Usage: bun packages/suites/scripts/generate-deepdive.ts
 */

const SEED = 20260612;

/** mulberry32 — small deterministic PRNG. */
function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// Vocabulary for filler prose. Deliberately contains no digits and none of
// the planted-fact tokens, so a fact can never appear in the filler by
// accident.
const OPENERS = [
  "The morning mist settled over",
  "Travelers often remarked upon",
  "Few records survive describing",
  "The committee deliberated about",
  "Local custom long favored",
  "Seasonal winds reshaped",
  "The caretaker quietly maintained",
  "Old maps faintly indicate",
  "Merchants once traded along",
  "The journal entries describe",
];

const SUBJECTS = [
  "the granite terraces",
  "the weathered boathouse",
  "the northern orchards",
  "the cobbled promenade",
  "the abandoned mill",
  "the salt marsh trails",
  "the village archive",
  "the harbor wall",
  "the cedar plantation",
  "the upper meadows",
];

const CONNECTORS = [
  "while the tide carried driftwood toward",
  "as lanterns flickered beside",
  "long before anyone surveyed",
  "though storms had battered",
  "whenever festivals filled",
  "until the railway bypassed",
  "because the council neglected",
  "after pilgrims abandoned",
];

const CLOSERS = [
  "the quiet estuary.",
  "the limestone quarry.",
  "the shepherd's crossing.",
  "the forgotten causeway.",
  "the western breakwater.",
  "the terraced vineyards.",
  "the old observatory.",
  "the market square.",
];

/** One deterministic filler sentence (~12 words). */
function sentence(rand: () => number): string {
  const pick = (xs: string[]) => xs[Math.floor(rand() * xs.length)];
  return `${pick(OPENERS)} ${pick(SUBJECTS)}, ${pick(CONNECTORS)} ${pick(CLOSERS)}`;
}

/**
 * Builds a document of roughly `words` words of filler with `fact` planted
 * at `depth` (0 = start, 1 = end).
 */
function document(rand: () => number, words: number, fact: string, depth: number): string {
  const sentences: string[] = [];
  let count = 0;
  while (count < words) {
    const s = sentence(rand);
    sentences.push(s);
    count += s.split(/\s+/).length;
  }
  const insertAt = Math.max(0, Math.min(sentences.length, Math.round(sentences.length * depth)));
  sentences.splice(insertAt, 0, fact);
  return sentences.join(" ");
}

interface TaskSpec {
  id: string;
  words: number;
  depth: number;
  fact: string;
  question: string;
  needles: string[];
}

const TASKS: TaskSpec[] = [
  {
    id: "needle-2k-early",
    words: 2000,
    depth: 0.25,
    fact: "The access code for the Meridian vault is 7341.",
    question:
      "What is the access code for the Meridian vault? Answer in one sentence and include the exact code.",
    needles: ["7341"],
  },
  {
    id: "needle-2k-late",
    words: 2000,
    depth: 0.75,
    fact: "The lighthouse keeper's cat is named Barnacle.",
    question:
      "What is the name of the lighthouse keeper's cat? Answer in one sentence and include the exact name.",
    needles: ["Barnacle"],
  },
  {
    id: "needle-4k-middle",
    words: 4000,
    depth: 0.5,
    fact: "Project Cobalt's launch window opens on October 19.",
    question:
      "On what date does Project Cobalt's launch window open? Answer in one sentence and include the exact date.",
    needles: ["October 19"],
  },
  {
    id: "needle-4k-deep",
    words: 4000,
    depth: 0.9,
    fact: "The rooftop greenhouse grows exactly 53 tomato plants.",
    question:
      "Exactly how many tomato plants does the rooftop greenhouse grow? Answer in one sentence and include the exact number.",
    needles: ["53"],
  },
  {
    id: "needle-8k-early",
    words: 8000,
    depth: 0.1,
    fact: "The ferry to Port Ellsworth departs from pier 12.",
    question:
      "From which pier does the ferry to Port Ellsworth depart? Answer in one sentence and include the exact pier number.",
    needles: ["12"],
  },
  {
    id: "needle-8k-middle",
    words: 8000,
    depth: 0.6,
    fact: "The archive's rarest manuscript is catalogued under the codename Silver Heron.",
    question:
      "Under what codename is the archive's rarest manuscript catalogued? Answer in one sentence and include the exact codename.",
    needles: ["Silver Heron"],
  },
];

function buildManifest(): unknown {
  const rand = mulberry32(SEED);
  const tasks = TASKS.map((t) => ({
    id: t.id,
    prompt: `Read the following document carefully, then answer the question at the end.\n\n${document(
      rand,
      t.words,
      t.fact,
      t.depth,
    )}\n\nQuestion: ${t.question}`,
    grading: {
      kind: "contains_all",
      required_substrings: t.needles,
    },
  }));

  return {
    id: "deepdive",
    version: 1,
    status: "frozen",
    title: "Deep Dive 1",
    description:
      "Long-context retrieval — needle-in-haystack questions planted in ~2k/4k/8k words of deterministic filler prose at varying depths. Filler generated by packages/suites/scripts/generate-deepdive.ts (mulberry32 PRNG, seed 20260612); this manifest is the frozen literal artifact.",
    tracks: ["local", "hosted"],
    reference: {
      label: "synthetic-1",
      notes:
        "v1 anchors the 1000-point scale to round-number synthetic baselines (0.8 accuracy, 500 tok/s prefill, 5000ms TTFT p95) rather than a measured reference rig. A future suite version will re-anchor to measured hardware; per the versioning rules that resets the leaderboard.",
    },
    protocol: {
      warmup_requests: 1,
      repetitions: 2,
      request_timeout_ms: 180000,
      streaming: true,
      decoding: {
        temperature: 0,
        max_tokens: 256,
      },
    },
    tasks,
    scoring: {
      quality: {
        metrics: [{ key: "quality_accuracy", reference: 0.8, weight: 1 }],
      },
      performance: {
        metrics: [
          { key: "prefill_tps_mean", reference: 500, weight: 1 },
          { key: "ttft_ms_p95", reference: 5000, weight: 1, lower_is_better: true },
        ],
      },
      composite_weights: {
        quality: 2,
        performance: 1,
      },
    },
    plausibility: {
      quality_accuracy: { min: 0, max: 1 },
      prefill_tps_mean: { min: 0.1, max: 1000000 },
      ttft_ms_p95: { min: 0.05, max: 600000 },
    },
  };
}

const outPath = new URL("../deepdive-1/manifest.json", import.meta.url).pathname;
const manifest = buildManifest();
await Bun.write(outPath, JSON.stringify(manifest, null, 2) + "\n");

const size = (await Bun.file(outPath).arrayBuffer()).byteLength;
console.log(`wrote ${outPath} (${(size / 1024).toFixed(1)} KB)`);
if (size > 300 * 1024) {
  console.error("ERROR: manifest exceeds 300KB budget");
  process.exit(1);
}
