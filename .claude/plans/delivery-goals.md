# aiMark Delivery Goals — branch `claude/ai-benchmark-platform-c12878`

Master checklist for "build it all the way to the end". Derived from
[the platform spec](../../docs/specs/draft/aimark-platform.md) (every phase) plus the
owner's added requirements (e2e CI, CI-source flagging, auto-updating docs images).
**Every status change must be reflected here before the goal is called done.**

Statuses: `[ ]` todo · `[x]` done+verified · `[~]` partial (note what's left) · `[B]` blocked on owner action

## G0 — Scaffolding (Phase 0)

- [x] G0.1 Monorepo: mise/bun/nx/prettier/renovate/release-it, format/lint/build/test/check/dev tasks
- [x] G0.2 Wired projects: apps/cli, services/api, services/web, packages/{schema,scoring,suites}
- [x] G0.3 test.yaml (autofix lint/build/test), deploy.yaml, release-cli.yaml, release-web.yaml; CI green
- [x] G0.4 goreleaser snapshot produces 6 platform archives
- [x] G0.5 `.claude` plugin config (1pass/github-app → Agent-Jack) verified end-to-end

## G1 — Data contracts

- [x] G1.1 `run.v1` schema (envelope: cli/suite/target/environment/metrics/scores/integrity)
- [x] G1.2 `run.v1` **source flag**: `source: user|ci|dev` — CI-originated runs are flagged at submit and excluded from default leaderboards
- [x] G1.3 `suite-manifest.v1` schema; sprint-1 manifest validates against it
- [x] G1.4 Bidirectional codegen committed: TS (json2ts) AND Go (go-jsonschema); `lint` fails on stale codegen
- [x] G1.5 Golden score vectors in `packages/schema/testdata/` passing in BOTH Go and TS test suites
- [x] G1.6 Raw per-request `samples` representation (artifact file format) documented in schema package

## G2 — CLI (Phases 1–3 scope)

- [x] G2.1 `aimark detect` — hardware (CPU/RAM/GPU/unified-memory, 3 OS, degrade-to-unknown) + runtime discovery; canonical `hardware_profile_id` hash
- [x] G2.2 `aimark doctor` — environment sanity checks
- [x] G2.3 `aimark suites list|info` — embedded suite manifests (go:embed from packages/suites)
- [x] G2.4 `aimark run <suite> --target <t>` — warmups, N reps, stream-event TTFT, monotonic token timestamps, metrics aggregation (p50/p95/p99, CV), provisional Go scoring, result file `~/.local/share/aimark/results/<ulid>.aimark.json`, score card output, `--json`/`--quiet`
- [x] G2.5 Adapters: OpenAI-compatible (covers vLLM/LM Studio/llama.cpp/OpenRouter/OpenAI) + native Ollama (digest/quantization metadata)
- [x] G2.6 `aimark results list|show|export`
- [x] G2.7 `aimark submit` (offline-first, `--all-pending`), anonymous; sends source flag (auto-detect CI env); claim token printed + stored
- [x] G2.8 HMAC integrity block (canonical JSON, embedded dev keygen, rotation structure)
- [x] G2.9 `--estimate` for hosted targets (projected cost)
- [x] G2.10 `aimark run --sweep sweep.yaml` — parameter matrix, shared sweep_id (Phase 3)
- [x] G2.11 Native hosted adapters: anthropic, google, bedrock (Phase 3)
- [x] G2.12 Forge sandbox: goja JS interpreter, no I/O, instruction/time caps (Phase 3)
- [x] G2.13 `login|logout|whoami` GitHub device flow (Phase 2) — code structure; e2e [B] on OAuth app

## G3 — API (Phases 1–3 scope)

- [x] G3.1 Drizzle schema (suites, models, runtimes, hardware_profiles, users, runs, scores, metrics, artifacts, leaderboard_agg) on bun:sqlite (dev/test) + D1 (prod); migrations
- [x] G3.2 POST /v1/runs pipeline: schema validate → known suite/version → HMAC + CLI-version check → payload-hash dedup → plausibility bounds → canonical recompute (packages/scoring) → persist → returns {run_id, status, scores, claim_token, public_url}
- [x] G3.3 GET /v1/leaderboard?suite&version&track + dimension filters; **excludes source=ci and flagged runs by default** (`include_ci=true` opt-in)
- [x] G3.4 GET /v1/runs/:id; PATCH /v1/runs/:id (claim/visibility/delete via claim token)
- [x] G3.5 GET /v1/suites; GET /v1/compare?ids=; GET /v1/models, /v1/hardware (+summaries)
- [x] G3.6 GET /v1/params/impact (effect sizes across submissions, Phase 3)
- [x] G3.7 Rate limiting: sliding window per hashed IP (KV in prod, memory/sqlite in dev)
- [x] G3.8 Artifacts: presign + one-shot upload + listing routes with sha256/size/TTL enforcement; R2-backed store when the worker binding exists (uploads proxy through the worker — true presigned R2 URLs need account-scoped S3 tokens). Was: Artifacts: presign endpoint + blob interface (R2 prod / filesystem dev)
- [~] G3.9 Plausibility bounds ✓, cross-cohort 4σ outlier flagging ✓ (env-tunable); unverified/flagged tiers live; "claimed/verified" tiers need OAuth [B G9.3]. Was: Statistical plausibility + >4σ outlier flagging; integrity tiers unverified/claimed/verified (Phase 3)
- [x] G3.10 GitHub OAuth web + device flow, /v1/me, profiles (Phase 2) — code structure; e2e [B] on OAuth app
- [x] G3.11 Aggregate refresh admin endpoint (GitHub Actions cron compatible)

## G4 — Web (Phases 1–2 scope)

- [x] G4.1 Home: hero, headline boards, download/install instructions
- [x] G4.2 /leaderboard/[suite]/[version]: sliceable table, track + verified/CI toggles
- [x] G4.3 /runs/[id]: full detail + integrity status
- [x] G4.4 /compare?ids=: per-metric deltas, param-diff highlighting
- [x] G4.5 /docs/glossary: ≥25 plain-English terms (Phase 1), full ~80 (Phase 2); `<Term>` hover tooltips
- [x] G4.6 /docs/methodology: scoring model, tracks, versioning, integrity honesty page
- [x] G4.7 /models, /hardware, /explore/params explorers (Phase 3)
- [x] G4.8 Install page + `install.sh` curl installer served from the site

## G5 — Suites (data)

- [x] G5.1 Sprint-1 frozen: prompts, protocol, reference baselines, weights (validates against suite-manifest.v1)
- [x] G5.2 Gauntlet-1: math/extraction/instruction tasks + objective graders (Phase 2)
- [x] G5.3 Marathon-1: concurrency 1/4/16 protocol (Phase 2)
- [x] G5.4 Deep Dive-1: context-length ladder + needle tasks (Phase 3)
- [x] G5.5 Forge-1: coding tasks + hidden tests via goja (Phase 3)
- [ ] G5.6 Relay-1: agentic tool-use suite (Phase 4 stretch)

## G6 — E2E CI (owner requirement)

- [x] G6.1 `tools/mock-llm`: deterministic OpenAI-compatible streaming server (no model download) for CI
- [x] G6.2 `e2e.yaml`: build CLI → `aimark run` against mock-llm → local API (bun+sqlite) → **anonymous submit** → assert run persisted, scores recomputed, claim token works
- [x] G6.3 E2E asserts **CI flagging**: submitted run has source=ci; default leaderboard hides it; `include_ci=true` shows it
- [x] G6.4 E2E exercises real-Ollama path on a schedule/manual dispatch (tiny model), not on every PR — verified: dispatch run 27452214397 succeeded (qwen2.5:0.5b benchmarked, submitted, flag assertions held)
- [x] G6.5 E2E green in CI on this branch (e2e-mock passed on 9a5cf56 and 4488a32)

## G7 — Auto-updating docs images (owner requirement)

- [x] G7.1 CLI terminal captures rendered to SVG/PNG (charmbracelet/freeze or equivalent) in CI
- [x] G7.2 Website screenshots (home, leaderboard, run detail) via Playwright against seeded local stack in CI
- [x] G7.3 `docs-images.yaml`: regenerates captures, auto-commits to `docs/images/` when changed (app-auth fallback pattern)
- [x] G7.4 README + docs reference the auto-updated images; images render on GitHub

## G8 — Release & distribution

- [x] G8.1 release-cli.yaml: v\* tags → goreleaser 6 targets + checksums + GitHub Release
- [x] G8.2 release-web.yaml: web-v\* via release-it
- [B] G8.3 Homebrew tap — homebrew_casks block prepared in .goreleaser.yaml with skip_upload: true; owner flips it on once nsheaps/homebrew-tap + HOMEBREW_TAP_TOKEN exist
- [ ] G8.4 First pre-release tag cut so `aimark version` installs from a release (after merge; needs owner merge decision)

## G9 — Owner-blocked items (tracked, not deliverable from this session)

- [B] G9.1 Cloudflare: account, CLOUDFLARE_API_TOKEN/ACCOUNT_ID secrets, D1/R2/KV creation, domain
- [B] G9.2 AUTOMATION*GITHUB_APP*\* secrets on aiMark (autofix commits + docs-image commits retrigger CI)
- [B] G9.3 GitHub OAuth app + device-flow client ID (Phase 2 identity)
- [B] G9.4 Homebrew tap repo access

## G10 — Phase 4 stretch (explicitly deferred unless time remains)

- [ ] G10.1 Energy metering (powermetrics/RAPL/nvml)
- [ ] G10.2 Python tasks via optional container runner
- [ ] G10.3 Rotating/procedural task pools for contamination resistance
- [ ] G10.4 Score badges/embeds; public data dumps

## Definition of "end" for this branch

All of G1–G7 done (or [B] with owner action documented), `mise run check` green,
e2e workflow green in CI, docs images generated and referenced, spec updated to
match what was built, goals file fully reconciled.

## GB — Zero-choice pivot (owner redirection, 2026-06-13)

Intent correction: aiMark is a SYSTEM benchmark — one command, no model/suite choices;
classes make scores comparable; models are test assets; cloud = monitoring reference;
site = hardware buying guidance + model-fit guidance + showing off. Decision assumptions
taken (flip-able, recorded in spec §0): bundled pinned llama.cpp; tiered download
budgets; central cron + optional user-run cloud monitoring; advanced mode kept.

- [x] GB1 Contracts: benchmark-program.v1 + benchmark.v1 schemas, bench_id on run.v1, codegen both ways
- [x] GB2 Pinned assets: 4 Qwen2.5 GGUFs (sha256 from HF API) + llama.cpp b9616 builds (sha256 from GitHub digests)
- [x] GB3 CLI: classify → assets → managed llama-server → fixed program → System Score → upload; `aimark` = bench; mock-runtime path for CI; `aimark monitor` probe mode
- [x] GB4 API: programs/benchmarks tables, POST /v1/benchmarks (cell verification + recompute), /v1/leaderboard/systems, /v1/model-fit, /v1/monitor/series
- [x] GB5 Web: home pivot, class leaderboards, /bench detail, /model-fit, /cloud, methodology update
- [x] GB6 CI: e2e-bench (mock, every PR; PASSES locally) + e2e-bench-real (nightly) + cloud-monitor.yaml cron [B keys]
- [x] GB7 README + docs images regenerated for the pivot (cli-bench, systems board, bench detail, model-fit, glossary); benchmark seeder added
- [x] GB8 Verified: mise run check + e2e.sh + e2e-bench.sh pass locally; CI green on 811838e (lint/build/test/e2e-mock/e2e-bench/deploy); real-asset path verified — dispatch run 27454432805 e2e-bench-real success (pinned llama.cpp + 1.5B model downloaded, classified compact, scored, server-verified)
