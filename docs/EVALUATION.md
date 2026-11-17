# How we tested it

This document explains how Self-Improving MCP was evaluated, why the evaluation is built the way it is, what it found, and
what it does not show. The dated lab notebook with every individual run is [TESTING.md](TESTING.md).

## The questions

1. **Does the runtime work?** Does the proxy forward faithfully, record what it should, hot-reload safely, and report itself?
2. **Can the gate be trusted?** Does it fail harmful changes, stay quiet on non-changes, and pass a real improvement?
3. **Does the loop improve anything?** Does a change learned from one set of tasks help on tasks it never saw, measured the
   way production would feel it?

The third question is the product's reason to exist, and the hardest to answer honestly. Most of this document is about it.

## Layers of testing

| Layer | What it covers | How to run |
|---|---|---|
| Unit tests | Every package: detectors, overlay validation, route executability, statistics, verdict branches, re-voting, retention, improver guards and pruning, store, publishing safety, hot reload | `go test ./...` |
| Race detector | Concurrency in the proxy, reload and gate | `docker run --rm -m 1500m -v "$PWD:/src" -w /src golang:1.26 sh -c "go vet ./... && go test -race -p 1 ./..."` |
| Stdio smoke | Real proxy, real `@modelcontextprotocol/server-filesystem`, real MCP JSON-RPC | `node scripts/smoke/mcp-smoke.mjs ...` |
| Hot-reload smoke | A running proxy changed from another process by the real `evolve` CLI: promote, notify, routed call executes, bad and non-runnable overlays refused, rollback, clean exit (12 checks) | `node scripts/smoke/hotreload-smoke.mjs ...` |
| Telemetry | The same session against a real OpenTelemetry collector in Docker; checks span attributes before and after a live promote (5 checks) | `sh scripts/smoke/otel-smoke.sh` |
| Container | The image builds, runs non-root, serves a healthy proxy, and `evolve` shares its data volume | `docker build -t simcp .` |
| Benchmark | The whole improvement loop on five domains, with controls | `sh scripts/bench/run-all.sh` |

## The benchmark

