import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readFileSync, statSync } from "node:fs";
import { connect, type Socket } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { BridgeError, DshHost, type Host, type Selections, type SessionOptions } from "./host.ts";
import { bridgeHome } from "./index.ts";
import { BridgeServer, type Rendezvous } from "./server.ts";
import { Outbox, replyTool, type Send } from "./outbox.ts";

class FakeHost implements Host {
  prompts: { sessionId: string; requestId: string; text: string; selections?: Selections }[] = [];
  cancelled: string[] = [];
  live = new Set<string>(["s_live"]);
  hang = false;
  fail: BridgeError | undefined;
  services() {
    return { sessionController: true, agents: true };
  }
  state(id: string) {
    return { live: this.live.has(id), status: this.live.has(id) ? "running" : "" };
  }
  async prompt(sessionId: string, requestId: string, text: string, signal: AbortSignal, selections?: Selections) {
    if (this.fail) throw this.fail;
    if (this.hang) return new Promise<void>(() => {}); // ignores the signal, as the worst Host would
    void signal;
    this.prompts.push({ sessionId, requestId, text, ...(selections ? { selections } : {}) });
  }
  async options(sessionId: string): Promise<SessionOptions> {
    return { models: [{ provider: "deepseek", providerName: "DeepSeek", model: "m1", modelName: "M1", efforts: ["low", "high"] }], permissions: [{ id: "ask", name: "Ask", description: "" }], current: sessionId === "s_live" ? { permission: "ask" } : {} };
  }
  cancel(id: string) {
    if (!this.live.has(id)) return false;
    this.cancelled.push(id);
    return true;
  }
}

/** A client speaking the bridge's protocol, one request at a time. */
async function client(r: Rendezvous) {
  const s: Socket = connect(r.endpoint);
  await new Promise<void>((res, rej) => s.once("connect", res).once("error", rej));
  s.setEncoding("utf8");
  let buf = "";
  const waiting: ((v: Record<string, unknown>) => void)[] = [];
  s.on("data", (c: string) => {
    buf += c;
    let nl: number;
    while ((nl = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, nl);
      buf = buf.slice(nl + 1);
      waiting.shift()?.(JSON.parse(line));
    }
  });
  let n = 0;
  const call = (method: string, params: object = {}, token = r.token) =>
    new Promise<Record<string, unknown>>((res) => {
      waiting.push(res);
      s.write(JSON.stringify({ id: ++n, token, method, params }) + "\n");
    });
  return { call, close: () => s.destroy(), socket: s };
}

async function started(host: Host, promptTimeoutMs = 2000) {
  const home = join(mkdtempSync(join(tmpdir(), "hmbridge-")), "aide-bridge");
  const server = new BridgeServer(host, { home, version: "0.1.0-test", promptTimeoutMs });
  const r = await server.start();
  return { server, r, home };
}

test("the rendezvous is the user's alone and names the endpoint and a token", async () => {
  const { server, r, home } = await started(new FakeHost());
  const onDisk = JSON.parse(readFileSync(join(home, "bridge.json"), "utf8")) as Rendezvous;
  assert.deepEqual(onDisk, r);
  assert.equal(r.protocol, 1);
  assert.match(r.token, /^[0-9a-f]{64}$/);
  if (process.platform !== "win32") {
    assert.equal(statSync(home).mode & 0o777, 0o700);
    assert.equal(statSync(join(home, "bridge.json")).mode & 0o777, 0o600);
    assert.equal(statSync(r.endpoint).mode & 0o777, 0o600);
  }
  await server.close();
  assert.equal(existsSync(join(home, "bridge.json")), false, "removed on close");
});

test("a request without the token is refused and the connection closed", async () => {
  const { server, r } = await started(new FakeHost());
  const c = await client(r);
  const closed = new Promise((res) => c.socket.once("close", res));
  const res = await c.call("hello", {}, "0".repeat(64));
  assert.deepEqual(res.error, { code: "unauthorized", message: "wrong token" });
  await closed;
  await server.close();
});

