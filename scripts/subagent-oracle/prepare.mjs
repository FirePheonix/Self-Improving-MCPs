// Turns pending oracle misses into subagent batch files.
//
// Usage: node scripts/subagent-oracle/prepare.mjs <round> [batchSize=40]
// Output: data/oracle/batches/round-<round>/*.json  (each batch = one subagent job)
//
// Each batch carries only what the model would see: system prompt, the tool specs,
// and the user query. Dataset labels are never written here.
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = process.env.ORACLE_ROOT || path.resolve(here, "..", "..", "data", "oracle");
const pendingDir = path.join(root, "pending");
const cacheDir = path.join(root, "cache");

const round = process.argv[2];
const batchSize = Number(process.argv[3] || 40);
if (!round) {
  console.error("usage: prepare.mjs <round> [batchSize]");
  process.exit(2);
}
const outDir = path.join(root, "batches", `round-${round}`);
fs.mkdirSync(outDir, { recursive: true });

const pendingFiles = fs.existsSync(pendingDir) ? fs.readdirSync(pendingDir).filter((f) => f.endsWith(".json")) : [];
const open = [];
for (const f of pendingFiles) {
  const p = JSON.parse(fs.readFileSync(path.join(pendingDir, f), "utf8"));
  if (fs.existsSync(path.join(cacheDir, p.key + ".json"))) {
    fs.unlinkSync(path.join(pendingDir, f)); // already answered
    continue;
  }
  open.push(p);
}

const groups = new Map();
for (const p of open) {
  const gk = crypto.createHash("sha256").update(JSON.stringify([p.kind, p.system, p.tools])).digest("hex").slice(0, 12);
  if (!groups.has(gk)) groups.set(gk, []);
  groups.get(gk).push(p);
}

let batchCount = 0;
let g = 0;
for (const [gk, items] of groups) {
  g++;
  for (let i = 0; i < items.length; i += batchSize) {
    const chunk = items.slice(i, i + batchSize);
    const first = chunk[0];
    const name = `${first.kind}-g${g}-${gk}-b${Math.floor(i / batchSize)}`;
    const batch = {
      name,
      kind: first.kind,
      system: first.system,
      tools: first.tools,
      items: chunk.map((p) => (p.kind === "select" ? { id: p.key, query: p.user } : { id: p.key, user: p.user })),
      answers_file: path.join(root, "answers", `${name}.json`),
    };
    fs.writeFileSync(path.join(outDir, name + ".json"), JSON.stringify(batch, null, 2));
    batchCount++;
  }
}

console.log(`open requests: ${open.length}  batches: ${batchCount}  dir: ${outDir}`);
if (open.length === 0) console.log("nothing pending — oracle is complete for this run");
