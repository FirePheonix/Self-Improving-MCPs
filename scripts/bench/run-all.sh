#!/bin/sh
# Full cross-domain test of the product path (improver on dev, gate on sealed, controls).
# Needs OPENAI_API_KEY with credits. Run from the self-improving-mcp/ folder:
#   sh scripts/bench/run-all.sh [seed]
# Environment:
#   DOCS=full|none   documentation the simulated servers ship (default full). "none" = names and schemas
#                    only, plus a restore-the-real-docs positive control that the gate must pass.
#   DOMAINS="map pay" run a subset (default: all five).
#   OUT=<dir>        results folder (default: a new data/bench-runs/<seed>-<docs>-<timestamp>, so earlier
#                    evidence is never overwritten).
# Results: $OUT/<domain>.log (human lines + one BENCH_RESULT json line each).
set -e
SEED=${1:-2026}
DOCS=${DOCS:-full}
DOMAINS=${DOMAINS:-browser map pay search file_system}
go build -o bin/proof-fs.exe ./cmd/proof-fs
[ -f .env ] && set -a && . ./.env && set +a
D=../evaluation/MCPToolBenchPP/data
OUT=${OUT:-data/bench-runs/$SEED-$DOCS-$(date +%Y%m%d-%H%M%S)}
mkdir -p "$OUT"
echo "results: $OUT"
for name in $DOMAINS; do
  case $name in
    browser) spec=browser/browser_0724_single_v3.json ;;
    map) spec=map/map_0717_single_multi_lang_500.json ;;
    pay) spec=pay/pay_0723_single.json ;;
    search) spec=search/search_0725_single_v2.json ;;
    file_system) spec=file_system/filesystem_0723_single.json ;;
    *) echo "unknown domain $name"; exit 2 ;;
  esac
  extra=""; [ "$name" = file_system ] && extra="-execute"
  echo "== $name (seed $SEED, docs $DOCS)"
  ./bin/proof-fs.exe -mode gate -docs "$DOCS" -dataset "$D/$spec" -seed "$SEED" $extra > "$OUT/$name.log" 2>&1 || {
    echo "   stopped: $(tail -1 $OUT/$name.log)"; exit 1; }
  grep -v '^BENCH_RESULT' "$OUT/$name.log" |
    grep -E 'baseline on sealed|headroom|dev mistake|improve round|improver|candidate|harmful|empty|restore|executed' | sed 's/^/   /'
done
