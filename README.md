<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
    <img alt="self-improving mcp" src="docs/assets/logo-light.svg" width="560">
  </picture>
</p>

<h3 align="center">MCP servers that learn from how agents use them, and prove it before they change</h3>

<p align="center">
  <a href="go.mod"><img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white"></a>
  <a href="docs/ARCHITECTURE.md"><img alt="MCP proxy and library" src="https://img.shields.io/badge/MCP-proxy%20%2B%20library-6250d6"></a>
  <a href="docs/EVALUATION.md"><img alt="Status: research preview" src="https://img.shields.io/badge/status-research%20preview-eda100"></a>
  <a href="paper/"><img alt="Whitepaper" src="https://img.shields.io/badge/whitepaper-LaTeX-1baf7a"></a>
  <a href="docs/"><img alt="Documentation" src="https://img.shields.io/badge/docs-read-2a78d6"></a>
</p>

An MCP server gives an agent a menu of tools. When the menu is unclear, agents pick the wrong tool, build the wrong arguments,
retry and waste calls, and the server's author rarely finds out. **Self-Improving MCP** sits between agents and your server,
watches what they get wrong, drafts better tool descriptions, and **ships a change only when it is measured to help** on tasks
the drafting step never saw. A language model may *propose* a change; only evidence and a person can *ship* it.

It is a Go proxy you put in front of any MCP server (stdio or HTTP), an offline `evolve` command that runs the improvement loop,
and a small library (`simcp`) for building servers with the loop built in.

## What does it offer?

- **[Watch without touching](docs/ARCHITECTURE.md#the-hot-path):** a transparent proxy records every tool call (redacted) and
  emits OpenTelemetry tagged with the active version. Your server is not modified and no LLM sits on the call path.
- **[Learn from real mistakes](docs/ARCHITECTURE.md#2-the-improver):** the improver reads the requests the agent got wrong, rewrites
  the tool descriptions involved, measures each rewrite and drops the parts that do harm.
- **[Prove it before it ships](docs/SELECTION_GATE.md):** the selection gate asks the same agent to choose tools on sealed,
  labeled tasks, before and after. A change passes only if it fixes enough tasks, breaks none, and the calls it produces can run.
- **[Change safely in production](docs/ARCHITECTURE.md#promote-hot-reload-rollback):** changes are versioned overlays. Promote
  hot-reloads the running proxy, a bad overlay is refused while the last good one keeps serving, and rollback is one command.
- **[Hand the fix back to the source](docs/UPSTREAM_PATCH.md):** a gate-approved description becomes an edit to the server's own
  code and docs, on a local branch for its maintainer.
- **[Build with it](docs/USAGE.md#build-a-server-with-simcp):** write handlers in Go with `simcp` and get tracing, overlays and
  the loop without running a separate proxy.

## How it works

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/how-it-works-dark.svg">
    <img alt="Live: agent, proxy and your MCP server, with every call recorded. Offline: observe, improve, gate, review, promote; the proxy then hot-reloads." src="docs/assets/how-it-works-light.svg" width="100%">
  </picture>
</p>

A change is an **overlay**: a small versioned JSON document the proxy applies on top of your server. It can rewrite or hide a
tool's description, route one tool's calls to another, cache results, or compose tools. It never edits the server itself.
The live path stays fast and deterministic; everything that involves a model runs offline. [Architecture →](docs/ARCHITECTURE.md)

## How a change is judged

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/gate-dark.svg">
    <img alt="A golden set is split into DEV, where the improver learns, and SEALED, which only the gate sees. Same agent, today's tools vs the candidate, a pick for every sealed task. Verdicts: pass, fail, inconclusive." src="docs/assets/gate-light.svg" width="100%">
  </picture>
</p>

Hosted models are not deterministic even at temperature 0, so disputed tasks are re-asked and a task counts only when the result
is decisive. A route that renames a call into a tool its arguments do not fit is rejected before any model call. No labels yet?
`evolve golden draft` writes candidate tasks from your tool docs for a person to approve. [The gate in detail →](docs/SELECTION_GATE.md)

## Does it work?

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/results-dark.svg">
    <img alt="Map server without descriptions: improver's descriptions 107 to 124 sealed tasks, pass; real descriptions 106 to 119 with 2 broken, fail; harmful control fail; empty control inconclusive." src="docs/assets/results-light.svg" width="100%">
  </picture>
</p>

Tested on five domains of [MCPToolBench++](https://huggingface.co/datasets/MCPToolBench/MCPToolBenchPP) (Browser, File System,
Map, Pay, Search) with `gpt-4.1-mini` as the agent, counting a pick correct only when the tool is right (or an equivalent) **and**
its arguments are valid:

- **The gate says no when it should:** a deliberately harmful change failed in every domain where one could be built, with exactly
  the predicted number of broken tasks, and an empty change has shown no false difference since the re-voting rule went in.
- **The first real improvement:** for a server shipped without descriptions, the loop learned descriptions that fixed 17 sealed
  tasks and broke none, better than the server's own descriptions (15 fixed, 2 broken).
- **Where servers already document their tools well, it found nothing to ship,** and the gate correctly refused small gains that
  did not hold up. An earlier headline gain of +8.6 points was withdrawn when we found it came from a route that could not
  actually run.

One model and one seed for the positive result so far. [How we tested, and every caveat →](docs/EVALUATION.md)

## Quick start

Requires Go 1.26+, Node (for `npx` servers) and an `OPENAI_API_KEY` for the offline steps (copy `.env.example` to `.env`).

```sh
go build -o bin/proxy ./cmd/proxy && go build -o bin/evolve ./cmd/evolve
cp config/proxy.example.yaml config/proxy.yaml       # point upstream at your real MCP server
cp config/evolve.example.yaml config/evolve.yaml
```

Point your MCP client at `bin/proxy -config config/proxy.yaml` instead of your server, use it as usual, then run the loop:

```sh
bin/evolve golden draft -proxy-config config/proxy.yaml -out data/golden/draft.json   # review, approve, then:
bin/evolve run                        # observe, improve, gate; a pass becomes a branch to review
bin/evolve promote -version <ID>      # the running proxy picks it up, no restart
bin/evolve rollback -version none     # back to the plain server at any time
```

Configuration, every command, Docker and the library: [Usage →](docs/USAGE.md)

## Documentation

| | |
|---|---|
| [Architecture](docs/ARCHITECTURE.md) | Components, overlays, the loop step by step, safety guarantees, monitoring |
| [Usage](docs/USAGE.md) | Install, configure, commands, `simcp`, Docker |
| [Selection gate](docs/SELECTION_GATE.md) | What counts as correct, statistics, verdicts, golden sets |
| [Evaluation](docs/EVALUATION.md) | How we tested, the benchmark design, results, threats to validity, how to reproduce |
| [Upstream patch](docs/UPSTREAM_PATCH.md) | Carrying an approved change into the server's own source |
| [Test log](docs/TESTING.md) | The dated lab notebook: every run, bug and correction |
| [Whitepaper](paper/) | The method and results as a paper (LaTeX) |

## Status

A research preview, built to be a foundation: stable overlay schema, versioned changes, OpenTelemetry, a gate you can trust to
say no. Not yet shown: improvement with a second model, on real (non-benchmark) traffic, or on servers that already document
their tools well. Nothing here pushes code or opens pull requests on its own.
