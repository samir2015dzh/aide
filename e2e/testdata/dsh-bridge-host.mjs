// A stand-in DSH Host for TestDSHBridgePlugin and the desktop's dsh spec:
// loads the real bridge plugin (apps/dsh-bridge, its TypeScript source)
// with a context that has DSH's services, and prints what DSH would have
// been asked, one JSON per line. With FAKE_DSH_LOG (an uncompressed
// session.v4.jsonl), a prompt is also written there as DSH writes a user
// message, its request id as the source's rpcId. Ends (disposing the
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
const live = new Set(["s_live"]);
const services = {
  sessionController: {
    prompt: async (req, signal) => {
      if (req.content[0]?.text === "hang") return new Promise(() => {}); // DSH that never takes it
      record(req);
      out({ prompt: req, signal: signal instanceof AbortSignal });
    },
  },
  agents: { get: (id) => (live.has(id) ? { status: "running", session: {}, cancel: (r) => out({ cancel: id, reason: r }) } : undefined) },
  sessions: { flush: async () => out({ flush: true }) },
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
