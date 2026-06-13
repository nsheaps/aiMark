# aiMark Platform Specification (Draft)

> Status: **draft** — approved as the initial build plan on 2026-06-12. Implementation notes appended inline as decisions land; delivery state tracked in `.claude/plans/delivery-goals.md`.

## Overview

aiMark is "3DMark for AI": a local benchmark tool (Go) runs standardized, versioned workload suites against AI models — both **local runtimes** (Ollama, llama.cpp, vLLM, LM Studio, MLX) and **hosted APIs** (Anthropic, OpenAI, Google, Bedrock, OpenRouter) — produces signed scores, and submits them to a public website with leaderboards, side-by-side comparisons, a parameter-impact explorer ("which parameters move scores"), and a novice-friendly glossary that defines every AI term. Free hosting, fronted by Cloudflare, full CI, one-command DX.

Core decisions: measure **everything** (performance + quality + cost + consistency); **Go CLI**, **Bun/TS web**; hosting chosen on DX/maintainability (research below); submissions **anonymous + optional GitHub login**.

---

## 1. Hosting decision: all-in Cloudflare (researched 2026-06)

| Platform                                                | Free tier reality                                                                                                                                                   | Verdict                       |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| **Cloudflare** (Workers + static assets + D1 + R2 + KV) | 100k Worker req/day, **unlimited free static asset requests**, D1: 5GB / 5M row-reads/day, R2: 10GB + **free egress**, KV 100k reads/day. Always-on, no card games. | ✅ **Chosen**                 |
| Vercel Hobby                                            | Generous numbers but **non-commercial use only**, hard-capped, can't buy headroom on Hobby                                                                          | ❌ ToS risk for a public site |
| Fly.io                                                  | **No free tier anymore** (PAYG, ~$2/mo floor, card required)                                                                                                        | ❌ not free                   |
| Render                                                  | Free web services **sleep after 15 min** (~50s cold start), free Postgres expires                                                                                   | ❌ bad for a public API       |
| Heroku / Railway                                        | No free tier / trial credit only                                                                                                                                    | ❌                            |

Cloudflare is also the only option where "fronted by Cloudflare" (the DNS/CDN requirement) is zero extra config, and it's one platform/one deploy/one dashboard — best maintainability. **Portability is preserved anyway** (cheap insurance, not extra work): Hono targets `app.fetch` (runs on Bun/Node/Workers identically), Drizzle's SQLite dialect covers local SQLite/libSQL/D1, blob storage sits behind a 3-method S3-compatible interface (R2 in prod, filesystem/MinIO in dev). Swapping hosts later touches only `services/api/entrypoints/*` + the deploy composite action.

Stack mapping: **Workers** (Hono API + static site assets in one Worker), **D1** (relational data), **R2** (raw run artifacts), **KV** (rate-limit state), **GitHub Actions cron → admin endpoint** (aggregate refresh; portable, no Workers Cron lock-in). Local dev runs on plain Bun + SQLite file (fast, no wrangler emulation needed for most work) with a `wrangler dev` mode for parity checks.

---

## 2. Benchmark methodology (the heart of the product)

### Score model

Four sub-scores + composite, all computed from a frozen per-suite-version manifest (weights + reference baselines are **data, not code**):

- **Performance** — decode tokens/sec, TTFT, latency p50/p95/p99, throughput under concurrency
- **Quality** — accuracy on objectively-graded task suites (no LLM-as-judge in v1)
- **Cost-Efficiency** — quality-per-dollar (hosted: pricing snapshot captured at run time; local: optional energy metering later)
- **Consistency** — inverse coefficient-of-variation across repetitions

`sub_score = 1000 × weighted geomean(metric / reference_metric)` — the 3DMark trick: each suite version ships a frozen reference baseline (e.g., Llama-3.1-8B-Q4 on Ollama on a reference machine ≈ 1000 points; twice as fast ≈ 2000). Composite **aiMark Score** = weighted geometric mean of applicable sub-scores (geomean so no one dimension dominates).

**Tracks**: scores comparable only within **(suite, version, track)** — `local` track (hardware captured, no cost) vs `hosted` track (provider/region captured, cost mandatory). Cross-track comparison is allowed only explicitly in the compare view, with caveats shown.

**Versioning like 3DMark generations**: a suite version freezes tasks, prompts, grading, baselines, weights, protocol. Any change → new version → new leaderboard. CLI version is independent; result schema (`aimark.run.v1`) versions independently too.

### Workload suites