test("hello, state, prompt in queue order with its request id, cancel", async () => {
  const host = new FakeHost();
  const { server, r } = await started(host);
  const c = await client(r);
  const hello = (await c.call("hello")).result as Record<string, unknown>;
  assert.equal(hello.protocol, 1);
  assert.equal(hello.version, "0.1.0-test");
  assert.deepEqual(hello.services, { sessionController: true, agents: true });
  assert.deepEqual((await c.call("state", { sessionId: "s_live" })).result, { live: true, status: "running" });
  assert.deepEqual((await c.call("state", { sessionId: "s_cold" })).result, { live: false, status: "" });
  assert.deepEqual((await c.call("prompt", { sessionId: "s_live", requestId: "aide-01", text: "继续" })).result, { accepted: true });
  assert.deepEqual(host.prompts, [{ sessionId: "s_live", requestId: "aide-01", text: "继续" }]);
  assert.deepEqual((await c.call("cancel", { sessionId: "s_live" })).result, { cancelled: true });
  assert.deepEqual((await c.call("cancel", { sessionId: "s_cold" })).result, { cancelled: false });
  assert.deepEqual(host.cancelled, ["s_live"]);
  c.close();
  await server.close();
});

test("bad input is refused before DSH sees it", async () => {
  const host = new FakeHost();
  const { server, r } = await started(host);
  const c = await client(r);
  for (const params of [
    { sessionId: "s", requestId: "r", text: "   " },
    { sessionId: "", requestId: "r", text: "x" },
    { sessionId: "../x", requestId: "r", text: "x" },
    { sessionId: "s", requestId: "a b", text: "x" },
    { sessionId: "s", requestId: "r", text: "x".repeat(100 * 1024 + 1) },
  ]) {
    const res = await c.call("prompt", params);
    assert.equal((res.error as { code: string }).code, "bad-request", JSON.stringify(params).slice(0, 80));
  }
  assert.equal(((await c.call("nope")).error as { code: string }).code, "bad-request");
  assert.deepEqual(host.prompts, []);
  c.close();
  await server.close();
});

test("a Host that never answers fails the prompt as a timeout, not a silent hang", async () => {
  const host = new FakeHost();
  host.hang = true;
  const { server, r } = await started(host, 200);
  const c = await client(r);
  const t0 = Date.now();
  const res = await c.call("prompt", { sessionId: "s_live", requestId: "aide-02", text: "hi" });
  assert.equal((res.error as { code: string }).code, "timeout");
  assert.ok(Date.now() - t0 < 2000);
  c.close();
  await server.close();
});

test("DSH's refusal comes back with its code", async () => {
  const host = new FakeHost();
  host.fail = new BridgeError("busy", "session/agent-busy: busy");
  const { server, r } = await started(host);
  const c = await client(r);
  assert.deepEqual((await c.call("prompt", { sessionId: "s_live", requestId: "aide-03", text: "hi" })).error, { code: "busy", message: "session/agent-busy: busy" });
  c.close();
  await server.close();
});

test("DshHost: queue mode with the request id, DSH's error codes, cancel only a live Agent", async () => {
  const calls: unknown[] = [];
  let flushed = 0;
  let cancelled = 0;
  const agent = { status: "running", session: {}, cancel: () => void cancelled++ };
  const services: Record<string, unknown> = {
    sessionController: {
      prompt: async (req: unknown) => {
        calls.push(req);
      },
    },
    agents: { get: (id: string) => (id === "s1" ? agent : undefined) },
    sessions: { flush: async () => void flushed++ },
  };
  const host = new DshHost({ get: (n) => services[n] });
  assert.deepEqual(host.services(), { sessionController: true, agents: true });
  await host.prompt("s1", "aide-x", "hello", new AbortController().signal);
  assert.deepEqual(calls, [{ sessionId: "s1", requestId: "aide-x", mode: "queue", content: [{ type: "text", text: "hello" }] }]);
  assert.equal(flushed, 1);
  assert.deepEqual(host.state("s1"), { live: true, status: "running" });
  assert.deepEqual(host.state("s2"), { live: false, status: "" });
  assert.equal(host.cancel("s2"), false);
  assert.equal(host.cancel("s1"), true);
  assert.equal(cancelled, 1);

  const err = Object.assign(new Error("busy"), { code: "session/agent-busy" });
  services.sessionController = { prompt: async () => Promise.reject(err) };
  await assert.rejects(host.prompt("s1", "r", "x", new AbortController().signal), (e: BridgeError) => e.code === "busy" && e.message === "session/agent-busy: busy");

  const none = new DshHost({ get: () => undefined });
  assert.deepEqual(none.services(), { sessionController: false, agents: false });
  await assert.rejects(none.prompt("s1", "r", "x", new AbortController().signal), (e: BridgeError) => e.code === "unavailable");
});

