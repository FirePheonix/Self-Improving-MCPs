// Imports subagent answers into the oracle cache.
//
// Usage: node scripts/subagent-oracle/import.mjs [answersDir=data/oracle/answers]
// Each answers file is {"<id>": {"tool": "...", "arguments": {...}}} for select batches,
// or {"<id>": {"content": "<overlay json>"}} for propose batches.
// Answers are validated; bad ones are reported and NOT cached (so they stay pending).
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = process.env.ORACLE_ROOT || path.resolve(here, "..", "..", "data", "oracle");
const answersDir = process.argv[2] || path.join(root, "answers");
const batchesRoot = path.join(root, "batches");
const cacheDir = path.join(root, "cache");
fs.mkdirSync(cacheDir, { recursive: true });

// Map id -> batch metadata (kind + tool names) across all rounds.
const meta = new Map();
if (fs.existsSync(batchesRoot)) {
  for (const r of fs.readdirSync(batchesRoot)) {
    const dir = path.join(batchesRoot, r);
    for (const f of fs.readdirSync(dir)) {
      const b = JSON.parse(fs.readFileSync(path.join(dir, f), "utf8"));
      // tools are OpenAI-format: {type:"function", function:{name, ...}}
      const toolNames = (b.tools ?? []).map((t) => t.function?.name ?? t.name);
      for (const it of b.items) meta.set(it.id, { kind: b.kind, toolNames });
    }
  }
}

let ok = 0, bad = 0, invalidTool = 0;
for (const f of fs.readdirSync(answersDir).filter((x) => x.endsWith(".json"))) {
  const answers = JSON.parse(fs.readFileSync(path.join(answersDir, f), "utf8"));
  for (const [id, ans] of Object.entries(answers)) {
    const m = meta.get(id);
    if (!m) { bad++; console.log(`unknown id in ${f}: ${id}`); continue; }
    if (m.kind === "select") {
      if (ans.tool === null || ans.tool === undefined) {
        // model chose not to call a tool: cache as "no tool call"
      } else if (typeof ans.tool !== "string") {
        bad++; console.log(`bad tool type ${f} ${id}`); continue;
      } else if (!m.toolNames.includes(ans.tool)) {
        invalidTool++; // keep it: an invalid pick is a real (wrong) answer
      }
      if (ans.arguments !== undefined && (typeof ans.arguments !== "object" || Array.isArray(ans.arguments))) {
        bad++; console.log(`bad arguments ${f} ${id}`); continue;
      }
    } else {
      if (typeof ans.content !== "string") { bad++; console.log(`bad content ${f} ${id}`); continue; }
      try { JSON.parse(ans.content); } catch { bad++; console.log(`overlay not JSON ${f} ${id}`); continue; }
    }
    fs.writeFileSync(path.join(cacheDir, id + ".json"), JSON.stringify(ans));
    ok++;
  }
}
console.log(`imported ok=${ok} rejected=${bad} picks_outside_tool_list=${invalidTool} cache=${cacheDir}`);
