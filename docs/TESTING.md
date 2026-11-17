# End-to-end test report

Date: 2026-11-13 · Platform: Windows 11, Go 1.26.1, Node 22.12, Docker Desktop 29.6.2 · Upstream: `@modelcontextprotocol/server-filesystem`

## Summary

| Area | Result | How verified |
|------|--------|--------------|
| Go unit tests | **PASS** (all packages) | `go test ./...` |
| Hot path: proxy ↔ MCP client over stdio | **PASS** | `scripts/smoke/mcp-smoke.mjs` (5/5 checks) |
| Trajectory recording (SQLite) | **PASS** | `tool_calls` rows written for each real call |
| OpenTelemetry export | **PASS after fix** (was broken) | local OTel collector in Docker receives spans |
| Offline detectors (`evolve evaluate -skip-llm`) | **PASS** | benign traffic → 0 findings; injected retry storm → 11 findings |
| Sandbox gate (`evolve sandbox`) | **PASS** (with a caveat) | candidate replayed against real upstream |
| Promote → restart → overlay served | **PASS** | restarted proxy returns the patched tool description |
| Rollback to an existing version | **PASS** | proxy returns the previous description |
| Rollback to "no overlay" (baseline) | **PASS** (fixed) | `evolve rollback -version none`; verified on a running proxy |
| `evolve propose` (LLM drafts overlay) | **NOT RUN** | OpenAI account has no credits (see below) |
| MCPToolBench++ File System proof (Pass@1) | **NOT VALID** | every OpenAI call failed; see below |
| Cursor dogfood | **NOT RUN** | manual step, per README |

## Bug found and fixed

**OTel resource schema conflict.** With `OTEL_EXPORTER_OTLP_ENDPOINT` set, the proxy refused to start:

```
proxy: otel setup: otel resource: conflicting Schema URL: https://opentelemetry.io/schemas/1.43.0 and https://opentelemetry.io/schemas/1.37.0
```

Cause: `resource.Default()` (SDK v1.47) carries schema 1.43.0, while `observability/provider.go` pinned semconv v1.37.0 with `resource.NewWithAttributes`. Fix: use `resource.NewSchemaless` for the app's own attributes. Regression test added: `observability/provider_test.go` (fails on the old code, passes on the fix).

## Evidence

### 1. Unit tests
`go test ./...` passes for `agent`, `config`, `improvement-sandbox`, `mcp`, `store`, `tracer`, and the new `observability` regression test.

### 2. Hot path (`scripts/smoke/mcp-smoke.mjs`)
- `initialize` returns `self-improving-mcp` serverInfo.
- `tools/list` exposes 14 filesystem tools.
- `list_directory` and `directory_tree` return real content.
- Unknown tool `no_such_tool` returns JSON-RPC `-32602 unknown tool`.

Note: unknown-tool calls are rejected before tracing, so they are **not** recorded in `tool_calls`. Decide whether failed-lookup calls should be traced.

### 3. Trajectories and detectors
After several sessions, `tool_calls` held the expected rows with `is_error`, `latency_ms`, and result summaries.

`evolve evaluate -skip-llm`:
- 12 benign calls → `rule findings: 0`.
- After 5 failing `read_text_file` calls on a missing file plus 4 repeated `list_directory` calls → 11 findings: `redundant` ×7, `retry_storm` (error), `repeated_failure` (error), `frequent_sequence` ×2.

### 4. OpenTelemetry (Docker)
Collector: `otel/opentelemetry-collector:latest` (v0.165.0) with an OTLP HTTP receiver on `:4318` and a `debug` exporter (`scripts/smoke/otel-collector.yaml`). After the fix, the collector received `execute_tool list_directory` and `execute_tool directory_tree` spans and resource metrics.

Run it:
```powershell
docker run -d --name otel-test -p 4318:4318 -v "${PWD}\scripts\smoke\otel-collector.yaml:/etc/otelcol/config.yaml" otel/opentelemetry-collector:latest --config /etc/otelcol/config.yaml
$env:OTEL_EXPORTER_OTLP_ENDPOINT = "http://localhost:4318"
node scripts/smoke/mcp-smoke.mjs bin\proxy.exe <proxy-config.yaml> <workspace>
docker logs otel-test
```
(In Git Bash, prefix the `docker run` line with `MSYS_NO_PATHCONV=1`, or the `/etc` path gets rewritten.)

Overlay version is **not yet** attached to spans; that is the planned monitoring slice.

### 5. Sandbox gate and promote lifecycle
- Seeded a candidate overlay (description patch on `list_directory`, no routing change).
- `evolve sandbox`: baseline and candidate both 21 calls, 5 errors each, p95 14 ms → 10 ms, `promote: true`.
- `evolve promote` → restarted proxy → log shows `active version: <id>` and the tool description was the patched text.
- `evolve rollback -version <previous id>` → restarted proxy → original description returned.

**Caveat:** the "efficiency win" was a 4 ms p95 difference on 21 calls, which is noise. A description-only patch cannot change routing, so the efficiency check is too weak for small samples. Consider a minimum sample size or a minimum relative improvement before `efficiency_win` counts.

