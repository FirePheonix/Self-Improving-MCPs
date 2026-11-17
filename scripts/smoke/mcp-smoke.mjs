// Hot-path smoke test: drives bin/proxy.exe over stdio like Cursor would.
//
// Usage (from self-improving-mcp/):
//   node scripts/smoke/mcp-smoke.mjs <proxy-binary> <proxy-config.yaml> <workspace-dir>
//
// Checks: initialize, tools/list, two real tool calls, one unknown tool (expects a JSON-RPC error).
// Requires Node 18+ and npx (the proxy launches @modelcontextprotocol/server-filesystem).
import { spawn } from "node:child_process";

const [, , proxyBin, cfg, workspace] = process.argv;
if (!proxyBin || !cfg || !workspace) {
  console.error("usage: node mcp-smoke.mjs <proxy-binary> <proxy-config.yaml> <workspace-dir>");
  process.exit(2);
}

const p = spawn(proxyBin, ["-config", cfg], { stdio: ["pipe", "pipe", "inherit"] });
let buf = "";
let nextId = 0;
const pending = new Map();

p.stdout.on("data", (chunk) => {
  buf += chunk;
  let i;
  while ((i = buf.indexOf("\n")) >= 0) {
    const line = buf.slice(0, i).trim();
    buf = buf.slice(i + 1);
    if (!line) continue;
    const msg = JSON.parse(line);
    if (msg.id !== undefined && pending.has(msg.id)) {
      pending.get(msg.id)(msg);
      pending.delete(msg.id);
    }
  }
});

function rpc(method, params) {
  const id = ++nextId;
  p.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
  return new Promise((resolve) => pending.set(id, resolve));
}
function notify(method, params) {
  p.stdin.write(JSON.stringify({ jsonrpc: "2.0", method, params }) + "\n");
}
function summarize(res) {
  if (!res) return { missing: true };
  if (res.error) return { error: res.error };
  const text = res.result?.content?.[0]?.text ?? "";
  return { isError: !!res.result?.isError, text: text.slice(0, 80) };
}

let failures = 0;
const check = (name, ok) => {
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}`);
  if (!ok) failures++;
};

const init = await rpc("initialize", {
  protocolVersion: "2025-06-18",
  capabilities: {},
  clientInfo: { name: "mcp-smoke", version: "0.0.1" },
});
check("initialize returns serverInfo", !!init.result?.serverInfo);
notify("notifications/initialized", {});

const list = await rpc("tools/list", {});
const names = (list.result?.tools ?? []).map((t) => t.name);
check(`tools/list exposes tools (${names.length})`, names.includes("list_directory"));

const ls = await rpc("tools/call", { name: "list_directory", arguments: { path: workspace } });
check("list_directory returns content", !ls.result?.isError && !!ls.result?.content?.length);

const tree = await rpc("tools/call", { name: "directory_tree", arguments: { path: workspace } });
check("directory_tree returns content", !tree.result?.isError && !!tree.result?.content?.length);

const bad = await rpc("tools/call", { name: "no_such_tool", arguments: {} });
check("unknown tool gets JSON-RPC error", !!bad.error);

console.log(JSON.stringify({ list: summarize(ls), tree: summarize(tree), bad: summarize(bad) }, null, 1));

p.stdin.end();
setTimeout(() => {
  p.kill();
  console.log(failures === 0 ? "\nSMOKE OK" : `\nSMOKE FAILED (${failures})`);
  process.exit(failures === 0 ? 0 : 1);
}, 500);