test("the bridge's folder follows DSH_HOME as aided reads it", () => {
  assert.equal(bridgeHome({ DSH_HOME: "/srv/dsh" }), join("/srv/dsh", "aide-bridge"));
  assert.match(bridgeHome({}), /\.dsh[\\/]aide-bridge$/);
  assert.match(bridgeHome({ DSH_HOME: "  " }), /\.dsh[\\/]aide-bridge$/);
  assert.match(bridgeHome({ DSH_HOME: "~/other" }), /other[\\/]aide-bridge$/);
});

test("options and a prompt's model and permission, over the wire; a bad one refused", async () => {
  const home = mkdtempSync(join(tmpdir(), "hmb-"));
  const host = new FakeHost();
  const server = new BridgeServer(host, { home, version: "t" });
  const r = await server.start();
  const c = await client(r);
  try {
    const hello = (await c.call("hello")).result as { features: string[] };
    assert.deepEqual(hello.features, ["options", "selections", "from", "outbox"]);
    const opts = (await c.call("options", { sessionId: "s_live" })).result as SessionOptions;
    assert.equal(opts.models[0].model, "m1");
    assert.deepEqual(opts.current, { permission: "ask" });
    await c.call("prompt", { sessionId: "s_live", requestId: "aide-1", text: "go", model: { provider: "deepseek", model: "m1", effort: "high" }, permission: "ask" });
    await c.call("prompt", { sessionId: "s_live", requestId: "aide-2", text: "plain", permission: "" });
    assert.deepEqual(host.prompts, [
      { sessionId: "s_live", requestId: "aide-1", text: "go", selections: { model: { provider: "deepseek", model: "m1", effort: "high" }, permission: "ask" } },
      { sessionId: "s_live", requestId: "aide-2", text: "plain" },
    ]);
    const bad = await c.call("prompt", { sessionId: "s_live", requestId: "aide-3", text: "x", model: { provider: "deepseek", model: "a b" } });
    assert.equal((bad.error as { code: string }).code, "bad-request");
    assert.equal(host.prompts.length, 2);
  } finally {
    c.close();
    await server.close();
  }
});