### 6. Benchmark proof (File System, 30 tasks) — invalid run
`go run ./cmd/proof-fs -limit 30` ran end to end, but every tool-selection call failed, so all Pass@1 values are 0.000 and the numbers mean nothing.

Two causes:
1. **OpenAI account has no credits.** `gpt-4.1-mini` and `gpt-4o-mini` both return `429 — You have no credits remaining`. The API key itself is valid (`/v1/models` returns 200).
2. **`gpt-4.1-nano` is deprecated.** It returns `404 model_not_found`. It is the default label model in `config/evolve.example.yaml` and in `cmd/proof-fs`. Replace it with a current model (e.g. `gpt-5.4-nano` or `gpt-4.1-mini` once funded).

Also: `propose` failed with 429 and the harness continued with an empty overlay, logging only one line. Per-task errors are counted but **not logged**, which made this failure look like a quiet 0.000. Log the first error per split.

## Known gaps / follow-ups

1. Add OpenAI credits; replace the deprecated `gpt-4.1-nano` default.
2. Rerun the proof: `go run ./cmd/proof-fs -limit 30`, then the full 241 tasks. Compare with the documented 0.877 → 0.986.
3. Add a baseline/"none" rollback target to the CLI (`rollback -version none`, or `deactivate`).
4. Make the sandbox efficiency check statistically meaningful for small samples.
5. Log the first error per evaluation split instead of silently counting it.
6. Trace unknown-tool calls, or document why they are excluded.
7. Attach overlay version to OTel spans (monitoring slice).
8. Cursor dogfood on your machine (manual, per README).

## Re-running the automated checks

```powershell
cd self-improving-mcp
go test ./...
go build -o bin\proxy.exe ./cmd/proxy
node scripts/smoke/mcp-smoke.mjs bin\proxy.exe <smoke-proxy.yaml> <abs-path>\evaluation\workspace\test_project_root
```

`<smoke-proxy.yaml>` must set the upstream to the same workspace, because the filesystem server only allows paths under its arguments. Example (use absolute paths; `data_dir` should be a scratch folder, not `data/`):

```yaml
data_dir: <abs-path>/scratch-smoke-data
upstream:
  command: npx
  args: ["-y", "@modelcontextprotocol/server-filesystem", "<abs-path>/evaluation/workspace/test_project_root"]
  env: {}
redact_payloads: true
capture_payloads_in_spans: false
```

---

## Addendum: File System proof with Claude subagents (replaces OpenAI)

OpenAI had no credits, so the benchmark selector and overlay proposer were **Claude Haiku 5.5 subagents**,
wired in through a local cache stub. See `scripts/subagent-oracle/README.md` for the method.

### Results (full 241-task File System set, seed 42, 70/30 split)

| Stage | n | Pass@1 | Notes |
|-------|---|--------|-------|
| Baseline holdout | 73 | **0.986** | 72/73 correct, 0 errors |
| Observe (train) | 126 | **1.000** | 126/126 correct |
| Rule findings | — | 46 | 22 redundant, 24 frequent-sequence; **0 tool-confusion** |
| Proposed overlay | — | empty | proposer judged no routing/description evidence |
| Sandbox gate | — | skipped | nothing to gate (empty candidate) |
| Evolved holdout | 73 | **0.986** | identical to baseline, as expected for an empty overlay |

### What this does and does not show

- **The pipeline runs end to end** on the full dataset: observe → evaluate → propose → (skip gate) → evolved holdout,
  with real selection, real upstream tool execution, and real trajectory storage.
- **It does not test the evolve loop.** The documented 0.877 → 0.986 gain came from a weaker selector that made
  wrong picks (`read_text_file` vs `read_multiple_files`) that description patches could fix. Haiku makes almost no
  mistakes on this set, so there were zero confusion findings and nothing for the loop to improve. This is a
  **ceiling effect**, not evidence that the loop works or fails.
- To exercise the loop, the selector needs to make mistakes. Options: run a description-blind selector (tool names
  only), or use a smaller model. Not yet done.

### Bugs found and fixed during this run

1. **Nondeterministic findings order.** `DetectRepeatedFailures` and `DetectFrequentSequences` iterated Go maps, so the
   same traffic produced findings in different orders. That changed the proposer's input and broke caching/reproducibility.
   Fixed with explicit sorting (`agent/detect_failures.go`, `agent/detect_sequences.go`).
2. **Empty overlay reported as failure.** When the proposer returns an empty overlay, the harness logs
   `propose failed (continuing with empty overlay): overlay is empty`. That is a "no change" outcome and should be logged
   as such, not as an error.
3. (Earlier) OTel resource schema conflict — see above.

### Remaining gaps

- Run the selector-with-mistakes ablation (above) so the evolve loop has something to fix.
- Relabel the empty-overlay outcome from "propose failed" to "no change proposed".
- Rerun with a funded OpenAI key to compare directly against the documented numbers.

### Addendum 2: simulated weak selector (skimming subagents) — did not discriminate

To give the selector room to make mistakes, the Haiku subagents were told to decide from each tool's **name and
first description sentence only** (separate cache: `data/oracle-skim/`).

