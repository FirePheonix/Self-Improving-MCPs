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

## Status

Scaffold only. Proxy and evolution loop land next.
