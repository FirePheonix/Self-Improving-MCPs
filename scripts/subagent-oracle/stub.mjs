// Local OpenAI-compatible stub. Answers chat/completions from a cache filled by
// subagents (see README.md in this folder). On a cache miss it records the request
// under data/oracle/pending/ and returns HTTP 400 (not retried by the SDK).
//
// Usage: node scripts/subagent-oracle/stub.mjs [port]   (default 8787)
import http from "node:http";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = process.env.ORACLE_ROOT || path.resolve(here, "..", "..", "data", "oracle");
const cacheDir = path.join(root, "cache");
const pendingDir = path.join(root, "pending");
fs.mkdirSync(cacheDir, { recursive: true });
fs.mkdirSync(pendingDir, { recursive: true });

const port = Number(process.argv[2] || 8787);

// Key only on what determines the answer: model, messages, and tool specs.
export function keyFor(body) {
  const canon = JSON.stringify({ model: body.model, messages: body.messages, tools: body.tools ?? null });
  return crypto.createHash("sha256").update(canon).digest("hex");
}

function completion(model, answer, isTool) {
  const base = {
    id: "oracle-" + crypto.randomUUID(),
    object: "chat.completion",
    created: Math.floor(Date.now() / 1000),
    model,
    usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 },
  };
  if (isTool) {
    if (!answer || !answer.tool) {
      return { ...base, choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: "" } }] };
    }
    return {
      ...base,
      choices: [{
        index: 0,
        finish_reason: "tool_calls",
        message: {
          role: "assistant",
          content: null,
          tool_calls: [{
            id: "call_" + crypto.randomUUID().slice(0, 8),
            type: "function",
            function: { name: answer.tool, arguments: JSON.stringify(answer.arguments ?? {}) },
          }],
        },
      }],
    };
  }
  return {
    ...base,
    choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: String(answer.content ?? "") } }],
  };
}

const server = http.createServer((req, res) => {
  if (req.method !== "POST" || !req.url.endsWith("/chat/completions")) {
    res.writeHead(404, { "content-type": "application/json" });
    return res.end(JSON.stringify({ error: { message: "oracle: unsupported route" } }));
  }
  let raw = "";
  req.on("data", (c) => (raw += c));
  req.on("end", () => {
    const body = JSON.parse(raw);
    const key = keyFor(body);
    const cachePath = path.join(cacheDir, key + ".json");
    const isTool = Array.isArray(body.tools) && body.tools.length > 0;

    if (fs.existsSync(cachePath)) {
      const answer = JSON.parse(fs.readFileSync(cachePath, "utf8"));
      res.writeHead(200, { "content-type": "application/json" });
      return res.end(JSON.stringify(completion(body.model, answer, isTool)));
    }

    const msgs = body.messages ?? [];
    const pending = {
      key,
      kind: isTool ? "select" : "propose",
      model: body.model,
      system: msgs[0]?.content ?? "",
      user: msgs[1]?.content ?? "",
      tools: body.tools ?? null,
    };
    fs.writeFileSync(path.join(pendingDir, key + ".json"), JSON.stringify(pending, null, 2));
    res.writeHead(400, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: { message: "oracle-miss " + key, type: "invalid_request_error", code: "oracle_miss" } }));
  });
});

server.listen(port, "127.0.0.1", () => {
  console.log(`oracle stub listening on http://127.0.0.1:${port}/v1  cache=${cacheDir}`);
});
