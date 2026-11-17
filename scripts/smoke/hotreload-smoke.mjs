// Hot-reload smoke test: a RUNNING proxy picks up promote / rollback / rollback-to-baseline
// done from another process through the real evolve CLI, with no restart.
//
// Usage (from self-improving-mcp/):
//   node scripts/smoke/hotreload-smoke.mjs <proxy-bin> <evolve-bin> <scratch-dir> <workspace-dir>
//
// It writes proxy.yaml and evolve.yaml into <scratch-dir> (use an empty one) and needs Node 18+ and npx.
import { spawn, execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

const [, , proxyArg, evolveArg, scratch, workspace] = process.argv;
const proxyBin = proxyArg && resolve(proxyArg);
const evolveBin = evolveArg && resolve(evolveArg);
if (!proxyBin || !evolveBin || !scratch || !workspace) {
  console.error("usage: node hotreload-smoke.mjs <proxy-bin> <evolve-bin> <scratch-dir> <workspace-dir>");
  process.exit(2);
}
const fwd = (p) => p.replaceAll("\\", "/");
mkdirSync(scratch, { recursive: true });
const port = 18700 + Math.floor(Math.random() * 200);
writeFileSync(join(scratch, "proxy.yaml"), `data_dir: ${fwd(scratch)}/data
upstream:
  command: npx
  args: ["-y", "@modelcontextprotocol/server-filesystem", "${fwd(workspace)}"]
  env: {}
redact_payloads: true
status_addr: 127.0.0.1:${port}
reload_interval_seconds: 1
`);
writeFileSync(join(scratch, "evolve.yaml"), `data_dir: ${fwd(scratch)}/data\n`);
const overlayFile = join(scratch, "overlay.json");
writeFileSync(overlayFile, JSON.stringify({
  tool_patches: { list_directory: { description: "HOT-RELOADED description" } },
  // an alias route that can execute: read_file (deprecated) takes the same arguments as read_text_file
  routing: [{ from_tool: "read_file", to_tool: "read_text_file" }],
}));
const badFile = join(scratch, "bad.json");
writeFileSync(badFile, JSON.stringify({ routing: [{ from_tool: "list_directory", to_tool: "no_such_tool" }] }));
// a route to a real tool whose arguments do not fit (path vs paths): it would fail on every call
const unrunnableFile = join(scratch, "unrunnable.json");
writeFileSync(unrunnableFile, JSON.stringify({ routing: [{ from_tool: "read_text_file", to_tool: "read_multiple_files" }] }));

const evolve = (...args) => execFileSync(evolveBin, args, { cwd: scratch, encoding: "utf8" });
const cfg = ["-config", join(scratch, "evolve.yaml")];

// run the proxy once so it migrates the DB, then import versions before the real session
const p = spawn(proxyBin, ["-config", join(scratch, "proxy.yaml")], { stdio: ["pipe", "pipe", "inherit"] });
let buf = "", nextId = 0;
const pending = new Map();
const notifications = [];
p.stdout.on("data", (chunk) => {
  buf += chunk;
  let i;
  while ((i = buf.indexOf("\n")) >= 0) {
    const line = buf.slice(0, i).trim(); buf = buf.slice(i + 1);
    if (!line) continue;
    const msg = JSON.parse(line);
    if (msg.id !== undefined && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); }
    else if (msg.method) notifications.push(msg.method);
  }
});
const rpc = (method, params) => {
  const id = ++nextId;
  p.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
  return new Promise((r) => pending.set(id, r));
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let failures = 0;
const check = (name, ok, extra = "") => { console.log(`${ok ? "PASS" : "FAIL"}  ${name} ${ok ? "" : extra}`); if (!ok) failures++; };
const listDesc = async () => (await rpc("tools/list", {})).result.tools.find((t) => t.name === "list_directory").description;
const health = async () => (await fetch(`http://127.0.0.1:${port}/health`)).json();
const waitFor = async (fn, ms = 8000) => { const end = Date.now() + ms; while (Date.now() < end) { if (await fn()) return true; await sleep(250); } return false; };

await rpc("initialize", { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "hr-smoke", version: "0" } });
p.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");