| Suite               | Exercises                                                                                                                                                                            | Primary metrics                                                              |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------- |
| **Sprint**          | Interactive "chat feel": short prompts, single stream                                                                                                                                | TTFT, inter-token latency, p50/p95/p99                                       |
| **Marathon**        | Sustained throughput at concurrency 1/4/16                                                                                                                                           | aggregate tok/s, scaling curve, p99 under load                               |
| **Deep Dive**       | Long context 4k→128k: needle-in-haystack, multi-fact                                                                                                                                 | accuracy-by-depth, prefill tok/s, TTFT vs context                            |
| **Gauntlet**        | Quality: math (numeric answers), structured extraction → JSON, multi-constraint instructions                                                                                         | accuracy via exact/tolerant match, JSON Schema validation, constraint checks |
| **Forge**           | Coding: implement-from-spec + hidden unit tests, **JS executed in sandboxed `goja` interpreter embedded in the CLI** (pure Go, no I/O, instruction/time caps — no Docker dependency) | pass@1, compile rate                                                         |
| **Relay** (Phase 4) | Agentic tool use with simulated tools                                                                                                                                                | completion, tool-call validity, steps                                        |

Protocol per suite: fixed prompts/decoding params (temp 0 where determinism matters), warmup runs discarded, N≥20 repetitions, timeouts.

### Parameter matrix & sweeps ("which parameters vary the scores")

- **Captured dimensions on every run**: model + param count + quantization, runtime/provider + version + region, hardware profile (CPU/cores/RAM/GPU+VRAM/unified-memory/OS), context length, concurrency, prompt caching, streaming flags, timestamp, CLI version, pricing snapshot, and **`source` (`user` | `ci` | `dev`)** — the CLI auto-flags runs executing under CI environments; `ci`/`dev` runs are accepted and scored but excluded from default leaderboards (`include_ci=true` opt-in).
- **Sweeps**: `aimark run --sweep sweep.yaml` takes a matrix (model × num_ctx × concurrency × …); each cell is an independent scored run sharing a `sweep_id`.
- **Corpus-level answer**: every leaderboard is sliceable by any captured dimension, and the **parameter-impact explorer** computes effect sizes across all submissions ("quantization moves Sprint score X% on average").

### Integrity / anti-gaming

Honest framing: raise the bar + statistical detection, like 3DMark — a client-side benchmark can never be cryptographically trustworthy.

1. HMAC-signed canonical-JSON payloads (key embedded in official release binaries, rotated per minor version; extractability documented).
2. Environment capture: runtime versions, model digests (Ollama manifest/GGUF hash), OS fingerprint.
3. Statistical plausibility: per-cohort distributions; physically-impossible or >4σ results auto-flagged (visible but excluded from default boards).
4. Tiers: `unverified` (anon) → `claimed` (GitHub) → `verified` (signature + plausibility). Boards default to verified.
5. Dedup via payload-hash uniqueness + nonce/timestamp window.

---

## 3. Go CLI (`aimark`)

Commands (cobra): `detect` (hardware+runtime discovery), `doctor`, `suites list|info`, `run <suite> [--target ollama:llama3.1:8b] [--param k=v] [--sweep f.yaml] [--estimate]`, `results list|show|export`, `submit [--all-pending]` (offline-first: run now, submit later), `login|logout|whoami` (GitHub device flow), `config`, `version`. UX: bubbletea/lipgloss live progress + tok/s readout, `--json`/`--quiet`, big score card at the end.

- **Adapters** behind a `Target` interface (Detect / streaming Complete with monotonic-clock token timestamps / Pricing): one **OpenAI-compatible adapter** covers vLLM, LM Studio, llama.cpp server, Ollama-compat, OpenRouter, OpenAI; native adapters where metadata matters: `ollama` (digest, quantization, load time), `anthropic`, `google`, `bedrock`. TTFT = first content token at stream-event level. API keys never enter result files.
- **Result files**: `~/.local/share/aimark/results/<ulid>.aimark.json`, schema `aimark.run.v1` — envelope of cli/suite/target/environment + raw per-request `samples` + aggregate `metrics` + provisional scores + integrity block. Raw samples go to R2 on submit (presigned direct upload); aggregates to D1.
- **Hardware detection**: gopsutil + per-OS GPU probes (`system_profiler` / `nvidia-smi` / `rocm-smi` / `lspci` / Win32_VideoController); Apple Silicon unified-memory aware; canonical fields hash to a `hardware_profile_id` so identical rigs cluster. Failures degrade to "unknown", never block.
- **Scoring implemented twice** (Go provisional, TS canonical server-side) against **shared golden test vectors** in `packages/schema/testdata/` — formula kept trivially simple (weighted geomean over manifest constants) to make dual implementation safe.

---

## 4. Backend API (Hono on Workers, `/v1`)

