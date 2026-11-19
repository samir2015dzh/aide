// The bridge's local endpoint, for this computer's hermeshd only.
//
// Rendezvous: <home>/bridge.json (home is <DSH_HOME>/hermesh-bridge) holds
// the endpoint and a random token, written 0600 in a 0700 folder (on
// Windows, a folder in the user's profile, which only the user can read).
// The endpoint is a Unix socket in that folder, or on Windows a named pipe
// with a random name. Every request carries the token: knowing where the
// pipe is is not enough. One JSON object per line each way.
//
// Methods: hello; state {sessionId}; prompt {sessionId, requestId, text};
// cancel {sessionId}. A prompt waits at most promptTimeoutMs for DSH to
// accept it and then fails as "timeout": the caller is never left with a
// message that silently hangs.

import { randomBytes, timingSafeEqual } from "node:crypto";
import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync, chmodSync } from "node:fs";
import { createServer, type Server, type Socket } from "node:net";
import { join } from "node:path";
import { BridgeError, type Host } from "./host.ts";

export const PROTOCOL = 1;
const MAX_LINE = 256 * 1024;
const MAX_TEXT = 100 * 1024;
const MAX_ID = 200;

export interface Options {
  /** Where bridge.json (and on Unix the socket) go. */
  home: string;
  /** This bridge's version, as hermeshd shows it. */
  version: string;
  promptTimeoutMs?: number;
  platform?: NodeJS.Platform;
}

/** What hermeshd reads to find the bridge. */
export interface Rendezvous {
  protocol: number;
  endpoint: string;
  token: string;
  pid: number;
  version: string;
  startedAt: string;
}

export class BridgeServer {
  private readonly host: Host;
  private readonly opts: Required<Options>;
  private readonly token = randomBytes(32).toString("hex");
  private server: Server | undefined;
  private endpoint = "";
  private readonly sockets = new Set<Socket>();
  private readonly inflight = new Set<AbortController>();

  constructor(host: Host, opts: Options) {
    this.host = host;
    this.opts = { promptTimeoutMs: 20_000, platform: process.platform, ...opts };
  }

  get rendezvousPath() {
    return join(this.opts.home, "bridge.json");
  }

  async start(): Promise<Rendezvous> {
    mkdirSync(this.opts.home, { recursive: true, mode: 0o700 });
    if (this.opts.platform !== "win32") chmodSync(this.opts.home, 0o700);
    this.endpoint =
      this.opts.platform === "win32" ? `\\\\.\\pipe\\hermesh-dsh-${randomBytes(16).toString("hex")}` : join(this.opts.home, "bridge.sock");
    if (this.opts.platform !== "win32") {
      // The kernel cuts longer socket paths short (sun_path is 104–108 bytes).
      if (Buffer.byteLength(this.endpoint) > 100) throw new Error(`socket path too long: ${this.endpoint}`);
      rmSync(this.endpoint, { force: true });
    }
    const server = createServer((s) => this.serve(s));
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(this.endpoint, () => {
        server.off("error", reject);
        resolve();
      });
    });
    if (this.opts.platform !== "win32") chmodSync(this.endpoint, 0o600);
    this.server = server;
    const r: Rendezvous = { protocol: PROTOCOL, endpoint: this.endpoint, token: this.token, pid: process.pid, version: this.opts.version, startedAt: new Date().toISOString() };
    const tmp = this.rendezvousPath + ".tmp";
    writeFileSync(tmp, JSON.stringify(r), { mode: 0o600 });
    renameSync(tmp, this.rendezvousPath);
    return r;
  }

  async close() {
    for (const c of this.inflight) c.abort();
    // The rendezvous goes only while it is still this bridge's (a newer one may have started).
    try {
      const r = JSON.parse(readFileSync(this.rendezvousPath, "utf8")) as Partial<Rendezvous>;
      if (r.token === this.token) rmSync(this.rendezvousPath, { force: true });
    } catch {
      // Gone already.
    }
    for (const s of this.sockets) s.destroy();
    const server = this.server;
    this.server = undefined;
    if (server) await new Promise<void>((resolve) => server.close(() => resolve()));
    if (this.opts.platform !== "win32") rmSync(this.endpoint, { force: true });
  }

  private serve(s: Socket) {
    this.sockets.add(s);
    s.on("close", () => this.sockets.delete(s));
    s.on("error", () => s.destroy());
    s.setEncoding("utf8");
    let buf = "";
    s.on("data", (chunk: string) => {
      buf += chunk;
      if (buf.length > MAX_LINE && !buf.includes("\n")) return s.destroy();
      let nl: number;
      while ((nl = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, nl);
        buf = buf.slice(nl + 1);
        if (line.trim() !== "") void this.answer(s, line);
      }
    });
  }

  private async answer(s: Socket, line: string) {
    let id: unknown = null;
    const reply = (body: object) => {
      if (!s.destroyed) s.write(JSON.stringify({ id, ...body }) + "\n");
    };
    try {
      const req = JSON.parse(line) as { id?: unknown; token?: unknown; method?: unknown; params?: unknown };
      id = req.id ?? null;
      if (!this.authorized(req.token)) {
        reply({ error: { code: "unauthorized", message: "wrong token" } });
        return s.destroy();
      }
      reply({ result: await this.call(String(req.method), (req.params ?? {}) as Record<string, unknown>) });
    } catch (e) {
      const err = e instanceof BridgeError ? e : new BridgeError(e instanceof SyntaxError ? "bad-request" : "failed", e instanceof Error ? e.message : String(e));
      reply({ error: { code: err.code, message: err.message } });
    }
  }

  private authorized(token: unknown): boolean {
    if (typeof token !== "string" || token.length !== this.token.length) return false;
    return timingSafeEqual(Buffer.from(token), Buffer.from(this.token));
  }

  private async call(method: string, p: Record<string, unknown>): Promise<unknown> {
    switch (method) {
      case "hello":
        return { protocol: PROTOCOL, version: this.opts.version, pid: process.pid, services: this.host.services() };
      case "state":
        return this.host.state(id(p.sessionId, "sessionId"));
      case "prompt": {
        const sessionId = id(p.sessionId, "sessionId");
        const requestId = id(p.requestId, "requestId");
        const text = typeof p.text === "string" ? p.text : "";
        if (text.trim() === "") throw new BridgeError("bad-request", "the message is empty");
        if (text.length > MAX_TEXT) throw new BridgeError("bad-request", `the message is ${text.length} characters; at most ${MAX_TEXT}`);
        const ac = new AbortController();
        this.inflight.add(ac);
        const timer = setTimeout(() => ac.abort(), this.opts.promptTimeoutMs);
        try {
          await Promise.race([
            this.host.prompt(sessionId, requestId, text, ac.signal),
            // A Host that ignores the signal still doesn't hold the caller.
            new Promise<never>((_, reject) => ac.signal.addEventListener("abort", () => reject(new BridgeError("timeout", "DSH did not accept the message in time")))),
          ]);
        } finally {
          clearTimeout(timer);
          this.inflight.delete(ac);
        }
        return { accepted: true };
      }
      case "cancel":
        return { cancelled: this.host.cancel(id(p.sessionId, "sessionId")) };
      default:
        throw new BridgeError("bad-request", `no method ${method}`);
    }
  }
}

function id(v: unknown, name: string): string {
  if (typeof v !== "string" || v === "" || v.length > MAX_ID || /[\s/\\]/.test(v)) throw new BridgeError("bad-request", `bad ${name}`);
  return v;
}