const baseDesc = await listDesc();
check("baseline served, health ok", (await health()).ok === true && !baseDesc.includes("HOT-RELOADED"));
// one real tool call on the baseline, so its span (overlay version "none") can be checked by otel-smoke.sh
const baseCall = await rpc("tools/call", { name: "list_directory", arguments: { path: workspace } });
check("baseline tool call runs", baseCall.result && !baseCall.result.isError, JSON.stringify(baseCall).slice(0, 300));

const imp = evolve("import", ...cfg, "-file", overlayFile).match(/candidate (\S+)/)[1];
evolve("promote", ...cfg, "-version", imp);
check("promote picked up WITHOUT restart", await waitFor(async () => (await listDesc()).includes("HOT-RELOADED")));
check("/health reports the new version", (await health()).active_version === imp);
// the SDK debounces list_changed (sent ~10 ms after the change), so wait for it rather than race it
check("client was notified tools/list_changed",
  await waitFor(async () => notifications.includes("notifications/tools/list_changed"), 3000), JSON.stringify(notifications));
const ov = await (await fetch(`http://127.0.0.1:${port}/overlay`)).json();
check("/overlay shows the live routing", ov.overlay.routing?.[0]?.to_tool === "read_text_file");
console.log(`active version: ${imp}`);
// a routed call must actually run, not just be renamed
const firstFile = (await rpc("tools/call", { name: "list_directory", arguments: { path: workspace } }))
  .result.content[0].text.split(/\r?\n/).map((l) => l.match(/^\[FILE\] (.+)$/)?.[1]).find(Boolean);
const routed = await rpc("tools/call", { name: "read_file", arguments: { path: join(workspace, firstFile ?? "missing") } });
check("routed call read_file -> read_text_file executes", firstFile && routed.result && !routed.result.isError,
  JSON.stringify(routed).slice(0, 300));

const unrunnable = evolve("import", ...cfg, "-file", unrunnableFile).match(/candidate (\S+)/)?.[1];
if (unrunnable) {
  evolve("promote", ...cfg, "-version", unrunnable);
  check("overlay with a route that cannot run is refused and the good one keeps serving",
    await waitFor(async () => (await health()).reload_error?.includes("read_multiple_files")) && (await health()).active_version !== unrunnable && (await listDesc()).includes("HOT-RELOADED"),
    JSON.stringify(await health()));
} else {
  check("overlay with a route that cannot run is refused (at import)", true);
}

const bad = evolve("import", ...cfg, "-file", badFile).match(/candidate (\S+)/)[1];
evolve("promote", ...cfg, "-version", bad);
check("overlay with a route to a missing tool is refused and the good one keeps serving",
  await waitFor(async () => (await health()).reload_error?.includes("does not exist")) && (await listDesc()).includes("HOT-RELOADED"),
  JSON.stringify(await health()));

evolve("rollback", ...cfg, "-version", "none");
check("rollback to baseline (-version none) picked up live", await waitFor(async () => (await listDesc()) === baseDesc));
check("/health back to none", await waitFor(async () => (await health()).active_version === "none"));

// end the session the way a client does (close stdin), so the proxy shuts down cleanly and flushes
// telemetry; a hard kill drops buffered spans. Kill only as a fallback.
const exited = new Promise((r) => p.on("exit", (code) => r(code)));
p.stdin.end();
const code = await Promise.race([exited, sleep(10000).then(() => "timeout")]);
check("proxy exits cleanly when the client closes stdin", code === 0, `exit: ${code}`);
if (code === "timeout") p.kill();
console.log(failures === 0 ? "\nALL PASS" : `\n${failures} FAILED`);
process.exit(failures === 0 ? 0 : 1);
