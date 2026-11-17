# Usage

## Install

Requires Go 1.26+, Node 18+ (only for `npx`-launched servers) and, for the offline steps, an `OPENAI_API_KEY`
(copy `.env.example` to `.env`; it is git-ignored). The proxy itself never calls a model.

```sh
go build -o bin/proxy  ./cmd/proxy
go build -o bin/evolve ./cmd/evolve
cp config/proxy.example.yaml  config/proxy.yaml
cp config/evolve.example.yaml config/evolve.yaml
```

On Windows the binaries are `bin\proxy.exe` and `bin\evolve.exe`; the commands are otherwise the same.

## Put the proxy in front of your server

Set the upstream in `config/proxy.yaml`, then point your MCP client at the proxy instead of the server. For a client that uses an
`mcp.json`:

```json
{
  "mcpServers": {
    "my-server": {
      "command": "/abs/path/to/bin/proxy",
      "args": ["-config", "/abs/path/to/config/proxy.yaml"],
      "env": { "DATA_DIR": "/abs/path/to/data" }
    }
  }
}
```

The proxy and `evolve` must share one data directory (`DATA_DIR` or `data_dir`; the database is `trajectories.db` inside it).

### Proxy settings (`config/proxy.yaml`)

| Key | Meaning |
|-----|---------|
| `upstream.command` / `args` / `env` | Launch a local stdio MCP server. |
| `upstream.url` | Or connect to an HTTP MCP endpoint. |
| `upstream.dir` | Working directory of the launched server (for servers that resolve relative paths). |
| `data_dir` | Where the SQLite database lives. |
| `redact_payloads` | Strip secrets and truncate stored arguments and results (recommended). |
| `capture_payloads_in_spans` | Attach payloads to OpenTelemetry spans (default off). |
| `status_addr` | e.g. `127.0.0.1:8787`: serves `/health` and `/overlay`. Keep it on loopback unless you add auth. |
| `reload_interval_seconds` | How often to check for a promoted overlay (default 5; negative disables hot reload). |

Environment overrides: `PROXY_CONFIG`, `DATA_DIR`, `OTEL_EXPORTER_OTLP_ENDPOINT`.

## The `evolve` commands

| Command | What it does |
|---------|--------------|
| `evaluate [-skip-llm]` | Run the detectors over recorded traffic. |
| `golden draft` / `golden approve` | Draft labeled tasks from the server's tool docs; a person approves; approved tasks split into DEV and SEALED. |
| `improve [-rounds 3] [-history FILE]` | Learn description changes on DEV; registers the best as a candidate. Prints the mistake patterns it worked on. |
| `propose` | The older findings-only proposer (used when there is no golden set). |
| `sandbox -version ID` | Replay recent traffic against a candidate (checks calls still run; advisory). |
| `gate -version ID [-branch -repo DIR] [-report FILE] [-accept-info-loss] [-exit-code]` | The authority on quality. `-exit-code`: 0 pass, 3 fail, 4 inconclusive (for CI). |
| `import -file overlay.json` | Register a hand-written or reviewed overlay as a candidate. |
| `promote -version ID` / `discard -version ID` / `rollback -version ID\|none` | Move the active pointer; `none` is the plain server. |
| `upstream-patch -version ID -repo DIR` | Carry approved description changes into the server's own source on a local branch. |
| `versions` | List versions and which one is active. |
| `run [-auto-promote]` | evaluate → improve (or propose) → replay sandbox → gate → branch or notification. `-auto-promote` needs a golden set. |

### A typical cycle

```sh
bin/evolve golden draft   -proxy-config config/proxy.yaml -out data/golden/draft.json -per-tool 6
#   read the draft: fix or delete bad tasks, set "approved": true on the ones you trust
bin/evolve golden approve -draft data/golden/draft.json -out data/golden/mine.json -proxy-config config/proxy.yaml
#   set gate.golden_path: data/golden/mine.json in config/evolve.yaml, use the server for a while, then:
bin/evolve run
bin/evolve gate -version <ID> -branch -repo . -report report.json    # a pass becomes branch improve/overlay-<ID>
bin/evolve promote -version <ID>                                     # the running proxy hot-reloads it
bin/evolve upstream-patch -version <ID> -repo <server-repo> -proxy-config config/proxy.yaml -report report.json
```

### Gate settings (`config/evolve.yaml`, `gate:`)

| Key | Default | Meaning |
|---|---|---|
| `golden_path` | — | The golden set; required for `improve`, `gate` and `-auto-promote`. |
| `model` | `gpt-4.1-mini` | The selector model that stands in for your agent. |
| `min_sealed` | 30 | Fewer sealed tasks is always inconclusive. |
| `alpha` | 0.05 | Sign-test threshold for a pass. |
| `max_broken` | 0 | Broken sealed tasks tolerated. |
| `max_selector_error_rate` | 0.05 | Above this, the result is void. |
| `confirm_votes` | 8 | Extra votes per arm on disputed tasks (negative disables). |
| `retention_votes` | 3 | Judge votes per sentence in the retention check. |
| `accept_info_loss` | false | Let a pass stand even if a description dropped needed information (recorded in `PR.md`). |

Notifications: `notify.webhook_url`, or the `EVOLVE_WEBHOOK_URL` environment variable (it is a secret, so prefer the variable).

## Build a server with `simcp`

```go
app := simcp.New("notes", "1.0.0")
app.Tool(simcp.Tool{Name: "read_note", Description: "Read one note by id.", Handler: readNote})
app.Serve(ctx, config.ProxyConfig{DataDir: "data", StatusAddr: "127.0.0.1:8787"})
```

Handlers are plain Go functions. Calls are traced, the active overlay is applied and hot-reloaded, and `evolve` works against
the same data directory. For `evolve`, launch your binary with `SIMCP_RAW=1` as the upstream command so it sees the real tool
list. A full example is in `examples/notes`.

## Monitoring

- `GET /health`: 200 with an upstream and registered tools, otherwise 503.
- `GET /overlay`: active version, the overlay in force, tool names, last reload error.
- OpenTelemetry: set `OTEL_EXPORTER_OTLP_ENDPOINT`. Tool spans carry `simcp.session.id`, `simcp.overlay.version` and
  `simcp.tool.requested` (when routing renamed the call). `scripts/smoke/otel-collector.yaml` is a ready collector config;
  `sh scripts/smoke/otel-smoke.sh` checks all of this against a real collector in Docker.

## Docker

```sh
docker build -t simcp .
docker run -i --rm -v simcp-data:/data -v "$PWD/config:/config:ro" simcp proxy -config /config/proxy.yaml
docker run --rm -v simcp-data:/data -v "$PWD/config:/config:ro" --env-file .env simcp evolve versions -config /config/evolve.yaml
```

The image runs as a non-root user and includes Node for `npx` servers. `docker-compose.yml` adds an OpenTelemetry collector.