| Stage | n | Pass@1 |
|-------|---|--------|
| Baseline holdout | 73 | **1.000** |
| Observe (train) | 126 | **1.000** |
| Rule findings | — | 47 (0 tool-confusion) |

Result: skimming did not induce any errors on this dataset, so there were no confusions for the loop to act on.
This is the same ceiling effect as the full-description run, with a stronger signal: a Claude-class selector solves
these tasks from tool names alone. **The benchmark as configured cannot test the evolve loop with a Claude selector.**
Next options: a harder selection set, a selector with genuine error modes, or a different benchmark.

---

## Live evidence: File System proof against OpenAI gpt-4.1-mini (real API, no simulation)

> **SUPERSEDED (2026-11-15).** The gains in this section were scored by tool *name* only, and all of them came from one route,
> `read_text_file` to `read_multiple_files`, which cannot execute (a real routed call fails: `expected array, received undefined
> at paths`). Re-tested with the same fix as description-only patches, 0 of 73 sealed tasks were fixed. See "Final acceptance run"
> at the end of this file. The numbers below are kept as a record of what the name-only metric reported, not as evidence of improvement.

Final numbers from `data/live/rerun/` (fixed proposer, all four seeds rerun on identical code).
Protocol: full 241 File System tasks, 70/30 train/holdout, sandbox = 25% of train, model `gpt-4.1-mini`.

| Seed | Baseline holdout | Evolved holdout | Δ | Tasks fixed / broken | Sign test p | Sandbox gate |
|------|------------------|-----------------|---|----------------------|-------------|--------------|
| 42 | 0.890 | 0.986 | +0.096 | 7 / 0 | 0.016 | passed (0.952 vs 0.857) |
| 7 | 0.904 | 1.000 | +0.096 | 7 / 0 | 0.016 | passed (0.976 vs 0.929) |
| 13 | 0.918 | 0.959 | +0.041 | 4 / 1 | 0.375 | passed (0.976 vs 0.857) |
| 99 | 0.863 | 0.973 | +0.110 | 8 / 0 | 0.008 | passed (1.000 vs 0.952) |
| **Pooled (n=292 paired)** | 0.894 | 0.980 | **+0.086** (95% CI +0.055 to +0.120) | 26 / 1 | — | 4/4 passed |

Every seed's overlay was the same single route: `read_text_file → read_multiple_files`, plus description and cache
patches. The fixed tasks are that confusion. This matches the documented mechanism; the documented headline
(0.877 → 0.986, single seed) is in the same range.

### How to read this

- **Supported:** on this benchmark, with a real model, gate-approved overlays from observed traffic raised holdout
  Pass@1 in all four seeds, with the pooled gain's confidence interval above zero and almost no regressions.
- **Not supported:** seed 13 alone is not significant. Four seeds is a small sample, and all of them use one dataset
  and one domain (File System).
- **Holdout integrity:** the holdout was not used to pick the overlay. The overlay came from train-split observations
  (and train labels for confusion detection, which is how the benchmark protocol defines the signal), gated on the
  separate sandbox slice, then scored on holdout.
- **Superseded run:** the first seed-99 run produced malformed proposer JSON and silently skipped the gate. That is the
  bug fixed by JSON mode (see "Bugs found"). Its result is excluded; the rerun above replaces it.
- **Not done:** an ungated-candidate control (documented earlier as a 0.877 → 0.712 regression) and an
  unrelated-patch control. Both would strengthen the attribution claim.

### Bug fixed during the live run

4. **Malformed proposer JSON silently voided the gate.** Seed 99 returned overlay JSON with a trailing `}`. The parser
   rejected it, the harness fell back to an empty overlay, and no candidate was tested. The proposal call now uses
   `response_format=json_object` (`agent/openai.go` `CompleteJSON`, `agent/propose.go`). The parser stays strict on purpose.

---

## Step 1 and 2 build: span context and the selection gate

### Verified
- **Span context.** Tool spans now carry `simcp.session.id`, `simcp.overlay.version` (`none` for baseline) and, when routing
  rewrote the name, `simcp.tool.requested`. Session id is on spans only (unbounded cardinality, never a metric label).
  Unit-tested with an in-memory span recorder (`observability/tool_span_test.go`).
- **Selection gate** (`selection-gate/`, `docs/SELECTION_GATE.md`): unit tests cover golden-set validation and leakage,
  the exact sign test against hand-computed values (7/0 → 1/128, 4/1 → 6/32), every verdict branch, routing applied to
  picks, selector errors counted as wrong, order preserved under concurrency, and cancellation.
- **Live gate, 4 known-answer candidates** on 73 sealed tasks with gpt-4.1-mini: real overlay **pass** (7 fixed, 0 broken,
  p=0.0078, matching the proof harness), harmful route **fail** (0 fixed, 11 broken, 65→54), empty overlay and neutral
  tweak **inconclusive** (identical outcomes).
- Shared `Overlay.ResolveRoute` now backs both the proxy and the gate; `mcp.VisibleTools` mirrors what the proxy exposes
  (including composed tools, sorted so map order cannot change the list).

