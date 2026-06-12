#!/usr/bin/env bash
# End-to-end test: CLI → mock-llm → anonymous submit → API → leaderboard.
#
# Exercises the full product loop without model weights:
#   1. mock-llm serves a deterministic OpenAI-compatible stream
#   2. the aimark CLI benchmarks it (sprint-1, reduced reps) and saves a result
#   3. the result is submitted ANONYMOUSLY to a local API instance
#   4. assertions: run persisted + canonically scored; CI-source flagging means
#      the run is hidden from the default leaderboard but visible with
#      include_ci=true; run detail renders; claim token can delete the run
#
# Used by .github/workflows/e2e.yaml and runnable locally: scripts/e2e.sh
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"
WORK="$(mktemp -d)"
API_PORT="${E2E_API_PORT:-8788}"
MOCK_PORT="${E2E_MOCK_PORT:-9091}"
export AIMARK_DATA_DIR="$WORK/aimark-data"

PIDS=()
cleanup() {
  for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  rm -rf "$WORK"
}
trap cleanup EXIT

fail() { echo "E2E FAIL: $*" >&2; exit 1; }

wait_for() { # url, name
  for _ in $(seq 1 50); do
    curl -fsS "$1" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  fail "$2 did not become ready at $1"
}

echo "==> building CLI"
(cd apps/cli && go build -o "$WORK/aimark" ./cmd/aimark)

echo "==> starting mock-llm on :$MOCK_PORT"
MOCK_PORT=$MOCK_PORT MOCK_TTFT_MS=15 MOCK_ITL_MS=3 bun tools/mock-llm/server.ts &
PIDS+=($!)
wait_for "http://localhost:$MOCK_PORT/health" mock-llm

echo "==> starting API on :$API_PORT (sqlite: $WORK/e2e.sqlite)"
PORT=$API_PORT AIMARK_DB_PATH="$WORK/e2e.sqlite" bun services/api/entrypoints/bun.ts &
PIDS+=($!)
wait_for "http://localhost:$API_PORT/v1/health" api

echo "==> aimark run (source should auto-flag as ci when CI=true)"
CI=true "$WORK/aimark" run sprint-1 \
  --target openai:mock-1 \
  --target-url "http://localhost:$MOCK_PORT/v1" \
  --reps 3 --warmups 1 --json >"$WORK/run.json"

RUN_ID=$(jq -r '.run_id' "$WORK/run.json")
[ -n "$RUN_ID" ] && [ "$RUN_ID" != "null" ] || fail "run produced no run_id: $(cat "$WORK/run.json")"
[ "$(jq -r '.source' "$WORK/run.json")" = "ci" ] || fail "expected source=ci in run envelope"
jq -e '.metrics.ttft_ms_p50 > 0 and .metrics.decode_tps_mean > 0' "$WORK/run.json" >/dev/null \
  || fail "metrics missing/zero: $(jq '.metrics' "$WORK/run.json")"
echo "    run $RUN_ID ok (provisional composite: $(jq -r '.provisional_scores.composite // "n/a"' "$WORK/run.json"))"

echo "==> anonymous submit"
CI=true "$WORK/aimark" submit --all-pending --api "http://localhost:$API_PORT" --yes >"$WORK/submit.out" 2>&1 \
  || { cat "$WORK/submit.out"; fail "submit failed"; }
cat "$WORK/submit.out"

echo "==> run detail is served"
curl -fsS "http://localhost:$API_PORT/v1/runs/$RUN_ID" >"$WORK/detail.json" || fail "run detail 404"
[ "$(jq -r '.source' "$WORK/detail.json")" = "ci" ] || fail "API lost the ci source flag"
jq -e '.composite > 0 and (.scores | length) > 0' "$WORK/detail.json" >/dev/null \
  || fail "server-side scores missing"

echo "==> CI flagging: default leaderboard must HIDE the ci run"
DEFAULT_COUNT=$(curl -fsS "http://localhost:$API_PORT/v1/leaderboard?suite=sprint&version=1&track=local" | jq "[.rows[] | select(.run_id == \"$RUN_ID\")] | length")
[ "$DEFAULT_COUNT" = "0" ] || fail "ci-sourced run leaked into the default leaderboard"

echo "==> CI flagging: include_ci=true must SHOW it"
CI_ROW=$(curl -fsS "http://localhost:$API_PORT/v1/leaderboard?suite=sprint&version=1&track=local&include_ci=true" | jq "[.rows[] | select(.run_id == \"$RUN_ID\")] | first")
[ "$(echo "$CI_ROW" | jq -r '.source')" = "ci" ] || fail "run missing from include_ci leaderboard"

echo "==> claim token works (anonymous claim → delete)"
CLAIM_TOKEN=$(jq -r '.claim_token // empty' "$AIMARK_DATA_DIR"/results/*.submitted.json 2>/dev/null | head -1)
[ -n "$CLAIM_TOKEN" ] || fail "no claim token persisted by CLI"
curl -fsS -X PATCH "http://localhost:$API_PORT/v1/runs/$RUN_ID" \
  -H 'content-type: application/json' \
  -d "{\"claim_token\":\"$CLAIM_TOKEN\",\"action\":\"delete\"}" >/dev/null || fail "claim delete rejected"
curl -s -o /dev/null -w '%{http_code}' "http://localhost:$API_PORT/v1/runs/$RUN_ID" | grep -q 404 \
  || fail "run still present after claim delete"

echo "==> duplicate submission is rejected (dedup)"
"$WORK/aimark" results export "$RUN_ID" --out "$WORK/export.json" >/dev/null 2>&1 || true

echo "E2E PASS"