test("DshHost: options from DSH's services; a prompt sets the model and permission as DSH's pickers do, only when they change", async () => {
  const log: string[] = [];
  const session = { id: "s1" };
  let perm = "ask";
  let model = { provider: "deepseek", model: "m1" } as { provider: string; model: string; reasoningEffort?: string };
  const agent = { id: "s1", status: "idle", session, cancel: () => {} };
  const services: Record<string, unknown> = {
    sessionController: {
      prompt: async (req: { requestId: string }) => void log.push("prompt " + req.requestId),
      resolveAgent: async (id: string) => (id === "s1" ? { agent } : { error: "gone" }),
      selectModel: async (req: { provider: string; model: string; reasoningEffort?: string }) => {
        log.push(`select ${req.provider}/${req.model}/${req.reasoningEffort ?? ""}`);
        model = req;
      },
    },
    agents: { get: (id: string) => (id === "s1" ? agent : undefined) },
    sessions: { flush: async () => {} },
    llm: {
      listProviders: () => [{ id: "deepseek", name: "DeepSeek" }, { id: "broken" }],
      listModels: async (p: string) => (p === "broken" ? Promise.reject(new Error("no key")) : [{ id: "m1", name: "M1" }, { id: "m2" }]),
      resolveModelInfo: async (_p: string, m: string) => (m === "m1" ? { reasoning: { efforts: [{ id: "low" }, { id: "high" }] } } : {}),
      resolveCallConfig: async (sel: { model: string }) => (sel.model === "nope" ? Promise.reject(new Error("unknown")) : {}),
    },
    permissionPresets: {
      catalog: () => ({ options: [{ value: "ask", name: "Ask" }, { value: "auto", name: "Auto", description: "reviewed" }, { value: "custom", name: "Custom" }] }),
      names: ["ask", "auto"],
      current: () => perm,
    },
    commands: {
      find: (_a: unknown, name: string) => name === "permission",
      execute: async (_a: unknown, line: string) => {
        log.push(line);
        perm = line.split(" ")[1];
        return { result: { kind: "success" } };
      },
    },
    sessionProjections: { snapshot: () => ({ values: { modelSelection: { next: model } } }) },
  };
  const host = new DshHost({ get: (n) => services[n] });
  const opts = await host.options("s1");
  assert.deepEqual(
    opts.models.map((m) => `${m.provider}/${m.model}:${m.efforts.join(",")}`),
    ["deepseek/m1:low,high", "deepseek/m2:"],
  );
  assert.deepEqual(opts.permissions.map((p) => p.id), ["ask", "auto"]);
  assert.deepEqual(opts.current, { model: { provider: "deepseek", model: "m1", effort: "" }, permission: "ask" });

  const sig = new AbortController().signal;
  await host.prompt("s1", "aide-a", "go", sig, { model: { provider: "deepseek", model: "m1", effort: "high" }, permission: "auto" });
  await host.prompt("s1", "aide-b", "again", sig, { model: { provider: "deepseek", model: "m1", effort: "high" }, permission: "auto" });
  assert.deepEqual(log, ["select deepseek/m1/high", "/permission auto", "prompt aide-a", "prompt aide-b"]);

  await assert.rejects(host.prompt("s1", "aide-c", "x", sig, { model: { provider: "deepseek", model: "nope" } }), (e: BridgeError) => e.code === "bad-request");
  await assert.rejects(host.prompt("s1", "aide-d", "x", sig, { permission: "custom" }), (e: BridgeError) => e.code === "bad-request");
  await assert.rejects(host.prompt("s9", "aide-e", "x", sig, { permission: "ask" }), (e: BridgeError) => e.code === "not-found");
  assert.equal(log.filter((l) => l.startsWith("prompt")).length, 2, "nothing sent when a selection fails");
});

test("a prompt from an agent tells the model who wrote it and how to answer; a bad sender refused", async () => {
  const host = new FakeHost();
  const { server, r } = await started(host);
  const c = await client(r);
  try {
    await c.call("prompt", { sessionId: "s_live", requestId: "aide-m1", text: "how far?", from: { address: "c_muse", name: "Muse\nthe agent" }, messageId: "msg_01abc" });
    await c.call("prompt", { sessionId: "s_live", requestId: "aide-m2", text: "the owner's" });
    assert.equal(host.prompts[0].text, "[Aide] A message (msg_01abc) from the agent Muse the agent (c_muse), through the owner's Aide. To answer, call aide_message_send.\n\nhow far?");
    assert.equal(host.prompts[1].text, "the owner's");
    const bad = await c.call("prompt", { sessionId: "s_live", requestId: "aide-m3", text: "x", from: { address: "c muse" } });
    assert.equal((bad.error as { code: string }).code, "bad-request");
    assert.equal(host.prompts.length, 2);
  } finally {
    c.close();
    await server.close();
  }
});

