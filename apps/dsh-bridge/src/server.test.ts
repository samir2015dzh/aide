import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readFileSync, statSync } from "node:fs";
import { connect, type Socket } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { BridgeError, DshHost, type Host } from "./host.ts";
import { bridgeHome } from "./index.ts";
import { BridgeServer, type Rendezvous } from "./server.ts";

class FakeHost implements Host {
  prompts: { sessionId: string; requestId: string; text: string }[] = [];
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
  async prompt(sessionId: string, requestId: string, text: string, signal: AbortSignal) {
    if (this.fail) throw this.fail;
    if (this.hang) return new Promise<void>(() => {}); // ignores the signal, as the worst Host would
    void signal;
    this.prompts.push({ sessionId, requestId, text });
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
  const home = join(mkdtempSync(join(tmpdir(), "hmbridge-")), "hermesh-bridge");
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
  assert.deepEqual((await c.call("prompt", { sessionId: "s_live", requestId: "hermesh-01", text: "继续" })).result, { accepted: true });
  assert.deepEqual(host.prompts, [{ sessionId: "s_live", requestId: "hermesh-01", text: "继续" }]);
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
  const res = await c.call("prompt", { sessionId: "s_live", requestId: "hermesh-02", text: "hi" });
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
  assert.deepEqual((await c.call("prompt", { sessionId: "s_live", requestId: "hermesh-03", text: "hi" })).error, { code: "busy", message: "session/agent-busy: busy" });
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
  await host.prompt("s1", "hermesh-x", "hello", new AbortController().signal);
  assert.deepEqual(calls, [{ sessionId: "s1", requestId: "hermesh-x", mode: "queue", content: [{ type: "text", text: "hello" }] }]);
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

test("the bridge's folder follows DSH_HOME as hermeshd reads it", () => {
  assert.equal(bridgeHome({ DSH_HOME: "/srv/dsh" }), join("/srv/dsh", "hermesh-bridge"));
  assert.match(bridgeHome({}), /\.dsh[\\/]hermesh-bridge$/);
  assert.match(bridgeHome({ DSH_HOME: "  " }), /\.dsh[\\/]hermesh-bridge$/);
  assert.match(bridgeHome({ DSH_HOME: "~/other" }), /other[\\/]hermesh-bridge$/);
});
