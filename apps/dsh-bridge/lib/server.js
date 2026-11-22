// The bridge's local endpoint, for this computer's aided only.
//
// Rendezvous: <home>/bridge.json (home is <DSH_HOME>/aide-bridge) holds
// the endpoint and a random token, written 0600 in a 0700 folder (on
// Windows, a folder in the user's profile, which only the user can read).
// The endpoint is a Unix socket in that folder, or on Windows a named pipe
// with a random name. Every request carries the token: knowing where the
// pipe is is not enough. One JSON object per line each way.
//
// Methods: hello; state {sessionId}; prompt {sessionId, requestId, text,
// model?: {provider, model, effort?}, permission?}; options {sessionId};
// cancel {sessionId}; outbox {waitMs}; outboxDone {id, ok, messageId?, to?,
// error?}. hello lists the features beyond the first version ("options",
// "selections"; "from": a prompt may name the agent it is from and its
// message id, which the session's model reads before the text; "outbox":
// the session's sends through Aide, which aided takes with outbox and
// answers with outboxDone), so aided knows what an older bridge lacks. A
// prompt waits at most promptTimeoutMs for DSH to accept it and then fails
// as "timeout": the caller is never left with a message that silently
// hangs.
import { randomBytes, timingSafeEqual } from "node:crypto";
import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync, chmodSync } from "node:fs";
import { createServer } from "node:net";
import { join } from "node:path";
import { BridgeError } from "./host.js";
import { Outbox } from "./outbox.js";
export const PROTOCOL = 1;
const MAX_LINE = 256 * 1024;
const MAX_TEXT = 100 * 1024;
const MAX_ID = 200;
const MAX_OUTBOX_WAIT = 25_000;
export class BridgeServer {
    host;
    opts;
    token = randomBytes(32).toString("hex");
    server;
    endpoint = "";
    sockets = new Set();
    inflight = new Set();
    /** The sessions' sends through Aide, for aided to take. */
    outbox;
    constructor(host, opts, outbox = new Outbox()) {
        this.host = host;
        this.outbox = outbox;
        this.opts = { promptTimeoutMs: 20_000, platform: process.platform, ...opts };
    }
    get rendezvousPath() {
        return join(this.opts.home, "bridge.json");
    }
    async start() {
        mkdirSync(this.opts.home, { recursive: true, mode: 0o700 });
        if (this.opts.platform !== "win32")
            chmodSync(this.opts.home, 0o700);
        this.endpoint =
            this.opts.platform === "win32" ? `\\\\.\\pipe\\aide-dsh-${randomBytes(16).toString("hex")}` : join(this.opts.home, "bridge.sock");
        if (this.opts.platform !== "win32") {
            // The kernel cuts longer socket paths short (sun_path is 104–108 bytes).
            if (Buffer.byteLength(this.endpoint) > 100)
                throw new Error(`socket path too long: ${this.endpoint}`);
            rmSync(this.endpoint, { force: true });
        }
        const server = createServer((s) => this.serve(s));
        await new Promise((resolve, reject) => {
            server.once("error", reject);
            server.listen(this.endpoint, () => {
                server.off("error", reject);
                resolve();
            });
        });
        if (this.opts.platform !== "win32")
            chmodSync(this.endpoint, 0o600);
        this.server = server;
        const r = { protocol: PROTOCOL, endpoint: this.endpoint, token: this.token, pid: process.pid, version: this.opts.version, startedAt: new Date().toISOString() };
        const tmp = this.rendezvousPath + ".tmp";
        writeFileSync(tmp, JSON.stringify(r), { mode: 0o600 });
        renameSync(tmp, this.rendezvousPath);
        return r;
    }
    async close() {
        for (const c of this.inflight)
            c.abort();
        this.outbox.close();
        // The rendezvous goes only while it is still this bridge's (a newer one may have started).
        try {
            const r = JSON.parse(readFileSync(this.rendezvousPath, "utf8"));
            if (r.token === this.token)
                rmSync(this.rendezvousPath, { force: true });
        }
        catch {
            // Gone already.
        }
        for (const s of this.sockets)
            s.destroy();
        const server = this.server;
        this.server = undefined;
        if (server)
            await new Promise((resolve) => server.close(() => resolve()));
        if (this.opts.platform !== "win32")
            rmSync(this.endpoint, { force: true });
    }
    serve(s) {
        this.sockets.add(s);
        s.on("close", () => this.sockets.delete(s));
        s.on("error", () => s.destroy());
        s.setEncoding("utf8");
        let buf = "";
        s.on("data", (chunk) => {
            buf += chunk;
            if (buf.length > MAX_LINE && !buf.includes("\n"))
                return s.destroy();
            let nl;
            while ((nl = buf.indexOf("\n")) >= 0) {
                const line = buf.slice(0, nl);
                buf = buf.slice(nl + 1);
                if (line.trim() !== "")
                    void this.answer(s, line);
            }
        });
    }
    async answer(s, line) {
        let id = null;
        const reply = (body) => {
            if (!s.destroyed)
                s.write(JSON.stringify({ id, ...body }) + "\n");
        };
        try {
            const req = JSON.parse(line);
            id = req.id ?? null;
            if (!this.authorized(req.token)) {
                reply({ error: { code: "unauthorized", message: "wrong token" } });
                return s.destroy();
            }
            reply({ result: await this.call(String(req.method), (req.params ?? {})) });
        }
        catch (e) {
            const err = e instanceof BridgeError ? e : new BridgeError(e instanceof SyntaxError ? "bad-request" : "failed", e instanceof Error ? e.message : String(e));
            reply({ error: { code: err.code, message: err.message } });
        }
    }
    authorized(token) {
        if (typeof token !== "string" || token.length !== this.token.length)
            return false;
        return timingSafeEqual(Buffer.from(token), Buffer.from(this.token));
    }
    async call(method, p) {
        switch (method) {
            case "hello":
                return { protocol: PROTOCOL, version: this.opts.version, pid: process.pid, services: this.host.services(), features: ["options", "selections", "from", "outbox"] };
            case "options":
                return await this.host.options(id(p.sessionId, "sessionId"));
            case "state":
                return this.host.state(id(p.sessionId, "sessionId"));
            case "prompt": {
                const sessionId = id(p.sessionId, "sessionId");
                const requestId = id(p.requestId, "requestId");
                const text = typeof p.text === "string" ? p.text : "";
                if (text.trim() === "")
                    throw new BridgeError("bad-request", "the message is empty");
                if (text.length > MAX_TEXT)
                    throw new BridgeError("bad-request", `the message is ${text.length} characters; at most ${MAX_TEXT}`);
                const selections = selectionsOf(p);
                const said = fromOf(p);
                const ac = new AbortController();
                this.inflight.add(ac);
                const timer = setTimeout(() => ac.abort(), this.opts.promptTimeoutMs);
                try {
                    await Promise.race([
                        this.host.prompt(sessionId, requestId, said ? said + text : text, ac.signal, selections),
                        // A Host that ignores the signal still doesn't hold the caller.
                        new Promise((_, reject) => ac.signal.addEventListener("abort", () => reject(new BridgeError("timeout", "DSH did not accept the message in time")))),
                    ]);
                }
                finally {
                    clearTimeout(timer);
                    this.inflight.delete(ac);
                }
                return { accepted: true };
            }
            case "cancel":
                return { cancelled: this.host.cancel(id(p.sessionId, "sessionId")) };
            case "outbox": {
                const waitMs = Math.min(Math.max(Number(p.waitMs) || 0, 0), MAX_OUTBOX_WAIT);
                const ac = new AbortController();
                this.inflight.add(ac);
                try {
                    return { sends: await this.outbox.take(waitMs, ac.signal) };
                }
                finally {
                    this.inflight.delete(ac);
                }
            }
            case "outboxDone": {
                const e = p.error;
                const found = this.outbox.done(id(p.id, "id"), p.ok === true, { messageId: typeof p.messageId === "string" ? p.messageId : undefined, to: typeof p.to === "string" ? p.to : undefined }, {
                    code: typeof e?.code === "string" ? e.code : undefined,
                    message: typeof e?.message === "string" ? e.message.slice(0, 2000) : undefined,
                });
                return { found };
            }
            default:
                throw new BridgeError("bad-request", `no method ${method}`);
        }
    }
}
// selectionsOf reads a prompt's model and permission, refusing what isn't a plain name.
function selectionsOf(p) {
    const name = (v, what) => {
        if (typeof v !== "string" || v === "" || v.length > MAX_ID || /[\s]/.test(v))
            throw new BridgeError("bad-request", `bad ${what}`);
        return v;
    };
    const sel = {};
    if (p.model !== undefined && p.model !== null) {
        const m = p.model;
        if (typeof m !== "object")
            throw new BridgeError("bad-request", "bad model");
        sel.model = { provider: name(m.provider, "model provider"), model: name(m.model, "model"), ...(m.effort ? { effort: name(m.effort, "effort") } : {}) };
    }
    if (p.permission !== undefined && p.permission !== null && p.permission !== "")
        sel.permission = name(p.permission, "permission");
    return sel.model || sel.permission ? sel : undefined;
}
// fromOf is what the session's model reads before a message from an agent:
// who wrote it, through Aide, and how to answer; "" for the owner's.
function fromOf(p) {
    const f = p.from;
    if (f === undefined || f === null)
        return "";
    if (typeof f !== "object" || typeof f.address !== "string" || f.address === "" || f.address.length > MAX_ID || /\s/.test(f.address))
        throw new BridgeError("bad-request", "bad from");
    const name = typeof f.name === "string" ? f.name.replace(/\s+/g, " ").trim().slice(0, 100) : "";
    const mid = p.messageId === undefined ? "" : id(p.messageId, "messageId");
    const who = name ? `${name} (${f.address})` : f.address;
    return `[Aide] A message${mid ? ` (${mid})` : ""} from the agent ${who}, through the owner's Aide. To answer, call aide_message_send.\n\n`;
}
function id(v, name) {
    if (typeof v !== "string" || v === "" || v.length > MAX_ID || /[\s/\\]/.test(v))
        throw new BridgeError("bad-request", `bad ${name}`);
    return v;
}