test("the outbox: a session's send waits for aided, which takes it once and answers it; unanswered, it fails", async () => {
  const host = new FakeHost();
  const home = join(mkdtempSync(join(tmpdir(), "hmbo-")), "aide-bridge");
  const server = new BridgeServer(host, { home, version: "t" }, new Outbox(300));
  const r = await server.start();
  const c = await client(r);
  const c2 = await client(r);
  try {
    const tool = replyTool(server.outbox);
    assert.equal(tool.name, "aide_message_send");
    assert.deepEqual(tool.parameters.required, ["text"]);
    // A session's model calls the tool; aided, polling, takes the send.
    const taking = c.call("outbox", { waitMs: 2000 });
    const sending = tool.execute({ text: "done: 3 of 4", reply_to: "msg_01abc" }, { agent: { id: "s_live" } });
    const took = (await taking).result as { sends: Send[] };
    assert.equal(took.sends.length, 1);
    const s = took.sends[0];
    assert.deepEqual({ ...s, id: "" }, { id: "", sessionId: "s_live", text: "done: 3 of 4", replyTo: "msg_01abc" });
    // Handed out once.
    assert.deepEqual(((await c.call("outbox", { waitMs: 0 })).result as { sends: Send[] }).sends, []);
    assert.deepEqual((await c2.call("outboxDone", { id: s.id, ok: true, messageId: "msg_02def", to: "c_muse" })).result, { found: true });
    assert.deepEqual(await sending, { message_id: "msg_02def", to: "c_muse" });
    assert.deepEqual(tool.output.render({}, { message_id: "msg_02def", to: "c_muse" }), [{ type: "text", text: "Sent msg_02def to c_muse through Aide." }]);

    // Refused by aided: the tool fails with its reason.
    const refused = tool.execute({ text: "to a session", to: "m_x/s_y" }, { agent: { session: { id: "s_live" } } });
    const refusal = assert.rejects(refused, (e: BridgeError) => e.code === "refused" && /not to sessions/.test(e.message));
    const t2 = ((await c.call("outbox", { waitMs: 2000 })).result as { sends: Send[] }).sends[0];
    assert.equal(t2.to, "m_x/s_y");
    await c.call("outboxDone", { id: t2.id, ok: false, error: { code: "refused", message: "a DSH session writes to agents, not to sessions" } });
    await refusal;

    // No aided taking it: it fails in time, and is no longer handed out.
    await assert.rejects(tool.execute({ text: "anyone?" }, { agent: { id: "s_live" } }), (e: BridgeError) => e.code === "timeout");
    assert.deepEqual(((await c.call("outbox", { waitMs: 0 })).result as { sends: Send[] }).sends, []);
    // Not in a session, or empty: refused here.
    await assert.rejects(tool.execute({ text: "x" }, {}), /runs in a session/);
    await assert.rejects(tool.execute({ text: " " }, { agent: { id: "s_live" } }), (e: BridgeError) => e.code === "bad-request");
    // An answer to no send.
    assert.deepEqual((await c.call("outboxDone", { id: "out_nope", ok: true })).result, { found: false });
  } finally {
    c.close();
    c2.close();
    await server.close();
  }
});

test("the plugin registers aide_message_send where DSH has a tool registry, and removes it when disposed", async () => {
  const { apply } = await import("./index.ts");
  const registered: string[] = [];
  let disposed = 0;
  let cleanup: (() => Promise<void> | void) | undefined;
  const dsh = mkdtempSync(join(tmpdir(), "hmbt-"));
  const prev = process.env.DSH_HOME;
  process.env.DSH_HOME = dsh;
  try {
    apply({
      get: (n: string) =>
        n === "tools"
          ? {
              register: (d: { name: string }) => {
                registered.push(d.name);
                return () => void disposed++;
              },
            }
          : undefined,
      effect: (f: () => () => Promise<void> | void) => {
        cleanup = f();
      },
    });
    assert.deepEqual(registered, ["aide_message_send"]);
    await cleanup?.();
    assert.equal(disposed, 1);
  } finally {
    if (prev === undefined) delete process.env.DSH_HOME;
    else process.env.DSH_HOME = prev;
  }
});