### Data
[MCPToolBench++](https://huggingface.co/datasets/MCPToolBench/MCPToolBenchPP) provides, per domain, the real tool list of public MCP
servers (names, descriptions, input schemas) and labeled requests: the tool that should be called, and `similar_tools` the dataset
itself considers equivalent. Five domains have enough tools to test choosing: Browser (32 tools), File System (11), Map (32),
Pay (6), Search (5). Finance has one tool and is skipped.

Because the dataset carries the tool lists, choosing can be tested without running the servers. File System is also executed for
real: every sealed call goes through the actual proxy to the actual server, on a fresh copy of a fixture workspace per run.

### Splits
Per domain, tasks are shuffled with a fixed seed and split 70/30 into **DEV** (the improver may look) and **SEALED** (only the gate).
A DEV task whose normalized text also appears in SEALED is dropped, because the benchmark repeats some requests. Each decision
records the SHA-256 of the sealed set.

### The agent: a small open model is the primary testbed
The agent under test sees the tool list as the proxy would serve it (`mcp.VisibleTools`) and returns a tool call with arguments,
at temperature 0, through the same code path the product uses. Any OpenAI-compatible API works (`OPENAI_BASE_URL`).

| Role | Primary testbed (Groq) | Earlier runs (OpenAI) |
|---|---|---|
| Agent under test | `openai/gpt-oss-20b` | `gpt-4.1-mini` |
| Improver and retention judge | `openai/gpt-oss-120b` | `gpt-4.1-mini` |

`sh scripts/bench/run-all.sh` uses the Groq setup by default (`GROQ_API_KEY` in `.env`); `PROVIDER=openai` reproduces the earlier
runs. The improver is deliberately a larger model than the agent: we are testing whether better descriptions help a given agent,
not whether that agent can write them.

### Why stronger agents make poor test subjects
Results measured on a strong agent are ephemeral: they say little about the method and expire with the next model release.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/ceiling-dark.svg">
    <img alt="A strong agent misses 1 of 57 tasks, so no change can reach the 5 fixed a pass needs; a small agent without descriptions misses 10 of 73." src="assets/ceiling-light.svg" width="100%">
  </picture>
</p>

1. **No room, no evidence.** A change can only be measured on tasks the agent gets wrong, and a pass needs at least 5 fixed with
   none broken. `gpt-4.1-mini` misses 1 of 57 Browser and 2 of 55 Search tasks: every verdict there is `inconclusive` whatever the
   change does. A Claude-class selector solved 72 of 73 File System tasks, and 73 of 73 when told to read only the first sentence
   of each description. Even `gpt-oss-120b` with the real descriptions misses only 4 of 73: below the 5 a pass needs. Without
   descriptions both gpt-oss agents miss 10, which is the room the loop needs.
2. **Some agents barely read descriptions.** With every description removed, `gpt-4.1-mini` scored the same on File System, Browser
   and Search, and better on Pay: it chooses from tool names and schemas. A description improvement cannot show up in an agent
   that does not use descriptions. Both gpt-oss agents do use them: without descriptions `gpt-oss-20b` loses 8 of 73 File System
   tasks and `gpt-oss-120b` loses 6. This is a property of the model, not only of its size, which is one more reason a result on one
   strong model does not carry over.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/models-dark.svg">
    <img alt="Sealed tasks right without and with descriptions, by agent and domain." src="assets/models-light.svg" width="100%">
  </picture>
</p>
3. **Their few mistakes are noise.** What a strong model still gets wrong is a handful of borderline tasks it answers
   inconsistently from run to run (the reason for the gate's re-voting), plus label artefacts. Optimising against those learns the
   benchmark, not the tools, and the set changes with every model version.
4. **Small agents are where descriptions matter in production.** Cost, latency and self-hosting push real deployments toward small
   and open models, and these depend on the tool interface. That is the population this product is for.
5. **Cost decides how many runs you can afford.** Credible results need several seeds and controls. A small hosted model makes that
   affordable; a frontier model makes one run the whole budget.

The gate in production should still use a selector that resembles *your* agent; the testbed choice is about measuring the method.

### What counts as correct
We report three levels for every baseline, because the gap between them turned out to matter more than anything else:

| Level | Meaning | Why it is not enough alone |
|---|---|---|
| exact name | the tool equals the label | penalizes equivalent tools: on Map, Google vs Amap vs Baidu for the same action |
| equivalent name | the tool is the label or in its `similar_tools` | blind to arguments: a route can rename a call into a tool it cannot run |
| **executable** | equivalent, **and** the arguments validate against the schema of the tool actually called, after routing | — this is what the gate scores |

On File System the executed result (the call also succeeded on the real server) is reported next to it, for comparing arms only:
even the dataset's own reference calls succeed 201 of 241 times against our fixture.

### Controls
Every domain run judges up to three overlays whose correct verdict is known in advance:

- **Harmful route** (must `fail`): routes the tool that serves the most baseline-correct tasks to an argument-compatible tool that
  is *not* acceptable for them. The harness prints how many tasks it should break; the gate must report exactly that.
- **Empty overlay** (must be `inconclusive`): identical tool lists. Any difference is noise the gate failed to control.
- **Restore** (in the no-descriptions simulation): puts the server's real descriptions back. Expected to help; whether it passes
  depends on the zero-regression rule.

### Statistics
Fixed and broken tasks are counted per task, not as accuracy differences. Disputed tasks are re-asked 8 times per arm, and a flip
counts only when decisive (≥ 75% vs ≤ 25%). A pass needs at least 5 fixed, none broken, one-sided exact sign test p ≤ 0.05. The
decision records the **headroom** (sealed tasks the baseline gets wrong); below 5, a pass is impossible and the gate says so.

### Simulating poorly documented servers
`-docs none` serves every tool with its name and input schema but no description, as many real servers ship. Everything else is
unchanged. This gives the loop a deficit with a known remedy (the real descriptions), which is how we test whether the gate can
say `pass` at all.

## Results

All runs: seed 2026, 70/30 split, temperature 0. Logs in `data/bench-runs/` (not committed; regenerate with the script).

### Primary testbed: gpt-oss on Groq (File System)

Sealed baseline, 73 tasks, executable correctness:

| Agent | Real descriptions | No descriptions | Descriptions worth | Headroom with / without |
|---|---|---|---|---|
| `openai/gpt-oss-20b` | **71 / 73** | **63 / 73** | 8 tasks | 2 / 10 |
| `openai/gpt-oss-120b` | 69 / 73 | 63 / 73 | 6 tasks | 4 / 10 |
| `gpt-4.1-mini` (for comparison) | 64 / 73 | 64 / 73 | 0 | 9 / 9 |

- With their real descriptions, both gpt-oss agents leave fewer than 5 sealed mistakes, so no change can pass there (the ceiling
  above). Without descriptions both miss 10, and the real descriptions recover most of it: a gap with a known answer, exactly the
  setting where the loop should help.
- **The full loop on this setting has not finished yet.** A reduced run (200 tasks, 60 sealed, 2 improver rounds, controls skipped
  to save calls) measured the sealed baseline at 54 / 60 and was then stopped, as designed, by Groq's free-tier daily token cap
  (about 200k tokens per model per day) during the improver's first DEV pass. The full run needs roughly 900k agent tokens: one run
  on a paid tier, or several days on the free one. Command:
  `DOCS=none DOMAINS=file_system sh scripts/bench/run-all.sh 2026`.
- The bigger model is not better at choosing: `gpt-oss-120b` scores below `gpt-oss-20b` with the real descriptions (69 vs 71).

### gpt-4.1-mini, servers with their real descriptions

| Domain | Sealed | Correct: exact / equivalent / executable | Improver on DEV | Verdict on SEALED | Harmful | Empty |
|---|---|---|---|---|---|---|
| Browser | 57 | 38 / 56 / 56 | 123 → 126 | inconclusive, 56 → 56 (a pass is out of reach) | — ¹ | inconclusive |
| Map | 150 | 52 / 121 / 121 | no gain | — | — ¹ | inconclusive |
| Pay | 93 | 86 / 86 / 86 | no gain | — | none possible ² | inconclusive |
| Search | 55 | 42 / 54 / 53 | no gain | — | fail, 11 broken | inconclusive |
| File System | 73 | 64 / 64 / 64 (51 executed) | 147 → 149 | inconclusive, 64 → 64 | fail, 14 broken | inconclusive |

¹ This run's harmful control routed to an equivalent twin and broke nothing; the bug was fixed and the corrected control is in the
next table. ² Pay's tools share no argument shapes, so every route is rejected before measuring.

**Counting equivalents changes the picture.** Most "mistakes" on Map, Browser and Search were an equivalent tool on a sibling server.
With equivalents counted, Browser and Search have 1–2 sealed mistakes left: nothing to improve, and a pass is impossible.

### gpt-4.1-mini, servers without descriptions (`-docs none`)

| Domain | Baseline | Headroom | Improver on DEV | Candidate on SEALED | Harmful (predicted) | Empty | Real docs restored |
|---|---|---|---|---|---|---|---|
| **Map** | 105 / 150 | 45 | 268 → 288 | **pass: 107 → 124, fixed 17, broke 0, p < 0.0001** | fail, 6 (6) | inconclusive | fail: fixed 15, broke 2 |
| File System | 64 / 73 | 9 | 145 → 149 | inconclusive, 64 → 64 | fail, 14 (14) | inconclusive | inconclusive, 64 → 64 |
| Pay | 89 / 93 | 4 | no gain | — | none possible | inconclusive | fail: broke 1 |
| Search | 54 / 55 | 1 | no gain | — | fail, 11 (11) | inconclusive | inconclusive |
| Browser | 56 / 57 | 1 | no gain | — | fail, 6 (6) | inconclusive | inconclusive |

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/results-dark.svg">
    <img alt="Map without descriptions: before and after for each arm" src="assets/results-light.svg" width="100%">
  </picture>
</p>

### What the results say

- **The gate behaves correctly.** Harmful controls failed with exactly the predicted number of broken tasks in all four domains
  where one exists; empty controls were inconclusive in all five.
- **The loop can produce a real, executable improvement.** On Map without descriptions, descriptions learned on DEV fixed 17
  sealed tasks and broke none. They state general rules, name the neighbouring tool to use instead, use the parameter names, and
  copy no DEV request. Without pruning there would have been no candidate: every full proposal scored below the DEV baseline
  (239, 246, 250 of 346 against 268); the pruned ones scored 271, 260, 288.
- **The learned descriptions beat the server's own** (15 fixed, 2 broken). The zero-regression rule fails the real docs. That rule
  is deliberately conservative; `max_broken` is the operator's choice.
- **For this model, descriptions rarely matter on these benchmarks.** Without any descriptions, Browser, Search and File System
  score the same as with them, and Pay scores better (89 vs 86). The agent chooses mostly from tool names and schemas. That, not a
  weak improver, is why the full-description runs had nothing to ship.
- **Remaining Map mistakes may be the benchmark's, not the agent's.** The largest group is "called `maps_geocode` when directions
  were labeled": geocoding first is a reasonable first step of a multi-step plan, scored against a single-call label.

## What we got wrong, and how we found out

The evaluation changed several times because it was wrong. Each correction is a regression test now.

| Finding | How it surfaced | Consequence |
|---|---|---|
| A **+8.6-point headline gain was not real.** It came entirely from the route `read_text_file → read_multiple_files`, scored by tool *name*. | Promoting it and making a real call through the proxy: `Invalid arguments for tool read_multiple_files`. | Claim withdrawn. Routes must be executable; the gate scores arguments. |
| **Most "mistakes" were equivalent tools.** | Grouping recorded DEV traffic by (picked, expected). | `acceptable_tools`; the gate had been rewarding the labeler's arbitrary choice. |
| **A no-op overlay was failed.** Temperature 0 is not deterministic; a coin-flip task flips between identical runs. | Repeated A/A runs; then once more with plain majority voting on Pay. | Re-votes with the decisive-flip rule; unstable tasks reported. |
| **The harmful control stopped being harmful** once equivalents counted (it routed to a twin). | Browser control: 56 → 56. | Control now targets an unacceptable tool and predicts the break count. |
| **The gate had never said `pass`.** A gate that never passes is indistinguishable from one that cannot. | Review of the evidence. | The no-descriptions simulation and the restore control. |
| **Some sealed sets cannot produce a pass.** | Browser: baseline misses 1 of 57. | Decisions record headroom and say when a pass is out of reach. |
| **The improver diverged** as rounds rewrote more tools. | Map: 282 → 266 → 256 → 241 on DEV. | Pruning of patches that do more harm than good; the improver also now sees parameters. |
| **Credits ran out mid-run**, and failed calls looked like agent mistakes. | Browser and Map results built on 429s. | Fatal API errors stop the run; the improver refuses an unreliable baseline. |
| **`evolve run` could gate a stale candidate** from an earlier run when the current run produced none. | Code review during the rerun. | Only versions created in the current run are considered. |
| **Real execution mutated the shared fixture.** | Second execution run found files moved. | Each arm runs on a fresh copy. |
| **A Claude-class selector made almost no mistakes** on File System, even from names alone. | An early ablation. | The ceiling effect is real; the evaluation uses a model that makes mistakes and reports headroom. |

## Threats to validity

- **One positive result.** One domain, one seed, one model (`gpt-4.1-mini`, Map). The primary-testbed loop (gpt-oss, File System
  without descriptions) has not completed yet, and a second Map seed was blocked by API credits. These are the next runs.
- **The agent is a stand-in.** A model choosing from a list is not your production agent in a multi-step session.
- **Free-tier limits shape what can run.** Groq's free tier allows about 1,000 requests and 200k tokens per model per day, enough
  for baseline probes, not for a full loop in one day.
- **Benchmark labels, not traffic.** Labels can be wrong (see the Map geocoding group), and real users phrase things differently.
  `evolve golden draft` + human approval is the intended path for real servers; drafted tasks are easier than real queries.
- **Selection is not success.** Except for File System, calls were validated against schemas, not executed.
- **The no-descriptions setting is a simulation.** It is realistic for many servers, but it is not a measured server in the wild.
- **Best-of-rounds on DEV is optimistic.** That is why only SEALED decides, once, per candidate.

## Reproducing

```sh
# 1. data (36 MB) and the File System fixture, next to the repo
git clone https://huggingface.co/datasets/MCPToolBench/MCPToolBenchPP ../evaluation/MCPToolBenchPP
python scripts/bench/setup_workspace.py ../evaluation/workspace/test_project_root

# 2. primary testbed (Groq, gpt-oss-20b agent, gpt-oss-120b improver): put GROQ_API_KEY in .env
BASELINE_ONLY=1 DOMAINS=file_system sh scripts/bench/run-all.sh 2026            # cheap probe: baseline and headroom
BASELINE_ONLY=1 DOMAINS=file_system DOCS=none sh scripts/bench/run-all.sh 2026
DOCS=none DOMAINS=file_system sh scripts/bench/run-all.sh 2026                  # the full loop (~900k agent tokens)

# 3. the earlier OpenAI runs: all five domains, with and without descriptions, or another seed
PROVIDER=openai sh scripts/bench/run-all.sh 2026
PROVIDER=openai DOCS=none sh scripts/bench/run-all.sh 2026
PROVIDER=openai DOCS=none DOMAINS=map sh scripts/bench/run-all.sh 7
```

On a tight quota, `CONCURRENCY`, `LIMIT`, `ROUNDS` and `SKIP_CONTROLS=1` trade breadth for calls (see the script header).

Each domain writes a log with human-readable lines and one `BENCH_RESULT` JSON line (verdicts, counts, the candidate overlay, the
mistake groups, every improver round) to a new `data/bench-runs/<seed>-<docs>-<timestamp>/` folder, so earlier evidence is never
overwritten. Check credits first: a fatal API error stops the run within seconds, by design.
