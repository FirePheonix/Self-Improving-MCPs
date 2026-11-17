#!/bin/sh
# Hot-reload smoke with a real OpenTelemetry collector (Docker), then checks that the collector received
# tool spans carrying the context the monitoring story depends on:
#   simcp.overlay.version  "none" before promote, the promoted version id after (hot reload reaches spans)
#   simcp.tool.requested   on the routed call (what the agent asked for, before routing)
#   simcp.session.id       on tool spans
# Run from self-improving-mcp/ with Docker running:  sh scripts/smoke/otel-smoke.sh [workspace]
set -e
WS=${1:-../evaluation/workspace/test_project_root}
WS=$(cd "$WS" && pwd -W 2>/dev/null || pwd)
TMP=$(mktemp -d)
NAME=simcp-otel-smoke-$$
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true; rm -rf "$TMP"' EXIT

go build -o "$TMP/proxy.exe" ./cmd/proxy
go build -o "$TMP/evolve.exe" ./cmd/evolve
CFG=$(pwd -W 2>/dev/null || pwd)/scripts/smoke/otel-collector.yaml
MSYS_NO_PATHCONV=1 docker run -d --name "$NAME" -p 4318:4318 -v "$CFG:/etc/otelcol/config.yaml" \
  otel/opentelemetry-collector:0.115.0 --config /etc/otelcol/config.yaml >/dev/null
i=0; until curl -s -o /dev/null http://127.0.0.1:4318/ 2>/dev/null; do i=$((i+1)); [ $i -gt 60 ] && { echo "collector did not start"; exit 1; }; sleep 1; done

OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318 node scripts/smoke/hotreload-smoke.mjs \
  "$TMP/proxy.exe" "$TMP/evolve.exe" "$TMP/scratch" "$WS" | tee "$TMP/smoke.txt"
VERSION=$(sed -n 's/^active version: //p' "$TMP/smoke.txt" | tr -d '\r')
sleep 6 # the batch span processor exports on shutdown or every 5 s
docker logs "$NAME" > "$TMP/collector.txt" 2>&1

fail=0
chk() { if grep -q "$2" "$TMP/collector.txt"; then echo "PASS  $1"; else echo "FAIL  $1 (no '$2' in collector output)"; fail=1; fi; }
chk "collector received tool spans"                     'execute_tool'
chk "baseline spans carry overlay version none"         'simcp.overlay.version: Str(none)'
chk "spans after promote carry the promoted version"    "simcp.overlay.version: Str($VERSION)"
chk "routed call records what the agent requested"      'simcp.tool.requested: Str(read_file)'
chk "tool spans carry the session id"                   'simcp.session.id: Str('
[ $fail -eq 0 ] && echo "OTEL ALL PASS" || { echo "collector log kept at: $TMP/collector.txt"; trap - EXIT; docker rm -f "$NAME" >/dev/null; exit 1; }
