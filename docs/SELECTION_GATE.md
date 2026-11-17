# Selection gate

Decides whether a candidate overlay may ship, by measuring what matters: **does the agent pick the right tool more often?**

The older replay sandbox replays recorded calls and checks that routed calls still run. It cannot tell whether the agent
would have chosen well, and its latency and efficiency checks are noisy on small samples. The selection gate replaces
"did it run" with "did selection improve", on labeled tasks the improver never sees.

## How it works

1. A **golden set** of labeled tasks (`query` → `expected_tool`) is split into **dev** and **sealed**.
   The improvement agent may see dev. Only the gate uses sealed.
2. The gate builds the tool list the agent would see under the **active overlay** (baseline) and under the
   **candidate** (`mcp.VisibleTools`: patched descriptions plus composed tools), asks a selector model to pick a tool
   for every sealed task under each, and applies the overlay's routing to each pick (`Overlay.ResolveRoute`, the same
   function the proxy uses).
3. It compares the two runs **task by task** and returns one of three verdicts.

Temperature is 0, but hosted models are **not fully deterministic**: a borderline task can flip between identical calls.
Observed live: the same harmless description tweak scored 65 -> 65 once and 66 -> 65 (one "broken" task) another time, which
a strict zero-regression limit turned into a false `fail`. So tasks where baseline and candidate disagree are re-asked
(`gate.confirm_votes`, default 8 extra votes per arm; negative disables). Agreed tasks are not re-asked.

Plain majority voting is not enough. For a task the model answers right about half the time, two independent majorities disagree
about half the time, so an identical overlay still looked like a change (found on the Pay domain: the empty-overlay control was
falsely failed). So a re-voted task counts as **fixed** only if the candidate is right at least 75% of the time and the baseline at
most 25%, **broken** in the mirror case, and otherwise **unstable**: excluded from fixed/broken, marked in both runs, and counted in
`unstable_tasks` of the decision. A real effect is decisive and repeats; a coin-flip is not evidence about the overlay.

## Routes must be executable

A routing rule rewrites the tool *name* of a call, not its arguments. The gate therefore rejects, before any model call, a candidate
with a route whose target cannot take the source's required arguments (a missing required argument on either side, or a type
mismatch on a shared one). The proxy applies the same rule when it loads an overlay. This exists because of a real failure: routing
`read_text_file` (takes `path`) to `read_multiple_files` (takes `paths`) scored well on tool selection, and a real routed call
failed with `Invalid arguments for tool read_multiple_files`. Only optional arguments are not compared, so genuine aliases such as
`read_file` to `read_text_file` are allowed. The check cannot tell whether two compatible tools mean the same thing; the selection
test is what catches that.

## What counts as correct

A sealed task is correct only if all of these hold:

1. **The tool actually called is acceptable**: the label's `expected_tool`, or one of its `acceptable_tools` (tools that do the
   same job, such as the same action on two servers). Routing is applied first, exactly as the proxy does.
2. **The call could run**: the arguments the agent built validate against the input schema of the tool actually called. A route
   renames the call, not its arguments, so a routed call whose arguments do not fit the target is wrong.
3. **The overlay did not deny it.**

The selector reports its arguments through `CallSelector`; a name-only selector is still accepted, and its outcomes say
`args_checked: false`. A fatal API error (no credits, no quota, bad key) stops the run instead of being recorded as one failure
per task.

Why: on MCPToolBench++, most "mistakes" on Map, Browser and Search were the agent choosing an equivalent tool from a sibling server,
which the dataset itself lists in `similar_tools`, and the one route that "won" on File System could not execute. Scoring tool names
against one label rewarded overlays that steered the agent to the labeler's arbitrary choice and passed one that could not run.

## Where candidates come from: the improver

