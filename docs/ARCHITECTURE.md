# Architecture

Self-Improving MCP has two halves that share one SQLite database: a **hot path** that serves agents and records what they
do, and a **cold path** that turns those records into reviewed, measured changes. The rule that shapes everything else:
**no language model is ever on the call path**, and nothing a model writes reaches agents without passing the selection gate.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/how-it-works-dark.svg">
    <img alt="Hot path and cold path" src="assets/how-it-works-light.svg" width="100%">
  </picture>
</p>

## Components

| Package | Role |
|---|---|
| `cmd/proxy` | The proxy binary: an MCP server to the agent, an MCP client to your server. |
| `mcp/` | Upstream connection (stdio or HTTP), tool registration, overlay application, hot reload, `/health` and `/overlay`. |
| `tracer/` | Records calls and sessions with redaction. |
| `observability/` | OpenTelemetry provider and the tool span attributes. |
| `store/` | SQLite: `sessions`, `tool_calls`, `findings`, `versions`, `active_version`, `experiments` (pure Go, `modernc.org/sqlite`). |
| `agent/` | Offline analysis: rule detectors, the improver, the older findings-based proposer, the OpenAI client and selector. |
| `improvement-sandbox/` | The overlay type, validation, route executability, replay sandbox and scorecard. |
| `selection-gate/` | Golden sets, the selection run, argument validation, re-voting, statistics, verdicts, retention check, golden drafting. |
| `publish/` | Side branches with evidence, notifications, webhook, upstream source patching. |
| `simcp/` | The library: build a server whose handlers run behind the same runtime. |
| `cmd/evolve` | The offline CLI that drives the loop. |
| `cmd/proof-fs` | The benchmark harness (evaluation only, not part of the product). |

## Overlays

An overlay is the unit of change: a versioned JSON document the proxy applies on top of the real server. Every version is
stored, exactly one is active (or none, meaning the plain server), and moving between them is a pointer change.

```json
{
  "tool_patches": {
    "read_multiple_files": { "description": "Read several files in one call. Use this whenever a request names two or more files. ..." },
    "read_file":           { "hidden": true }
  },
  "routing":        [ { "from_tool": "read_file", "to_tool": "read_text_file" } ],
  "cache":          [ { "tool_name": "get_file_info", "ttl_seconds": 60 } ],
  "composed_tools": { "inspect_dir": { "description": "...", "steps": [ { "tool": "list_directory" }, { "tool": "directory_tree" } ] } }
}
```

| Field | Effect on the agent |
|---|---|
| `tool_patches.<tool>.description` | What the agent reads when choosing. The main lever. |
| `tool_patches.<tool>.hidden` | The tool is not offered (for deprecated aliases). Refused for unknown tools or for every tool. |
| `tool_patches.<tool>.input_schema` | Replaces the advertised schema. |
| `routing` | A call to `from_tool` is served by `to_tool`, or denied. Only between tools whose required arguments fit (see below). |
| `cache` | Identical calls within the TTL are answered from memory (idempotent tools only). |
| `composed_tools` | A new tool that runs a fixed sequence of upstream calls. |

`mcp.VisibleTools(upstream, overlay)` computes the tool list an agent sees under an overlay. The proxy serves it, and the gate
and the improver show exactly the same list to the selector, so what is measured is what is served. `Overlay.ResolveRoute` is
likewise shared by the proxy and the gate.

**Routes must be executable.** A route renames a call but not its arguments. A route is refused, by the gate before any model
call and by the proxy at load time, when the target requires an argument the source does not take, or a shared argument has a
different type. This was learned the hard way: `read_text_file → read_multiple_files` scored well on tool names, and every
routed call failed (`path` vs `paths`). Optional arguments are not compared, so genuine aliases such as `read_file → read_text_file`
pass.

## The hot path

1. The agent connects to the proxy over stdio. The proxy connects to the upstream (a command it launches, or an HTTP URL),
   lists its tools and registers `VisibleTools(upstream, active overlay)`.
2. On `tools/call`, the proxy resolves routing (rename or deny), checks the cache, forwards the call, and returns the result.
   Composed tools fan out to their steps.
3. Every call is recorded in `tool_calls`: session and sequence number, tool name, redacted arguments and an argument hash,
   result summary, error flag and type, latency. Payloads are redacted and truncated by default (`redact_payloads`). The overlay
   version and, for routed calls, the requested tool are on the spans (next point), not in the table.
4. OpenTelemetry spans (`execute_tool <name>`) carry `simcp.session.id`, `simcp.overlay.version` (`none` for the plain server)
   and, when routing changed the name, `simcp.tool.requested`. The session id is never a metric label (unbounded cardinality).
   Without `OTEL_EXPORTER_OTLP_ENDPOINT` the provider is a no-op.
5. When the client closes stdin, the proxy shuts down cleanly and flushes telemetry. (A hard kill loses spans still in the
   5-second batch; this is verified in `scripts/smoke/otel-smoke.sh`.)

## Promote, hot reload, rollback

`evolve promote -version ID` moves the active pointer in the store; it does not talk to the proxy. The proxy polls the pointer
(`reload_interval_seconds`, default 5) and, on a change:

1. Loads the overlay and validates it **against the live tool list**: routes to tools the server lacks, cache rules and composed
   steps naming unknown tools, routes that cannot run, and hiding unknown or all tools are refused.
2. If valid, swaps the served tool list in place (the SDK sends `notifications/tools/list_changed` to clients), clears the cache,
   and new calls see the new routing. Calls already running finish on the snapshot they started with.
3. If invalid, keeps serving the last good overlay and reports the error on `/overlay`.

