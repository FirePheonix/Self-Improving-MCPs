#!/bin/sh
# Full cross-domain test of the product path (improver on dev, gate on sealed, controls).
# Run from the self-improving-mcp/ folder:
#   sh scripts/bench/run-all.sh [seed]
# Environment:
#   DOCS=full|none    documentation the simulated servers ship (default full). "none" = names and schemas
#                     only, plus a restore-the-real-docs positive control.
#   DOMAINS="map pay" run a subset (default: all five).
#   PROVIDER=groq|openai  which OpenAI-compatible API to use. Default groq: the primary testbed is a small open agent
#                     (openai/gpt-oss-20b) with a larger improver (openai/gpt-oss-120b), because strong agents leave too
#                     few mistakes to measure (docs/EVALUATION.md). groq reads GROQ_API_KEY or GROQ_DEV_API_KEY from .env.
#                     openai uses OPENAI_API_KEY and gpt-4.1-mini (the earlier runs).
#   MODEL / PROPOSER_MODEL  the agent under test, and the improver + retention judge.
#   CONCURRENCY, RETRIES, LIMIT, ROUNDS  parallel calls, SDK retries, max tasks per domain, improver rounds.
#   SKIP_CONTROLS=1   skip the harmful and empty controls (restore still runs) to save calls.
#   BASELINE_ONLY=1   measure the sealed baseline and headroom only (a cheap probe before a full run).
#   OUT=<dir>         results folder (default: a new data/bench-runs/<seed>-<docs>-<timestamp>).
# Results: $OUT/<domain>.log (human lines + one BENCH_RESULT json line each).
set -e
SEED=${1:-2026}
DOCS=${DOCS:-full}
DOMAINS=${DOMAINS:-browser map pay search file_system}
go build -o bin/proof-fs.exe ./cmd/proof-fs
[ -f .env ] && set -a && . ./.env && set +a
case ${PROVIDER:-groq} in
  groq)
    # the key goes to this process only, as the OpenAI-compatible client expects it
    OPENAI_API_KEY=${GROQ_API_KEY:-$GROQ_DEV_API_KEY}
    [ -n "$OPENAI_API_KEY" ] || { echo "set GROQ_API_KEY or GROQ_DEV_API_KEY in .env"; exit 2; }
    export OPENAI_API_KEY OPENAI_BASE_URL=https://api.groq.com/openai/v1
    MODEL=${MODEL:-openai/gpt-oss-20b}; PROPOSER_MODEL=${PROPOSER_MODEL:-openai/gpt-oss-120b}
    CONCURRENCY=${CONCURRENCY:-2}; RETRIES=${RETRIES:-10} ;;
  openai) MODEL=${MODEL:-gpt-4.1-mini} ;;
  *) echo "unknown PROVIDER $PROVIDER"; exit 2 ;;
esac
[ -n "$RETRIES" ] && export OPENAI_MAX_RETRIES=$RETRIES
D=../evaluation/MCPToolBenchPP/data
OUT=${OUT:-data/bench-runs/$SEED-$DOCS-$(date +%Y%m%d-%H%M%S)}
mkdir -p "$OUT"
echo "results: $OUT  (agent $MODEL, improver ${PROPOSER_MODEL:-$MODEL})"
for name in $DOMAINS; do
  case $name in
    browser) spec=browser/browser_0724_single_v3.json ;;
    map) spec=map/map_0717_single_multi_lang_500.json ;;
    pay) spec=pay/pay_0723_single.json ;;
    search) spec=search/search_0725_single_v2.json ;;
    file_system) spec=file_system/filesystem_0723_single.json ;;
    *) echo "unknown domain $name"; exit 2 ;;
  esac
  extra=""; [ "$name" = file_system ] && [ -z "$BASELINE_ONLY" ] && extra="-execute"
  [ -n "$BASELINE_ONLY" ] && extra="$extra -baseline-only"
  [ -n "$SKIP_CONTROLS" ] && extra="$extra -skip-controls"
  echo "== $name (seed $SEED, docs $DOCS)"
  ./bin/proof-fs.exe -mode gate -docs "$DOCS" -dataset "$D/$spec" -seed "$SEED" -model "$MODEL" \
    -proposer-model "${PROPOSER_MODEL:-$MODEL}" -concurrency "${CONCURRENCY:-8}" -limit "${LIMIT:-0}" -rounds "${ROUNDS:-3}" \
    $extra > "$OUT/$name.log" 2>&1 || {
    echo "   stopped: $(tail -1 $OUT/$name.log)"; exit 1; }
  grep -v '^BENCH_RESULT' "$OUT/$name.log" |
    grep -E 'baseline on sealed|headroom|dev mistake|improve round|improver|candidate|harmful|empty|restore|executed' | sed 's/^/   /'
done
