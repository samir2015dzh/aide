// A stand-in DSH Host for TestDSHBridgePlugin and the desktop's dsh spec:
// loads the real bridge plugin (apps/dsh-bridge, its TypeScript source)
// with a context that has DSH's services, and prints what DSH would have
// been asked, one JSON per line. With FAKE_DSH_LOG (an uncompressed
// session.v4.jsonl), a prompt is also written there as DSH writes a user
// message, its request id as the source's rpcId, and what a prompt set
// first (the session's model, its permission preset) is appended to
// FAKE_DSH_LOG + ".actions", one JSON per line. Ends (disposing the
// plugin) when stdin closes.
import { appendFileSync, readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const [entry] = process.argv.slice(2);
// Before the plugin loads (TypeScript, stripped by Node): a test that waits
// for "ready" can tell a Node that never ran from a plugin that never loaded.
process.stdout.write(JSON.stringify({ node: process.version }) + "\n");
const mod = await import(pathToFileURL(entry).href); // a Windows path (D:\...) is no module URL
const log = process.env.FAKE_DSH_LOG;
const record = (req) => {
  if (!log) return;
  const seqs = readFileSync(log, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l).seq).filter((s) => typeof s === "number");
  const seq = seqs.length ? Math.max(...seqs) + 1 : 0;
  const ev = { type: "user/message", seq, time: Date.now(), surfaceOp: "append",
    data: { id: req.requestId, role: "user", content: req.content, source: { kind: "user", rpcId: req.requestId } } };
  appendFileSync(log, JSON.stringify(ev) + "\n");
};
const out = (o) => process.stdout.write(JSON.stringify(o) + "\n");
const act = (o) => {
  out(o);
  if (log) appendFileSync(log + ".actions", JSON.stringify(o) + "\n");
};
const live = new Set(["s_live"]);
// Every session's model and permission preset, as DSH's projections would tell them.
const model = new Map();
const permission = new Map();
const agentOf = (id) => ({ id, status: live.has(id) ? "running" : "idle", session: { id }, cancel: (r) => out({ cancel: id, reason: r }) });
const services = {
  sessionController: {
    prompt: async (req, signal) => {
      if (req.content[0]?.text === "hang") return new Promise(() => {}); // DSH that never takes it
      record(req);
      out({ prompt: req, signal: signal instanceof AbortSignal });
    },
    resolveAgent: async (id) => ({ agent: agentOf(id) }),
    selectModel: async (req) => {
      model.set(req.sessionId, { provider: req.provider, model: req.model, reasoningEffort: req.reasoningEffort });
      act({ selectModel: req });
    },
  },
  agents: { get: (id) => (live.has(id) ? agentOf(id) : undefined) },
  sessions: { flush: async () => out({ flush: true }) },
  llm: {
    listProviders: () => [{ id: "deepseek", name: "DeepSeek" }],
    listModels: async () => [{ id: "deepseek-v4", name: "DeepSeek V4" }, { id: "deepseek-v4-flash", name: "V4 Flash" }],
    resolveModelInfo: async (_p, m) => (m === "deepseek-v4" ? { reasoning: { efforts: [{ id: "low" }, { id: "high" }] } } : {}),
    resolveCallConfig: async (sel) => {
      if (!sel.model.startsWith("deepseek-v4")) throw new Error("no such model");
      return {};
    },
  },
  permissionPresets: {
    catalog: () => ({ options: [{ value: "ask", name: "Ask first" }, { value: "auto", name: "Auto review", description: "reviewed" }] }),
    names: ["ask", "auto"],
    current: (session) => permission.get(session.id) ?? "ask",
  },
  commands: {
    find: (_agent, name) => (name === "permission" ? {} : undefined),
    execute: async (agent, line) => {
      permission.set(agent.id, line.split(" ")[1]);
      act({ command: line, sessionId: agent.id });
      return { result: { kind: "success" } };
    },
  },
  sessionProjections: { snapshot: (session) => ({ values: { modelSelection: { next: model.get(session.id) ?? { provider: "deepseek", model: "deepseek-v4-flash" } } } }) },
};
const disposers = [];
mod.apply({ get: (name) => services[name], effect: (fn) => disposers.push(fn()) });
out({ ready: true });
process.stdin.resume();
process.stdin.on("end", async () => {
  for (const d of disposers) await d();
  out({ disposed: true });
  process.exit(0);
});