### Bugs found and fixed
5. **Promote could succeed and change nothing.** `SetActiveVersion` updated the active pointer with a plain `UPDATE`; if the
   pointer row was missing it matched zero rows and still reported success. It also accepted a version id that does not
   exist. Now it upserts the pointer and returns `unknown version` for an id that matches nothing. `ActiveVersionID`
   returns "none" instead of an error when the row is missing. Covered by three store tests.
6. **Benchmark has repeated queries.** 13 queries (26 of 241 tasks) appear more than once, with consistent labels. The
   golden-set validator caught a train/holdout collision. Overlays act on tool names, not query text, so earlier
   conclusions stand, but the holdout is not perfectly disjoint.

### Re-verified after the fixes
- `go build ./...` and `go vet ./...` clean; `go test ./...` passes in all 8 test packages
  (agent, config, improvement-sandbox, mcp, observability, selection-gate, store, tracer).
- **Gate on the proposer's real candidate.** On unlabeled real traffic the proposer produced no routing, only cache rules
  and five composed tools. The selection gate returned **inconclusive**: 65 → 65 correct on 73 sealed tasks, 0 fixed,
  0 broken. It did no harm and no good, so it is correctly not shipped. This run also exercised the store fix live
  (the DB with an empty active-pointer table no longer crashes the gate).

- **Selection gate inside `evolve run`, live.** With the replay thresholds loosened for the test only (the default replay
  latency check discarded the first attempt on noise before the gate ran), `evolve run -auto-promote` went evaluate, live
  propose, replay sandbox (passed), selection gate (**inconclusive**: 65 → 65, 0 fixed, 0 broken) and ended
  "NOT PROMOTED". The store confirms it: active pointer still empty, candidate still `candidate`, experiments recorded
  as `passed` (replay) and `inconclusive` (gate). The post-gate and pre-gate branching is also a pure, unit-tested
  function (`cmd/evolve/next_action.go`), including: no golden set refuses auto-promote, fail discards even with
  auto-promote, and an unrecognized verdict never promotes.
- **Not exercised live:** the `pass`-then-auto-promote and `fail`-then-discard branches inside `run` (the proposer would
  not produce a routing fix on unlabeled traffic). They are covered by the unit tests and, for the gate verdicts
  themselves, by the four known-answer candidates above.

### Side branch (live, throwaway repo)
`evolve gate -branch -repo <demo repo>` on the real overlay: verdict **pass** (7 fixed, 0 broken), then
`side branch created: improve/overlay-cd4f4dc3 (commit 928176e)`. Checked afterward in the demo repo: `master` unchanged
(`926fbc2 init`), an uncommitted `wip.txt` still untracked, a single worktree, the commit contains exactly
`improvements/cd4f4dc3/{overlay.json,evidence.json,PR.md}`. The generated `PR.md` shows the 65/73 → 72/73 table, the routing
rule, the patched description verbatim, and the seven queries that flipped from `read_text_file` to `read_multiple_files`.
Unit tests cover: working tree untouched, HEAD and checked-out branch unchanged, no push to a configured remote, refusal to
overwrite, rejection of non-repo and empty repo, and cleanup when the commit fails (forced with a failing pre-commit hook).

### Not verified or not finished
- **No `-race` run** (no 64-bit C toolchain on this machine).
- **Notifications were not run live.** The live demo for the `fail` (harmful route) and `inconclusive` (neutral tweak)
  candidates was stopped by the system for low memory after the `pass` case finished; I did not restart it. Notification
  rendering and file writing are unit-tested (content, no-overwrite, timestamped names); the end-to-end CLI path for
  those two verdicts has not been executed.
- **Not pushed, no PR opened, no webhook/Slack/email.** Notifications are files plus a terminal line.
- **Descriptions and docs are not improved yet.** The proposer can still only change routing, tool descriptions, cache and
  composed tools at the proxy; rewriting upstream docs is not built.

### Verified in Docker and live (2026-11-14)
- `go vet ./...` clean; `go test -race -p 1 -count=1 ./...` in `golang:1.26` (memory-capped container) passes in every package,
  including `selection-gate`, `publish` and `cmd/evolve`. This closes the earlier "no race detector" gap.
- **Notifications, live:** `fail` (harmful route) and `inconclusive` (empty overlay, neutral tweak) each wrote a timestamped
  file under `<data_dir>/notifications/` with reason, evidence and the discard command, and printed a terminal line.
- **Selector flake found:** the neutral tweak was once judged `fail` (66 -> 65, one task broken) although it had scored 65 -> 65
  before. Hosted models are not deterministic at temperature 0. The majority-vote confirm step (`gate.confirm_votes`) now
  re-asks disputed tasks. Re-run live after rebuilding: neutral -> **inconclusive** (0 fixed, 0 broken); real overlay -> **pass**
  (p = 0.0078). One re-run is evidence, not proof the flake is gone; a repeated A/A run would quantify it.
- Docker command (Git Bash needs `MSYS_NO_PATHCONV=1`):
  `docker run --rm -m 1500m -v "$(pwd -W):/src" -w /src golang:1.26 sh -c "go vet ./... && go test -race -p 1 ./..."`

