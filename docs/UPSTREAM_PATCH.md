# Carrying an improvement back into the server's source

An overlay fixes what agents read at the proxy. That keeps the upstream untouched, but the real server never
learns the lesson: every deployment needs the overlay forever. `evolve upstream-patch` turns a **gate-approved**
overlay into a real edit of the upstream server's own source and docs, on a local side branch, for a maintainer to
review. It is the "pull request" half of the loop.

```powershell
.\bin\evolve.exe gate -version <ID> -report report.json          # must end in: pass
.\bin\evolve.exe upstream-patch -version <ID> -repo <server-repo> -proxy-config config\proxy.yaml -report report.json
```

Result: a branch `improve/source-<id>` in `<server-repo>` holding the edited source/doc files plus
`improvements/<id>/{overlay.json,evidence.json,PR.md}`. Nothing is pushed and no pull request is opened.

## What is allowed to change source

- Only a recorded selection-gate **pass**. The newest gate decision for the version decides: a later `fail` or
  `inconclusive` overrides an older pass. Replay-sandbox results never count; they say calls still run, not that the
  agent chooses better.
- Only **tool descriptions**. They are the one part of an overlay that has a single place in source to change. Routing,
  cache rules and composed tools cannot be applied automatically; the PR lists each as a suggestion for a maintainer
  ("agents confuse A with B: consider merging", "consider caching inside the server").

## How a description is found

The current description comes from the live upstream's `tools/list`. The tool then finds it in the repo and rewrites it
only where it matches **exactly**:

| Where it lives | Handled |
|---|---|
| One `"..."` or `'...'` literal (Go, JS/TS, Python, JSON, Rust, Java, ...) | yes, re-escaped for the quote style |
| Several adjacent literals wrapped over lines (`"..." +` newline `"..."`, Python implicit concatenation) | yes, rewritten in the same layout and indentation |
| Go raw string, JS template literal | yes (a Go raw string cannot take a backtick; that case is skipped) |
| Plain text in `.md`, `.mdx`, `.txt`, `.rst` | yes (this is how README tool tables get updated) |
| Built from variables, generated, or split by code | **no**: reported, edit by hand |

It never guesses. A description is skipped, with the reason printed and put in the PR, when it is shorter than 20
characters (too generic), already the approved text, found nowhere, or found in more than 5 places (ambiguous).
`node_modules`, `vendor`, `dist`, `build`, `target`, `.git`, `bin` and virtualenvs are never read or edited.

## Safety properties (each has a test)

- Edits are planned against the **committed** files in a temporary git worktree. Your uncommitted work, including an
  uncommitted edit to the very same file, is never read, changed or swept into the commit.
- Only the edited files and `improvements/<id>/` are staged.
- An existing branch of the same name is never overwritten. A failed attempt leaves no branch and no worktree.
- Nothing is pushed; no PR is opened.

## What the PR tells the reviewer

Evidence (the same table as an overlay PR), a table of every edited file and line, every changed description
**verbatim, before and after**, the skipped tools with reasons, and the parts of the overlay that source edits cannot
carry. It also raises a flag when a description became much shorter (below 60% of its old length):

> the gate measures whether the agent picks the right tool, not whether the description still says what an agent needs
> once it has chosen: return format, limits, side effects.

This is not hypothetical. On the real File System server, the overlay that earned a gate pass shortened
`read_multiple_files` from 324 to 64 characters and dropped "each file's content is returned with its path" and "only
works within allowed directories". The branch was correct and the gate was right about selection, and a person still
has to decide whether that information may be lost. That is why the output is a branch for review.

## Verified

- Unit tests: every quoting style and escape, wrapped runs, partial-run and neighbouring-property non-matches,
  undecodable escapes, ambiguity and exclusion rules, uncommitted work untouched, refusal of non-pass, no leftover
  branch on failure, no overwrite, store ordering, and the gate-pass requirement.
- (Caveat added 2026-11-15: the overlay used below was a pass under the old name-only gate. Its route cannot execute and today the gate
  rejects it, so this run proves the patching mechanics, not that the change was useful.)
- Live, on the real File System server's published source copied into a throwaway repo: the real overlay (gate pass 65 to
  72 of 73) was located in `src/server.js:288` as a five-line wrapped literal and replaced; the edited file passes
  `node --check`; an uncommitted file stayed untouched; one worktree remained; an `inconclusive` candidate was refused.

## Not covered

- The repo used in the live check held the server's compiled JS, standing in for a maintainer's source. A real
  TypeScript repo has `.ts` files and the same layout of literals; that exact repo was not tested.
- Running the maintainer's own test suite after the edit is left to the reviewer; the tool does not know how to run it.
- Docs are updated only where they contain the old description text verbatim. Rewriting prose that merely explains a tool is
  not attempted.