`evolve improve` (and `evolve run` when a golden set is configured) learns only from the **dev** split. It shows a model every
tool's description and the dev requests the agent got wrong (wrong tool, no tool, or right tool with arguments that cannot run),
grouped by pattern. The model proposes description patches or hides a deprecated alias; the patch is measured on dev; the result
("fixed", "still wrong", "newly broken") goes back for the next round. The best round, at least 2 dev tasks better than the
baseline, becomes the candidate. The gate then judges it on **sealed**, which the improver never saw.

The improver sees each tool's parameters as well as its description, as the agent does. After each round, patches for tools involved
in more broken than fixed dev tasks are dropped and the round is measured again; the better version is kept and the next round is
told what was dropped. Without this, refinement diverged on Map (282 dev baseline, then 266, 256, 241 as rounds rewrote more tools);
with it, the run that produced the first sealed pass kept the pruned version in all three rounds. The baseline's mistake groups are
recorded in the result, so a reader can check whether they are real mistakes or label artefacts.

Choosing the best of several dev rounds is optimistic: a lucky round looks good. That is why the dev score never ships anything;
the sealed gate does. Guards: no routing (a route cannot repair arguments built for the wrong tool), known tools only, no copied
dev requests (it caught real leaks of the benchmark's fixture paths), never hide every tool, and a dev baseline with more than 5%
selector failures is refused.

## Verdicts

| Verdict | Meaning | What happens |
|---------|---------|--------------|
| `pass` | Selection measurably improved, no regressions | May ship (promote, or later: open a branch) |
| `fail` | Selection got worse | Discard |
| `inconclusive` | Not enough evidence either way | **Not shipped**; left for a human, who should be told |

Checks run in this order; the first that applies decides:

1. Sealed set smaller than `min_sealed` (default 30) → inconclusive
2. Selector failed on more than `max_selector_error_rate` (default 5%) of calls in either run → inconclusive
3. Candidate broke more than `max_broken` (default 0) previously-correct tasks → **fail**
4. Candidate fixed no tasks → inconclusive ("no improvement")
5. Exact one-sided sign test on fixed vs broken, `p <= alpha` (default 0.05) → **pass**, else inconclusive

A regression fails even when other tasks improved. A flaky selector can never produce a pass: selector errors count as
wrong and, past the limit, void the result.

**Headroom.** A pass needs at least `min_fixed_to_pass` fixed tasks with none broken (5 at alpha 0.05). Each decision records
`headroom`, the sealed tasks the baseline gets wrong; when it is below that floor, no candidate can pass, and the reason says
"a pass is out of reach on this sealed set" instead of only "no improvement". Seen live: Browser's baseline missed 1 of 57 and
Search's 1 of 55, so those sets can only ever say inconclusive. Add harder tasks before expecting a pass.

**Zero regressions is a policy, and it bites.** On Map with descriptions removed, putting the server's real descriptions back fixed
15 sealed tasks and broke 2: `fail`. The improver's descriptions fixed 17 and broke 0: `pass`. A mostly-good change with any
regression is rejected; `max_broken` is the operator's choice.

## Golden set format

```json
{
  "name": "my-mcp-golden",
  "tasks": [
    {"id": "t1", "query": "read these three files", "expected_tool": "read_multiple_files", "split": "sealed"},
    {"id": "t2", "query": "list the project folder",  "expected_tool": "list_directory",       "split": "dev"}
  ]
}
```

Validation rejects: empty fields, duplicate ids, unknown split, no sealed tasks, and the **same query on both sides**
(normalized for case and whitespace), which would let the improver memorize an answer. Each decision records
`sealed_sha256`, so a later edit to the sealed set is detectable.

Build one from the File System benchmark with the same split the proof uses:

```powershell
.\bin\proof-fs.exe -limit 0 -seed 42 -export-golden data\golden\fs-seed42.json
```

(It drops dev tasks whose query also appears in sealed, and reports how many.)

## Using it

```yaml
# config/evolve.yaml
gate:
  golden_path: data/golden/fs-seed42.json
```

```powershell
.\bin\evolve.exe gate -version <ID> -proxy-config config\proxy.yaml -report report.json   # judge one candidate
.\bin\evolve.exe gate -version <ID> -exit-code                                             # CI: 0 pass, 3 fail, 4 inconclusive
.\bin\evolve.exe run -auto-promote                                                         # needs gate.golden_path
```

`evolve run` now ends in this gate. `-auto-promote` is **refused** when no golden set is configured, because without one
nothing verifies selection quality. The result is stored as an experiment (`passed` / `failed` / `inconclusive`) and
`-report` writes per-task outcomes for baseline and candidate, which is the evidence a reviewer needs.

## Side branch and notifications

What happens after a verdict (`evolve gate` and `evolve run`):

| Verdict | Action |
|---------|--------|
| `pass` | With `-branch -repo <dir>`: a local side branch `improve/overlay-<id>` holding `improvements/<id>/overlay.json`, `evidence.json` and `PR.md`. Without `-branch`: a hint only. |
| `fail` / `inconclusive` | Always a notification file in `<data_dir>/notifications/<timestamp>-<id>-<verdict>.md` plus a terminal line. Never silent. |

Safety properties of the branch step (each covered by a test):

- Built in a temporary git worktree from `HEAD`: the working tree and index you have open are never touched, so
  uncommitted work cannot end up in the commit.
- Exactly three files are committed, all under `improvements/<id>/`.
- **Nothing is pushed and no pull request is opened.** Review the branch, then push or open a PR yourself.
- An existing branch of the same name is never overwritten; on any failure the temporary worktree and half-made branch
  are removed.

`PR.md` lists the exact routing, cache rules and composed tools, shows new descriptions **verbatim** (they are what agents
read), and prints **review flags** for descriptions that contain a URL, are unusually long, or address the model as an
authority ("you must", "ignore previous", "system prompt", ...). The scan is crude on purpose: it raises a question and a
person answers it. It also lists every sealed task whose outcome flipped (query, expected tool, before, after).

## Evidence (File System, seed 42, 73 sealed tasks, gpt-4.1-mini, temperature 0)

| Candidate | Expected | Verdict | Baseline → candidate | Fixed / broken | p |
|-----------|----------|---------|----------------------|----------------|---|
| Earlier "winning" overlay (`read_text_file → read_multiple_files`, patch, cache), judged by name only | pass | **pass** (old gate) | 65 → 72 | 7 / 0 | 0.0078 |
| The same overlay under today's rules | fail | **fail** (route cannot run, no model call) | n/a | n/a | n/a |
| Same fix as descriptions only | no change | **inconclusive** | 65 → 65 | 0 / 0 | 1.0 |
| Harmful route (`list_directory → directory_tree`) | fail | **fail** | 65 → 54 | 0 / 11 | 1.0 |
| Empty overlay (A/A control) | no change | **inconclusive** | 65 → 65 | 0 / 0 | 1.0 |
| Neutral description tweak | no change | **inconclusive** | 65 → 65 | 0 / 0 | 1.0 |

The first row reproduced the independent proof harness (65 → 72), but only because both counted tool names. That route cannot execute,
so the "improvement" was not real and is withdrawn (rows 2 and 3). The harmful route is the kind of
change that produced the documented ungated regression (0.877 → 0.712); it is now caught with the exact count of broken
tasks. The A/A control shows the gate does not invent differences from selector noise.

## Building a golden set for your own server

The gate needs labeled tasks. `evolve golden` drafts them from the server's own tool docs, and a person approves them.

```powershell
.\bin\evolve.exe golden draft   -proxy-config config\proxy.yaml -out data\golden\draft.json -per-tool 6
# read draft.json: fix or delete bad tasks, set "approved": true on the ones you trust
.\bin\evolve.exe golden approve -draft data\golden\draft.json -out data\golden\mine.json -proxy-config config\proxy.yaml
```

- **Drafting** connects to the upstream, lists the tools, and asks the model for realistic requests per tool. Tasks are dropped
  when the label names a tool that does not exist, the query is empty or a duplicate, or the query contains the tool's own name
  (the answer is handed over). Nothing starts approved. It refuses to overwrite an existing draft.
- **Baseline pick.** Each task records what the selector chooses today. Disagreements are listed first: each is a real
  confusion (the improvement signal) or a wrong label (fix it). Review those first.
- **Approving** keeps only `approved: true` tasks, rejects labels naming unknown tools, and splits dev/sealed by a hash of the
  normalized query: deterministic, the same query can never land on both sides, and adding tasks does not reshuffle old ones.
  It refuses to overwrite an existing golden set, because past decisions record its fingerprint. It warns below 30 sealed tasks.
- **Why a person approves.** A model that writes the labels, judged by a model that picks tools, grades its own homework.
  The human step is what makes a sealed task a fact rather than a guess.

Live on the File System server (82 tasks drafted, 2 dropped for leaking a tool name): 11 disagreements, of which 6 were
`read_file` vs `read_text_file` (a deprecated alias, so arguably a wrong label: exactly what review is for) and 2 were the
known `read_multiple_files` -> `read_text_file` confusion, **found from the docs alone with no benchmark labels**.

Limits: drafted queries are shaped by the same tool descriptions the agent reads, so they are easier than real traffic.
Mixing in reviewed real queries is still better. Tools whose descriptions overlap heavily need the most careful review.

## Limits (read before relying on it)

- **Needs labels.** (`evolve golden` now drafts them for review; see above.) Sealed tasks need correct answers. The proof's confusion signal came from benchmark labels; on real,
  unlabeled traffic the proposer found none (see TESTING.md). Writing golden tasks from docs and reviewed traffic is the
  open work for production use.
- **One model; one passing case.** Controls validated on five MCPToolBench++ domains with one selector model. The only `pass` on an
  executable change so far is Map with descriptions removed, on one seed (TESTING.md, Round 4).
- **Selector is a model.** The gate measures a stand-in agent, not your production agent.
- **Replay sandbox still runs first** and can discard on noisy latency (observed: p95 163 ms → 263 ms over 50 calls).
  The selection gate is the authority on quality; the replay check should become advisory or significance-tested.
- **Race-tested in Docker.** `docker run golang:1.26 go test -race -p 1 ./...` passes for all packages (the Windows host has no
  C toolchain for `-race`).

## Retention check: a description must not lose what agents need after choosing

Selection accuracy measures whether the agent picks the right tool. A description can win that while dropping what an agent
needs *after* picking: result format, limits, side effects, preconditions. This was found on the real File System server: the
overlay that earned a selection pass shortened `read_multiple_files` from 324 to 64 characters and dropped three such sentences.

For every tool whose description a candidate changes, the gate now splits the served description into sentences and asks a
judge model, per sentence, two yes/no questions: is this information needed *after* choosing the tool, and does the new text
still state it? The judge runs 3 times (`gate.retention_votes`, negative disables) and the majority decides each sentence, so one
noisy answer cannot raise or hide a loss.

| Outcome | Verdict |
|---|---|
| Selection pass, nothing needed dropped | `pass` |
| Selection pass, needed sentences dropped | **`inconclusive`**, with the dropped sentences in the notification |
| Same, with `-accept-info-loss` (or `gate.accept_info_loss`) | `pass`, loss recorded as "accepted by a person" in `PR.md` |
| Judge unreachable or unparseable | `inconclusive` ("could not be verified"), even with `-accept-info-loss` |
| `fail` / `inconclusive` selection result | unchanged; the check never makes a result better |

Live (gpt-4.1-mini): the real overlay is held back with the three dropped sentences listed; the same overlay with
`-accept-info-loss` passes and records them; a version of the fix that keeps every detail passes with no loss reported.
The proposer prompt also now tells the model to keep usage sentences when it rewrites a description.

Limits: the judge is a model, so it can be wrong in both directions (a recorded loss that is not one, or a miss). Majority voting
reduces noise, not bias. It only compares old and new description text; it cannot know what your server actually returns.