### Selector flake rate, measured (empty overlay vs itself, 73 sealed tasks, 5 runs each)
| Confirmation | Runs with a disputed task | Verdicts |
|--------------|---------------------------|----------|
| Off (`confirm_votes: -1`) | 3 of 5 (1 task each) | 1 false **fail**, 4 inconclusive |
| On (default 4 votes) | 2 of 5 disputed, both settled to no difference | 5 inconclusive, 0 false fails |

Without confirmation, a no-op candidate was falsely failed in 1 of 5 runs, so the zero-regression rule is unsafe on a raw
single pass. With it, 0 of 5. A single flipped task never reached `pass` in either mode (a pass needs about 5 fixes for
p <= 0.05). Small sample: it shows the flake is real and the fix works, not an exact rate.

### Product slices built on 2026-11-14 (all verified; commands below)
- **Hot reload, live.** `scripts/smoke/hotreload-smoke.mjs` runs the real proxy against the real filesystem server and changes the
  active version from another process with the real `evolve` CLI: promote picked up with no restart, `tools/list_changed` sent,
  `/health` and `/overlay` updated, an overlay routing to a missing tool refused while the good one kept serving, and
  `rollback -version none` restored the baseline. 8/8 checks. Unit tests (in-memory MCP client, `-race`) also cover: cache cleared
  on reload, a store outage never disturbs serving, concurrent calls during reloads, watcher stops with its context.
- **Two bugs the tests caught:** (1) the overlay validator cannot know upstream tool names, so a route to a nonexistent tool
  passed validation; the proxy now checks routes, cache rules and composed steps against the live tool list before loading.
  (2) a transient store error stayed on `/overlay` after recovery; a clean check now clears it.
- **Webhook.** Live: `EVOLVE_WEBHOOK_URL` + a real gate run delivered a counts-only JSON body to a local receiver. Tests: no
  task text in the payload, errors never contain the URL (it is the secret), redirects are not followed, non-http(s) refused.
- **Library (`simcp`).** Tools are plain Go handlers; the runtime treats the app as an `Upstream`. Live with `examples/notes`:
  traced mode recorded both calls (the failing one as `tool_error`) and served `/health`; raw mode (`SIMCP_RAW=1`) wrote nothing
  and listed the true tool list. Handler errors and panics become tool errors, not crashes.
- **Docker image.** Builds, runs non-root (uid 10001), proxy reports 14 healthy tools on `/health` through a published port, and
  `evolve` runs from the same image against the same data volume.

- **Upstream source patch** (`evolve upstream-patch`, see `docs/UPSTREAM_PATCH.md`). Live on the real File System server's
  source: the first run found nothing, because the real server writes descriptions as literals wrapped over lines and my planner
  only knew single literals. Fixed by reading adjacent literals as one string and writing the replacement back in the same layout.
  Then: the real gate-passed overlay edited `src/server.js:288`, the result parses (`node --check`), uncommitted work and the
  worktree list were untouched, and an inconclusive candidate was refused. The live edit also showed the gate's blind spot: the
  winning description was 81% shorter and dropped usage details, so the PR now shows before/after and flags large shrinkage.

Not verified: the library path has not been run through the selection gate end to end (only the unit and live checks above); the
webhook was tested against a local receiver, not a real Slack workspace.


## Final acceptance run (2026-11-15)

Goal: test the whole product as it now stands, on fresh data, with no hand-picked seed, and look for anything that does not hold.