Endpoints: `POST /runs` (submit), `POST /runs/:id/artifacts/presign`, `GET /runs/:id`, `PATCH /runs/:id` (claim/visibility/delete), `GET /leaderboard?suite&version&track&<any-dim>`, `GET /compare?ids=`, `GET /models[/:id/summary]`, `GET /hardware[/:id/summary]`, `GET /suites`, `GET /params/impact`, GitHub OAuth (web redirect + device flow), `GET /me`, `GET /users/:handle`.

**Submission pipeline** (ordered): JSON-Schema validate → known suite/version → HMAC + CLI-version check → payload-hash dedup → plausibility bounds → canonical score recompute (`packages/scoring`) → persist → aggregate refresh. Returns `{run_id, status, scores, claim_token (anon), public_url}`.

**Data model** (D1/Drizzle): `suites`, `models`, `runtimes`, `hardware_profiles` (id = hash), `users`, `runs` (full captured dimension vector, status, payload_sha256 UNIQUE, ip_hash), `scores`, `metrics` (aggregates), `artifacts` (R2 keys), plus precomputed `leaderboard_agg` and `param_impact_agg` so read paths are cached lookups (essential on free tier; lets the CDN cache leaderboard JSON hard).

Rate limiting: sliding window per hashed IP in KV (anon ~20/day; authed 10×). Sessions: signed JWT cookie; CLI holds a long-lived scoped token.

---

## 5. Frontend (Astro + React islands, served as Worker static assets)

Astro chosen because the site is content + read-mostly data: near-zero JS on doc pages, **MDX content collections are ideal for the glossary requirement**, minimal framework churn vs Next. Interactive bits (comparison picker, charts, filterable tables) are scoped React islands.

Pages: `/` (hero + headline boards + download), `/leaderboard/[suite]/[version]` (sliceable by any dimension, verified toggle), `/runs/[id]` (full detail + integrity status), `/compare?ids=` (radar chart, per-metric deltas, param-diff highlighting), `/models[/:id]`, `/hardware[/:id]`, `/explore/params` (parameter-impact explorer), `/docs/**` (methodology, how-to, FAQ), `/u/[handle]` (profile, claimed runs, badges).

**Novice-first docs**: `/docs/glossary` with ~80 MDX entries (TTFT, token, quantization, context window, temperature, VRAM, p99, …) each with a plain-English `short_def`; a site-wide `<Term>` component renders hover tooltips so **any jargon anywhere on the site is one hover from a definition**.

---

## 6. Monorepo layout & DX (mirrors `nsheaps/agents` conventions)

```
aiMark/
├── mise.toml            # pins bun, node, go, golangci-lint, goreleaser, wrangler; tasks: format/lint/build/test/check/dev
├── package.json         # bun workspaces [services/*, packages/*, apps/cli]; nx root project
├── nx.json, tsconfig.base.json, renovate.json (+gomod), .release-it.json, .goreleaser.yaml, prettier configs
├── apps/
│   └── cli/             # Go module; package.json wraps go build/test/golangci-lint as nx targets
│       ├── cmd/aimark/main.go
│       └── internal/{adapters,detect,run,score,results,submit,auth,ui,sandbox,schema(generated)}
├── services/
│   ├── api/             # Hono; src/{routes,pipeline,db,blob,auth}; entrypoints/{worker.ts,bun.ts}
│   └── web/             # Astro; src/{pages,components,islands,content/{docs,glossary}}
├── packages/
│   ├── schema/          # SOURCE OF TRUTH: JSON Schemas (run.v1, suite-manifest.v1, hardware-profile.v1) + generated TS + golden testdata/
│   ├── scoring/         # canonical TS scoring
│   └── suites/          # suite manifests + task datasets (sprint-1/, …); embedded in CLI at build
└── .github/workflows/   # test.yaml, deploy.yaml, release-cli.yaml, release-web.yaml
```

- **Go↔TS contract**: JSON Schema is source of truth; codegen both ways (`omissis/go-jsonschema` → Go structs, `json-schema-to-typescript` → TS), committed; `lint` fails on stale codegen (fits the autofix format/lint pattern).
- **One-command dev**: `mise run dev` = Bun API on local SQLite + filesystem blobs + seeded fixture runs + Astro dev server. `mise run check` = lint+build+test via nx fan-out. CLI loop: `go run ./cmd/aimark run sprint-1 --api http://localhost:8787`.

### CI/CD