`evolve rollback -version none` returns to the plain server the same way. A store outage never disturbs serving.

Status endpoints (`status_addr`, bind to loopback unless you add auth): `GET /health` is 200 only with an upstream and
registered tools; `GET /overlay` shows the active version, the overlay in force (including the descriptions agents read), the tool
names and the last reload error.

## The cold path

`evolve run` chains the stages; each also exists as its own command.

### 1. Observe and analyze
Deterministic detectors over recorded traffic: retry storms (at least 3 errors in a window of 5 on one tool), repeated failures
(same tool and arguments failing at least 3 times), redundant consecutive calls, frequent tool sequences, latency outliers
(slower than max(3× mean, p95) once a tool has 5 calls). An optional model labels findings. Findings are stored and sorted so the
same traffic always produces the same input to later stages.

### 2. The improver
When a golden set is configured, candidates come from `agent.ImproveDescriptions`, which learns **only from the DEV split**:

1. Run the agent on DEV under the current overlay. Refuse to continue if more than 5% of calls failed (a broken API key or quota
   would otherwise look like agent mistakes).
2. Group the mistakes by pattern ("should be X, agent did Y", "no tool at all", "right tool, arguments that cannot run").
3. Show a model every tool **as the agent sees it** (name, compact parameter list, description) and the grouped mistakes. It
   returns description patches, or hides a tool marked deprecated.
4. Guards drop unsafe patches: unknown tools, empty descriptions, text that copies eight or more consecutive words of a DEV
   request (it caught real leaks of benchmark file paths), hiding every tool. Routing is never proposed: a route cannot repair
   arguments built for the wrong tool.
5. Measure the patch on DEV. Attribute every changed outcome to the patched tools it involves; drop the patches involved in more
   broken than fixed tasks and measure once more; keep the better version. (Without this, refinement diverged on Map: each round
   rewrote more tools and broke more tasks.)
6. Feed back what was fixed, what is still wrong, what broke, and what was dropped; repeat (3 rounds by default).
7. The best round becomes the candidate only if it beats the DEV baseline by at least 2 tasks.

Choosing the best of several DEV rounds is optimistic by construction; that is why the DEV score never ships anything.

Without a golden set, the older findings-based proposer drafts an overlay from detector findings, and auto-promotion is refused.

### 3. The selection gate
The authority on quality, judged on SEALED tasks the improver never saw. Summary below; full detail in
[SELECTION_GATE.md](SELECTION_GATE.md).

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/gate-dark.svg">
    <img alt="The selection gate" src="assets/gate-light.svg" width="100%">
  </picture>
</p>

- Two arms, same agent: the tools as served today, and the tools under the candidate. A pick is correct only if the tool actually
  called (after routing) is the label or an accepted equivalent, its arguments validate against that tool's schema, and the
  overlay did not deny it.
- Tasks where the arms disagree are re-asked (8 votes per arm). A flip counts only when decisive (at least 75% right on one side,
  at most 25% on the other); anything in between is *unstable* and excluded.
- Verdict: `fail` if any task broke; `pass` if at least 5 fixed, none broken, and a one-sided sign test gives p ≤ 0.05;
  otherwise `inconclusive`. Each decision records `headroom` and says when a pass is out of reach on that sealed set.
- **Retention check** on a pass: for every changed description, a judge (3 votes) checks each sentence of the old text: is it
  needed after choosing the tool, and does the new text still say it? A loss turns `pass` into `inconclusive` unless a person
  accepts it.

### 4. Review and notify
- `pass` with `-branch -repo DIR`: a local branch `improve/overlay-<id>` with `overlay.json`, `evidence.json` and `PR.md`, built
  in a temporary worktree from `HEAD`. Your working tree is never touched, exactly three files are committed, nothing is pushed.
  `PR.md` shows new descriptions verbatim and flags URLs, unusual length and authority wording ("ignore previous", "you must").
- `fail` or `inconclusive`: a timestamped notification file, a terminal line, and optionally a webhook (counts only, no task text;
  errors never contain the URL).

### 5. Promote, and fix the source
A person promotes (or `-auto-promote` does, only after a gate pass with a golden set). `evolve upstream-patch` carries approved
description changes into the server's own source and docs on a local branch, editing only exact matches.
See [UPSTREAM_PATCH.md](UPSTREAM_PATCH.md).

## Safety guarantees

Each has a test.

- **Only `pass` ships.** `fail` and `inconclusive` never promote; `-auto-promote` refuses to run without a golden set.
- **`evolve run` judges only what it just produced**, never a stale candidate from an earlier run.
- **The improver never sees SEALED.** The same query can never be on both sides of the split; each decision records the
  sealed set's SHA-256 so a later edit is detectable.
- **A flaky or broken judge cannot pass anything.** Selector errors count as wrong and, past 5%, void the result; a fatal API
  error (no credits, bad key) stops the run at once instead of being recorded as one failure per task.
- **No LLM on the call path.**
- **A bad overlay cannot take the proxy down.** Reloads are validated against the live tools and the last good overlay keeps serving.
- **Nothing is pushed and no pull request is opened.**
- **Secrets stay secret.** Arguments are redacted before storage; payloads are not on spans by default.

## The library: `simcp`

`simcp` runs your Go handlers behind the same runtime: tracing, overlays, hot reload and the status endpoints, with the server
itself as the "upstream". A handler's error or panic becomes an MCP tool error, not a crash. Run the binary with `SIMCP_RAW=1` as
the upstream command for `evolve`, so the loop sees the real, unpatched tool list. Example: `examples/notes`.