**Setup.** `proof-fs -mode gate` runs the current product path on any MCPToolBench++ domain with no server (the dataset carries each
domain's real tool list and labels): observe the dev split, find confusions, propose with the production proposer, apply the
confusion-safe filter, then judge on the sealed split with 8 re-votes per disputed task, the decisive-flip rule, the route
executability check and the retention check. Two controls per domain check the gate itself: a deliberately harmful route (must
fail) and an empty overlay (must be inconclusive). `gpt-4.1-mini`, 70/30 split, seed 2026.

| Domain | Tools | Dev / sealed | Proposer found | Proposal verdict | Harmful control | Empty control |
|---|---|---|---|---|---|---|
| Pay | 6 | 216 / 93 | no confusions | inconclusive | fail (29 broken) | inconclusive |
| Search | 5 | 126 / 55 | no confusions | inconclusive | fail (17 broken) | inconclusive |
| Browser | 32 | 126 / 57 | 1 route (`playwright_screenshot` to `puppeteer_screenshot`) | fail (fixed 4, broke 3) | fail (5 broken) | inconclusive |
| Map | 32 | 346 / 150 | 3 routes | fail: `maps_distance_matrix` to `maps_distance` cannot run; the rest fixed 6, broke 3 | fail (15 broken) | inconclusive |
| File System | 11 | 165 / 73 | 1 route (`read_file` to `read_multiple_files`) | fail: route cannot run; without it fixed 0 | fail (14 broken) | inconclusive |

**Result.** Gate controls: 5 of 5 correct on both. Shipped changes: **0**. Blocked proposals: 3 (one real regression, two routes that
cannot run). **No domain produced an improvement that survives the gate and can execute.** The product's safety is demonstrated;
its ability to improve a server is not.

**Findings, each fixed with a regression test**
1. *Routes that cannot run.* Found by making a real routed call through the real proxy after promoting the File System overlay:
   `Invalid arguments for tool read_multiple_files`. The earlier +0.086 headline was entirely this route and is withdrawn. The gate,
   the proxy loader and the bench now reject any route whose target cannot take the source's required arguments.
2. *Majority voting false-failed an identical overlay.* The empty-overlay control returned `fail` (83 to 82) on Pay. Fixed with the
   decisive-flip rule and 8 re-votes; the same control was inconclusive on all 5 domains afterwards. Unstable tasks are reported.
3. *Sandbox of a name-only metric.* The benchmark and the old proof harness compare names; they cannot see argument fit. Argument
   fit now has its own check, but a benchmark that executes tools would be the real test.

**Also run as part of this pass:** `go vet` and `go test -race` in Docker; the stdio and hot-reload smoke scripts; the Docker image.

**What this does not establish.** One selector model. Fixed-size domain tool lists, not live servers (Browser, Map and Pay were not
executed). A name-and-argument-shape check cannot prove a route is *semantically* right. No domain gave the proposer a usable fix,
so the executable-and-useful case, the one that matters most, remains untested.

## Round 3: measuring what production feels, and an evidence-driven improver (2026-11-15)

### What the final run's "mistakes" really were
Recorded dev traffic from the final run, grouped by (picked, expected), showed most errors were not errors:

| Domain | Dev "wrong" | What most of it was |
|---|---|---|
| Map | 68% | the same action on a sibling server (Google Maps vs Amap vs Baidu), listed as equivalent in the dataset's own `similar_tools` |
| Browser | 22% | `playwright_*` vs `puppeteer_*` twins, also in `similar_tools` |
| Search | 25% | `tavily-search` vs `search`, also in `similar_tools` |
| File System | 10% | real: the deprecated alias `read_file` used for multi-file requests (x16) |
| Pay | 7% | real: `create_product` instead of `create_subscription_plan` (x10), no tool at all for shipment tracking (x6) |

So the gate had been scoring against an arbitrary choice between equivalent tools, and the earlier Browser route
(`playwright_screenshot` to `puppeteer_screenshot`) was "fixing" exactly that. Also, the description-only test in the final run
patched `read_text_file`, while the agent was actually picking `read_file`: the patch missed the real behaviour.

### Architecture changes (each with tests)
- **`acceptable_tools` on golden tasks.** Equivalent tools count as correct. The bench and `-export-golden` fill them from the
  dataset's `similar_tools`; reviewers can add them in a draft. Fingerprints of sets without them are unchanged (verified:
  `fs-seed42` still `bb3ea882...`).
- **Correct means executable.** The selector now returns the arguments it built (`CallSelector`). A pick counts only if the
  arguments validate against the input schema of the tool actually called, after routing. A route that renames a call into a tool
  its arguments do not fit now fails by measurement, not only by the separate route check.
- **Overlays can hide a tool** (`tool_patches.<name>.hidden`), for deprecated aliases. The loader refuses to hide unknown tools or
  every tool; PRs list hidden tools as "consider removing in the server".
