# Self-Improving MCP

A transparent MCP proxy that sits between **Cursor** and a real upstream MCP server.
It records every tool call, then an offline loop proposes and validates improvements.

## How it works

1. **Hot path** — Cursor talks to our proxy. The proxy forwards to your real MCP.
2. **Cold path** — Offline tools read captured traffic, find problems, try fixes in a sandbox, and promote only winners.

No private chain-of-thought required. We only look at tool names, arguments, results, latency, and errors.

## Folders

| Folder | Job |
|--------|-----|
| `mcp/` | Proxy Cursor talks to. Forwards to the real MCP. |
| `tracer/` | Remembers every tool call (trajectories). |
| `observability/` | Ships spans and metrics via OpenTelemetry. |
| `agent/` | Offline brain: find problems, draft fixes (OpenAI). |
| `improvement-sandbox/` | Try a fix safely. Keep it or throw it away. |
| `store/` | Shared SQLite notebook. |
| `config/` | Human-editable yaml examples. |
| `cmd/proxy/` | Binary Cursor launches. |
| `cmd/evolve/` | Offline evolution CLI. |
| `data/` | Runtime database (gitignored). |

## Data directory

Runtime SQLite lives under `data/` (gitignored). Default path: `data/trajectories.db`.

Override with `DATA_DIR` or `data_dir` in config. The proxy and `evolve` CLI should share the same directory so offline analysis sees live traffic.

## Cursor wiring

1. Copy `config/proxy.example.yaml` → `config/proxy.yaml` and set your real upstream `command` / `args` (or `url` later).
2. Build the proxy:

```powershell
go build -o bin/proxy.exe ./cmd/proxy
```

3. Point Cursor at the proxy instead of the upstream. Example `mcp.json` entry (adjust the absolute path):

```json
{
  "mcpServers": {
    "self-improving": {
      "command": "E:/self-improving-mcp/self-improving-mcp/bin/proxy.exe",
      "args": ["-config", "E:/self-improving-mcp/self-improving-mcp/config/proxy.yaml"],
      "env": {
        "DATA_DIR": "E:/self-improving-mcp/self-improving-mcp/data"
      }
    }
  }
}
```

Cursor launches the proxy; the proxy launches (or connects to) your real MCP.

## Dogfood checklist (manual)

Do this once after wiring Cursor:

1. Upstream alone works in Cursor (sanity check before the proxy).
2. Switch Cursor to the proxy entry above; reload MCP servers.
3. Confirm the tool list matches upstream (same names).
4. Call one simple tool through the proxy; confirm the result looks normal.
5. Check proxy stderr/logs for `proxy ready; serving stdio`, `trajectory session: …`, and `active version: none`.
6. Call a tool, then inspect `data/trajectories.db` (see `tracer/README.md` for SQL).

If list or call fails, fix `config/proxy.yaml` upstream `command`/`args` first.

## OpenTelemetry

Optional. Set `OTEL_EXPORTER_OTLP_ENDPOINT` (see `.env.example`) to export spans/metrics.
With no endpoint, the proxy uses no-op providers (zero collector required).

Payloads are **not** attached to spans by default.

## Status

Proxy + trajectory tracing + OTel instrumentation work. Offline agent loop next.


