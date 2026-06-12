#!/usr/bin/env bash
# Regenerates the images embedded in README/docs:
#   - CLI terminal captures rendered to SVG via charmbracelet/freeze
#   - website screenshots via Playwright against a locally seeded stack
#
# Output goes to docs/images/. Run by .github/workflows/docs-images.yaml
# (weekly + manual), which auto-commits changes. Locally: scripts/capture-docs-images.sh
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"
OUT="$ROOT/docs/images"
WORK="$(mktemp -d)"
API_PORT="${CAPTURE_API_PORT:-8789}"
MOCK_PORT="${CAPTURE_MOCK_PORT:-9092}"
export AIMARK_DATA_DIR="$WORK/aimark-data"

PIDS=()
cleanup() {
  for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  rm -rf "$WORK"
}
trap cleanup EXIT

wait_for() {
  for _ in $(seq 1 50); do curl -fsS "$1" >/dev/null 2>&1 && return 0; sleep 0.2; done
  echo "not ready: $1" >&2
  exit 1
}

mkdir -p "$OUT"

echo "==> build CLI + start stack"
(cd apps/cli && go build -o "$WORK/aimark" ./cmd/aimark)
MOCK_PORT=$MOCK_PORT MOCK_TTFT_MS=15 MOCK_ITL_MS=3 bun tools/mock-llm/server.ts &
PIDS+=($!)
PORT=$API_PORT AIMARK_DB_PATH="$WORK/capture.sqlite" bun services/api/entrypoints/bun.ts &
PIDS+=($!)
wait_for "http://localhost:$MOCK_PORT/health"
wait_for "http://localhost:$API_PORT/v1/health"

echo "==> seed leaderboard with plausible user runs"
bun tools/seed/seed.ts --api "http://localhost:$API_PORT"

echo "==> CLI captures (freeze → SVG)"
export PATH="$WORK/aimark-bin:$PATH"
mkdir -p "$WORK/aimark-bin" && cp "$WORK/aimark" "$WORK/aimark-bin/aimark"
FREEZE="go run github.com/charmbracelet/freeze@v0.2.2"
FOPTS=(--theme dracula --window --font.size 14 --padding 20)

capture_cli() { # name, command...
  local name="$1"
  shift
  "$@" >"$WORK/$name.txt" 2>&1 || true
  (cd apps/cli && $FREEZE "$WORK/$name.txt" "${FOPTS[@]}" --language text -o "$OUT/$name.svg")
}

capture_cli cli-suites "$WORK/aimark" suites list
capture_cli cli-detect "$WORK/aimark" detect
AIMARK_RUN_OUT="$WORK/runout.txt"
"$WORK/aimark" run sprint-1 --target openai:mock-1 --target-url "http://localhost:$MOCK_PORT/v1" \
  --reps 3 --warmups 1 --source dev >"$AIMARK_RUN_OUT" 2>&1 || true
(cd apps/cli && $FREEZE "$AIMARK_RUN_OUT" "${FOPTS[@]}" --language text -o "$OUT/cli-run.svg")

echo "==> website screenshots (Playwright)"
(cd services/web && bun run build >/dev/null)
bunx astro preview --root services/web --port 4322 &
PIDS+=($!)
wait_for "http://localhost:4322"
CAPTURE_WEB_URL="http://localhost:4322" CAPTURE_API_URL="http://localhost:$API_PORT" \
  bun tools/screenshots/capture.ts --out "$OUT"

echo "==> done"
ls -la "$OUT"