- **Evidence-driven improver** (`agent.ImproveDescriptions`, `evolve improve`, and `evolve run` when a golden set exists). It sees
  every tool's description and the dev requests the agent got wrong, proposes description patches or a hide, measures them on DEV,
  refines from "still wrong" and "newly broken" feedback, and returns the best dev round (at least 2 tasks better than baseline).
  The sealed gate then judges it once. Guards: no routing, known tools only, no copied dev requests (it caught real leaks of the
  benchmark's fixture paths into descriptions), never hide every tool.
- **`upstream.dir`** in proxy config: the upstream's working directory, for servers that resolve relative paths.
- **Fatal API errors stop a run.** No credits / no quota / bad key now abort `Evaluate` at once (verified live: 4 s instead of 73
  failing calls in 48 s). The improver refuses a dev baseline with more than 5% selector failures, and a round with them is not
  counted. Found because credits ran out mid-run (below): the gate stayed safe (every affected verdict was `inconclusive`), but the
  improver would have "learned" from failures. The SDK's error text says only `429 Too Many Requests` for both a rate limit and empty
  credits, so the structured error code is read, not the message.
- **`evolve run` never judges an old candidate.** It used to take the newest candidate in the database; if the current run proposed
  nothing it would gate, or with `-auto-promote` promote, a stale one. It now only considers versions created during the run.
- **The bench executes against a fresh copy of the fixture.** Real calls move and write files; the first execution run had already
  moved `tests/` out of the fixture. Each arm now runs on its own temporary copy, and the fixture was restored.

### Results so far (gpt-4.1-mini, seed 2026)

| Domain | Sealed | Baseline: exact name / equivalent name / executable | Improver (dev) | Sealed verdict | Controls |
|---|---|---|---|---|---|
| Pay | 93 | 85 / 85 / 85 | 199 -> 199 of 216: no round beat baseline by 2 | none to judge | empty: inconclusive |
| Search | 55 | **42 / 54 / 53** | 123 -> 125 of 126 | **inconclusive** (53 -> 53) | harmful: fail (11 broken); empty: inconclusive |
| File System | 73 | 62 / 62 / 62 | 148 -> 150 of 165 | **inconclusive** (64 -> 64) | harmful: fail (14 broken); empty: inconclusive |
| Browser | 57 | 35 / 49 / 49 | not valid: credits ran out mid-run | not valid | not valid |
| Map | 150 | not run: no credits | | | |

- Counting equivalent tools changes the picture: Search goes from 42 to 54 of 55 correct, Browser from 35 to 49 of 57. Much of the
  apparent room to improve was label noise.
- Where real mistakes remain (Pay, File System), the improver's description changes did not carry over to the sealed tasks: small
  dev gains (+2) that the gate correctly judged as no evidence. **Still no shippable improvement on any domain**, now measured
  honestly: right tool, equivalent allowed, arguments valid.
- **Real execution on File System**: of the baseline's 62 correct picks, 48 ran successfully. Running the dataset's own 241 reference
  calls against the same fixture: 201 succeed. 22 `write_file` calls are refused by the server on this Windows setup ("not a regular
  file"), and about 18 `edit_file` calls fail because our hand-written fixture (`evaluation/setup_workspace.py`) does not contain the
  text the reference edits expect. So execution failures there are mostly the fixture, not the agent; execution scores on this
  benchmark are only comparable between arms, never as absolute numbers.

## Round 4: a positive control, and the first real pass (2026-11-18)

### Full rerun of all five domains (seed 2026, real descriptions, `data/bench-runs/2026-20261118-005053/`)

| Domain | Sealed correct (baseline) | Improver on dev | Sealed verdict | Harmful control | Empty control |
|---|---|---|---|---|---|
| Browser | 56 / 57 | 123 -> 126 | inconclusive (56 -> 56) | invalid (bug 1 below) | inconclusive |
| Map | 121 / 150 | no round beat baseline (diverged: 282 -> 266, 256, 241) | none | invalid (bug 1) | inconclusive |
| Pay | 86 / 93 | no gain | none | no compatible route exists | inconclusive |
| Search | 53 / 55 | no gain | none | fail, 11 broken | inconclusive |
| File System | 64 / 73 (51 execute) | 147 -> 149 | inconclusive (64 -> 64) | fail, 14 broken | inconclusive |

### What was wrong with the test itself
1. **The harmful control stopped being harmful.** It routed the most-used tool to *any* argument-compatible tool. Once equivalent
   tools counted as correct, that was the twin on a sibling server (playwright -> puppeteer), which breaks nothing: 56 -> 56 on Browser.
   It now picks the route that moves the most baseline-correct tasks to a tool that is not acceptable for them, and prints how many
   should break. Regression test: `cmd/proof-fs/bench_test.go`.
2. **No positive control.** The gate had only ever said `fail` or `inconclusive`. A gate that cannot say `pass` looks exactly like one
   that blocks everything. New: `-docs none` simulates a server that ships no descriptions (names and schemas only), and a `restore`
   control puts the real descriptions back.
3. **Some sealed sets cannot produce a pass at all.** A pass needs at least 5 fixed tasks with none broken (p <= 0.05). Browser's
   baseline misses 1 sealed task, Search's 2. The gate now records `headroom` and `min_fixed_to_pass`, and says "a pass is out of reach
   on this sealed set" instead of blaming the candidate with "no improvement".

### Improver changes
- **It sees the parameters.** The prompt said "as the agent sees them" but listed only names and descriptions. It now includes a
  compact schema per tool ("path (string, required)"); with missing descriptions that is most of what tells tools apart.
- **Pruning.** Proposals that rewrite many tools fixed some tasks and broke more, and refinement diverged (Map above). After each
  round, the patched tools involved in more broken than fixed dev tasks are dropped and the round is measured once more; the better
  of the two is kept and the next round is told. In the run below it fired in 10 of 15 rounds; on Map every full proposal was worse than
  the baseline (239, 246, 250 of 346 against 268) and every pruned one better (271, 260, 288): without pruning there would have
  been no candidate. The dropped patches were always the routing and geocoding tools, i.e. the improver chasing the
  geocode-vs-directions pattern that may be a label artefact (see Open, below).
- **Mistake groups are recorded** (`ImproveResult.Mistakes`, printed by `evolve improve` and the bench), so a reader can judge
  whether the improver was chasing real mistakes or label artefacts.

### Servers without descriptions (`DOCS=none`, seed 2026, `data/bench-runs/2026-none-20261118-010829/`)

| Domain | Baseline (sealed) | Headroom | Improver dev | Candidate on sealed | Harmful (predicted) | Empty | Restore real docs |
|---|---|---|---|---|---|---|---|
| Map | 105 / 150 | 45 | 268 -> 288 | **pass: 107 -> 124, fixed 17, broke 0, p < 0.0001** | fail, 6 (6) | inconclusive | fail: fixed 15, broke 2 |
| File System | 64 / 73 | 9 | 145 -> 149 | inconclusive (64 -> 64) | fail, 14 (14) | inconclusive | inconclusive (64 -> 64) |
| Pay | 89 / 93 | 4 | no gain | none | none possible | inconclusive | fail: broke 1 |
| Search | 54 / 55 | 1 | no gain | none | fail, 11 (11) | inconclusive | inconclusive |
| Browser | 56 / 57 | 1 | no gain | none | fail, 6 (6) | inconclusive | inconclusive |

- **The first gate `pass` on an executable change.** On Map the loop learned descriptions on dev, and on sealed tasks it never saw
  they fixed 17 and broke none, with arguments validated against the tool actually called. The retention check had nothing to lose
  (no old text). The descriptions state rules ("use this when you have coordinates and want the address"), name the neighbouring tool,
  use the parameters ("requires a uid parameter"), and copy no dev request.
- **They beat the server's own descriptions**, which fixed 15 and broke 2. The gate fails that by its zero-regression rule (`max_broken`
  0). So the restore control did not "pass" as first expected: the rule is conservative by design and a mostly-good change with any
  regression is rejected. Raising `max_broken` is a policy choice for the operator, not something to tune to make a control pass.
- **Descriptions do not matter much for this model elsewhere.** Without any descriptions Browser (56) and Search (54) score the same as
  with them, File System is unchanged (64), and Pay is better without (89 vs 86). Tool names and schemas carry the choice. That is why
  no improvement was possible on the full-docs runs: there was little that descriptions could fix.
- **Controls:** harmful failed with exactly the predicted number of broken tasks in all 4 domains that have one (Pay's tools share no
  arguments, so no route can run); empty was inconclusive in 5 of 5.
- **Open:** the remaining Map mistakes are mostly `maps_geocode` where directions were labelled. Geocoding first is a reasonable first
  step of a multi-step plan, so in a single-call benchmark these may be label artefacts rather than errors.

### Not done (OpenAI credits ran out again, `credit_balance_exhausted`)
- The full-docs rerun with the fixed harmful control and pruning (stopped at once by the fatal-error check, as designed).
- **A second seed for Map without descriptions.** One seed is one sample; this is the most important next run:
  `DOCS=none DOMAINS=map sh scripts/bench/run-all.sh 7`
- Then: `DOCS=full sh scripts/bench/run-all.sh` for the full-docs rerun.

### Also verified on 2026-11-18
- **OpenTelemetry against a real collector (Docker)**, `sh scripts/smoke/otel-smoke.sh`: spans carry `simcp.overlay.version` (`none`,
  then the promoted id after a live hot reload), `simcp.tool.requested` on a routed call, and `simcp.session.id`. 17 of 17 checks,
  3 runs in a row. Found: a hard-killed proxy loses buffered spans; a client closing stdin (what real clients do) shuts it down
  cleanly and everything is exported. The smoke now ends that way and checks a clean exit.
- **Hot-reload smoke repaired.** It promoted `read_text_file -> read_multiple_files`, which the loader now refuses (cannot run), and it
  raced the SDK's 10 ms debounce on `tools/list_changed`. It now uses the runnable alias route `read_file -> read_text_file`, checks
  that the routed call really executes, that a non-runnable route is refused while the good overlay keeps serving, and waits for the
  notification. 12 of 12.
- `go vet` and `go test -race ./...` in Docker (`golang:1.26`): all packages passed on the code before the pruning and
  mistake-recording change. The rerun on the final code was stopped for low memory on the host; the final code passes `go vet` and
  `go test ./...` natively (no race detector on this Windows host). Rerun the Docker command above when memory allows.

## Round 5: a small open agent as the primary testbed (Groq, 2026-11-18)

**Why.** On `gpt-4.1-mini` almost every domain left fewer than 5 sealed mistakes, and removing descriptions changed nothing on four
of five domains, so most tests could only say `inconclusive`. A test can only measure what the agent gets wrong. The primary
testbed is now `openai/gpt-oss-20b` as the agent and `openai/gpt-oss-120b` as the improver, on Groq (`PROVIDER=groq`, the default
in `scripts/bench/run-all.sh`). Llama 3.1 8B was the first choice; it is not offered on this Groq account (`model_not_found`).

**Harness changes** (each tested): any OpenAI-compatible provider via `OPENAI_BASE_URL`; `OPENAI_MAX_RETRIES` so per-minute
limits are retried as the server's Retry-After asks; a per-day limit and an unknown model are fatal and stop the run (Groq answers
429 `rate_limit_exceeded` for both "wait 5 s" and "come back tomorrow"); `-proposer-model` separates the improver from the agent;
`-baseline-only` probes the sealed baseline and headroom cheaply; `-skip-controls` saves calls on a tight quota.

**Probes (File System, 73 sealed tasks, seed 2026):**

| Agent | Real descriptions | No descriptions |
|---|---|---|
| `gpt-oss-20b` | 71 | 63 |
| `gpt-oss-120b` | 69 | 63 |
| `gpt-4.1-mini` (Round 4) | 64 | 64 |

**Full loop, not finished.** `DOCS=none`, 200 tasks (60 sealed), 2 rounds, controls skipped: sealed baseline 54 / 60, then the run
stopped during the improver's first DEV pass on Groq's free-tier daily token cap (about 200k tokens per model per day; the response
headers only show the per-minute budget). The stop is the intended behaviour. The full run needs roughly 900k agent tokens.

**Also found.** Editing `run-all.sh` while it was running broke the running copy (`sh` reads scripts incrementally): the first probe's
result had already been written and is valid, the wrapper then failed with "unexpected EOF". Do not edit the runner during a run.