- `test.yaml` — agents-repo shape: autofix lint job (`mise run format` + auto-commit), build, test; adds Go/golangci caches.
- `deploy.yaml` — wrangler deploy (API Worker + static assets) on main; **preview deploy per PR** (workers.dev preview URL commented on the PR); D1 migrations applied via wrangler in the deploy job.
- `release-cli.yaml` — **`v*` tags** → goreleaser: darwin/linux/windows × amd64/arm64, checksums, GitHub Release, `install.sh` curl installer served from the site. (Implementation note: the CLI owns plain `v*` tags because goreleaser's monorepo tag prefixes are a paid feature; Homebrew tap publishing is prepared but disabled until the tap repo is reachable.)
- `release-web.yaml` — release-it + conventional changelog, **`web-v*` tags**, manual dispatch (deploys themselves are continuous on main).
- `e2e.yaml` — full product loop on every PR against `tools/mock-llm` (deterministic OpenAI-compatible SSE server, no model downloads): build CLI → benchmark → **anonymous submit** to a local API → assert the run is canonically scored, **flagged `source=ci`**, hidden from the default leaderboard, visible with `include_ci=true`, and claim-deletable. A real-Ollama variant (tiny model) runs nightly/on dispatch only.
- `docs-images.yaml` — weekly/manual regeneration of docs images: CLI output rendered to SVG via charmbracelet/freeze, site screenshots via Playwright against a seeded local stack; auto-commits to `docs/images/`.
- **Three independent version streams**: CLI semver (`vX.Y.Z`), web/API semver (`web-vX.Y.Z`), suite versions (named integers like `sprint-1`, frozen deliberately — bumping resets a leaderboard).

---

## 7. Phased delivery

- **Phase 0 — Scaffolding**: repo bootstrap (mise/bun/nx/prettier/renovate/release-it/test.yaml), wired-but-empty api/web/schema/cli projects, JSON Schema drafts + bidirectional codegen + staleness lint, goreleaser dry-run, wrangler deploy of hello-world site. _Done when CI is green, `mise run dev` works, `aimark version` installs from a pre-release._
- **Phase 1 — MVP**: **Sprint-1** suite; CLI `detect/doctor/run/results/submit` with Ollama + OpenAI-compat adapters, 3-OS hardware detection, HMAC signing; API submit pipeline + leaderboard + rate limiting + anon claim tokens; web home, Sprint leaderboard with filters, run detail, first 25 glossary terms, install page. _Done when a stranger can `brew install aimark`, benchmark Ollama, submit, and see their row live._
- **Phase 2 — Quality + identity + comparison**: **Gauntlet-1**, **Marathon-1**; GitHub OAuth (web + device flow), profiles, claiming; compare view, model explorer; cost capture + Cost-Efficiency + composite score; glossary complete + `<Term>` tooltips + methodology docs.
- **Phase 3 — Sweeps + analytics + integrity + breadth**: `--sweep`; **Deep Dive-1**, **Forge-1** (goja sandbox); parameter-impact explorer + precomputed aggregates; hardware explorer; verified tier fully enforced (plausibility/outlier flagging); native Anthropic/Google/Bedrock adapters, MLX via compat endpoints.
- **Phase 4 — Stretch**: **Relay-1** (agentic), energy metering (powermetrics/RAPL/nvml), Python tasks via optional container runner, rotating/procedural task pools for contamination resistance, score badges/embeds, public data dumps.

---

## 8. Key risks (with mitigations)

1. **Dataset contamination** (quality suites leak into training data) → procedurally generated task instances frozen per suite version; ~6-month suite cadence as rotation; documented openly.
2. **Hosted API variance** (time/region/load) → N≥20 reps, region+timestamp captured, Consistency sub-score, leaderboards show median-of-runs with count.
3. **TTFT fidelity** (network overhead) → stream-event-level measurement, separate RTT preflight, local vs hosted never implicitly compared.
4. **HMAC key extraction** → accepted; statistical detection is the real defense; future option: reproducibility challenges for top ranks.
5. **BYO API keys cost users money** → `--estimate` prints projected cost before hosted runs; suites sized ≲$1–2/run.
6. **Free-tier ceilings** → precomputed aggregates + CDN caching + presigned direct-to-R2 uploads keep Workers/D1 usage minimal; portability seams allow migration if outgrown.

---

## Manual setup required (owner action items)

1. Create a Cloudflare account/zone for the domain; add `CLOUDFLARE_API_TOKEN` + `CLOUDFLARE_ACCOUNT_ID` secrets to the repo (deploy workflow).
2. Create D1 database, R2 bucket, and KV namespace (`wrangler d1 create aimark`, `wrangler r2 bucket create aimark-artifacts`, `wrangler kv namespace create rate-limits`) and record IDs in `wrangler.toml`.
3. Phase 2: create a GitHub OAuth app (web flow) + device-flow client ID; add secrets.

## Verification

- Phase 0: `mise run check` green locally and in CI; `mise run dev` serves site+API; goreleaser `--snapshot` produces 6 binaries.
- Phase 1 e2e: run `aimark run sprint-1 --target ollama:<model>` against a local Ollama, `aimark submit`, confirm the run appears on the deployed leaderboard and `/runs/<id>` renders; golden score vectors pass in both Go and TS test suites.
