#!/usr/bin/env bash
# End-to-end test of the ZERO-CHOICE flow: `aimark bench` → class assignment →
# fixed program against the mock runtime → benchmark envelope → class leaderboard.
#
#   1. mock-llm stands in for the managed llama.cpp runtime
#      (AIMARK_BENCH_RUNTIME_URL bypass; AIMARK_BENCH_FAST trims reps)
#   2. `aimark bench` runs with zero choices; CI env ⇒ source=ci
#   3. assertions: a class was assigned; cells + benchmark envelope accepted and
#      server-rescored; ci-sourced benchmark hidden from the default systems
#      leaderboard, visible with include_ci=true
set -euo pipefail

cd "$(dirname "$0")/.."
WORK="$(mktemp -d)"
API_PORT="${E2E_API_PORT:-8794}"
MOCK_PORT="${E2E_MOCK_PORT:-9094}"
export AIMARK_DATA_DIR="$WORK/aimark-data"

PIDS=()
cleanup() {
  for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  rm -rf "$WORK"
}
trap cleanup EXIT
fail() { echo "E2E-BENCH FAIL: $*" >&2; exit 1; }
wait_for() {
  for _ in $(seq 1 50); do curl -fsS "$1" >/dev/null 2>&1 && return 0; sleep 0.2; done
  fail "$2 not ready"
}

echo "==> build CLI + start stack"
(cd apps/cli && go build -o "$WORK/aimark" ./cmd/aimark)
MOCK_PORT=$MOCK_PORT MOCK_TTFT_MS=10 MOCK_ITL_MS=2 bun tools/mock-llm/server.ts &
PIDS+=($!)
PORT=$API_PORT AIMARK_DB_PATH="$WORK/bench.sqlite" bun services/api/entrypoints/bun.ts &
PIDS+=($!)
wait_for "http://localhost:$MOCK_PORT/health" mock-llm
wait_for "http://localhost:$API_PORT/v1/health" api

echo "==> aimark bench (zero choices; CI env should flag source=ci)"
CI=true AIMARK_BENCH_RUNTIME_URL="http://localhost:$MOCK_PORT/v1" AIMARK_BENCH_FAST=1 \
  "$WORK/aimark" bench --json --yes --api "http://localhost:$API_PORT" >"$WORK/bench.json" \
  || { cat "$WORK/bench.json"; fail "bench run failed"; }

BENCH_ID=$(jq -r '.bench_id' "$WORK/bench.json")
CLASS=$(jq -r '.class' "$WORK/bench.json")
[ -n "$BENCH_ID" ] && [ "$BENCH_ID" != "null" ] || fail "no bench_id in output"
[ -n "$CLASS" ] && [ "$CLASS" != "null" ] || fail "no class assigned"
[ "$(jq -r '.source' "$WORK/bench.json")" = "ci" ] || fail "expected source=ci"
echo "    bench $BENCH_ID class=$CLASS"

echo "==> benchmark rescored server-side (composite from the score cells)"
curl -fsS "http://localhost:$API_PORT/v1/benchmarks/$BENCH_ID" >"$WORK/detail.json" \
  || fail "benchmark detail 404"
jq -e '.composite > 0' "$WORK/detail.json" >/dev/null || fail "no server composite"
jq -e '.cells | length > 0' "$WORK/detail.json" >/dev/null || fail "no cells recorded"
[ "$(jq -r '.source' "$WORK/detail.json")" = "ci" ] || fail "API lost ci source"
# The mock runtime emits canned text, so the gauntlet validity cell scores 0 and
# the benchmark is flagged validity_failed — exactly the broken-run signal the
# validity check exists to catch. A real run on a healthy model passes it.
[ "$(jq -r '.flag_reason' "$WORK/detail.json")" = "validity_failed" ] \
  || fail "expected validity_failed against the mock runtime"

echo "==> default systems leaderboard hides it (ci-sourced AND flagged)"
BOARD="http://localhost:$API_PORT/v1/leaderboard/systems?program=bench&version=1&class=$CLASS"
[ "$(curl -fsS "$BOARD" | jq "[.rows[] | select(.bench_id == \"$BENCH_ID\")] | length")" = "0" ] \
  || fail "benchmark leaked into the default systems board"

echo "==> include_ci=true alone still hides it (still flagged)"
[ "$(curl -fsS "$BOARD&include_ci=true" | jq "[.rows[] | select(.bench_id == \"$BENCH_ID\")] | length")" = "0" ] \
  || fail "flagged benchmark leaked with include_ci alone"

echo "==> include_ci + include_flagged reveals it, tagged source=ci"
ROW=$(curl -fsS "$BOARD&include_ci=true&include_flagged=true" | jq "[.rows[] | select(.bench_id == \"$BENCH_ID\")] | first")
[ "$(echo "$ROW" | jq -r '.source')" = "ci" ] \
  || fail "benchmark missing from include_ci+include_flagged board"

echo "E2E-BENCH PASS"
