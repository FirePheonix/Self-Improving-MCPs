# Subagent oracle

Runs the File System proof (`cmd/proof-fs`) with **Claude subagents as the model**
instead of OpenAI. Used when the OpenAI account has no credits.

## Why it is valid (and its limits)

- The Go harness is unchanged except for `OPENAI_BASE_URL` (agent/openai.go), which points the
  OpenAI client at `stub.mjs`.
- `stub.mjs` answers each chat request from a cache keyed by a hash of (model, messages, tools).
  A miss is recorded and returned as HTTP 400 (the SDK does not retry 400).
- Cache entries are filled by Haiku subagents, one per batch of up to 40 queries sharing a tool list.
  Subagents see only the system prompt, tool specs, and query text. Dataset labels are never written
  to batch files, and subagents are told not to read `evaluation/` or the cache.
- Haiku is the chosen selector because it is the closest tier to `gpt-4.1-mini`, the model the
  benchmark was designed around. **Numbers are not directly comparable** to the documented 0.877 → 0.986.

## Loop

```powershell
cd self-improving-mcp
node scripts\subagent-oracle\stub.mjs 8787            # keep running in its own window
Remove-Item data\oracle\pending\*.json -ErrorAction SilentlyContinue
$env:OPENAI_API_KEY="oracle"; $env:OPENAI_BASE_URL="http://127.0.0.1:8787/v1"
.\bin\proof-fs.exe -limit 0 -data-dir data\oracle\runN   # 1. run; misses land in data\oracle\pending
node scripts\subagent-oracle\prepare.mjs N 40            # 2. group misses into batches
#    3. give each file in data\oracle\batches\roundN\ to one subagent; it writes data\oracle\answers\<name>.json
node scripts\subagent-oracle\import.mjs                  # 4. validate + cache answers
#    5. repeat 1-4 until pending is empty
```

Rounds are needed because the overlay depends on what was observed: a new proposal changes the
sandbox and evolved requests. Each run uses a fresh data dir, so results are reproducible from the cache.
