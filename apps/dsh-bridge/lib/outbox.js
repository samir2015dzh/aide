// What a taken-over DSH session sends through Aide: the session's model calls
// the aide_message_send tool this plugin registers in the Host, the send
// waits here, and aided, which polls the bridge for them (outbox), sends it
// as the machine, signed with the machine's key, and answers (outboxDone).
// The bridge only ever answers aided, so a send goes no further than this
// computer's Aide; Aide decides who it may go to (docs/agent-messages.md
// §15.3). Nothing here trusts the model about who it is: the session is the
// one DSH runs the tool for.
import { randomBytes } from "node:crypto";
import { BridgeError } from "./host.js";
const MAX_TEXT = 100 * 1024;
const MAX_ADDRESS = 200;
export class Outbox {
    queue = [];
    waiting = new Map();
    takers = [];
    sendTimeoutMs;
    constructor(sendTimeoutMs = 30_000) {
        this.sendTimeoutMs = sendTimeoutMs;
    }
    /** Queues a session's send and waits for aided's answer (at most sendTimeoutMs). */
    send(s, signal) {
        const text = typeof s.text === "string" ? s.text : "";
        if (text.trim() === "")
            return Promise.reject(new BridgeError("bad-request", "the message is empty"));
        if (text.length > MAX_TEXT)
            return Promise.reject(new BridgeError("bad-request", `the message is ${text.length} characters; at most ${MAX_TEXT}`));
        for (const [k, v] of [["to", s.to], ["reply_to", s.replyTo]]) {
            if (v !== undefined && (typeof v !== "string" || v.length > MAX_ADDRESS || /\s/.test(v)))
                return Promise.reject(new BridgeError("bad-request", `bad ${k}`));
        }
        const send = { id: "out_" + randomBytes(12).toString("hex"), sessionId: s.sessionId, text, ...(s.to ? { to: s.to } : {}), ...(s.replyTo ? { replyTo: s.replyTo } : {}) };
        return new Promise((resolve, reject) => {
            const done = () => {
                clearTimeout(timer);
                signal?.removeEventListener("abort", abort);
                this.waiting.delete(send.id);
                const i = this.queue.indexOf(send);
                if (i >= 0)
                    this.queue.splice(i, 1);
            };
            const timer = setTimeout(() => {
                done();
                reject(new BridgeError("timeout", "Aide on this computer did not take the message: is it running?"));
            }, this.sendTimeoutMs);
            const abort = () => {
                done();
                reject(new BridgeError("timeout", "cancelled"));
            };
            signal?.addEventListener("abort", abort);
            this.waiting.set(send.id, {
                resolve: (r) => {
                    done();
                    resolve(r);
                },
                reject: (e) => {
                    done();
                    reject(e);
                },
            });
            this.queue.push(send);
            for (const t of this.takers.splice(0))
                t();
        });
    }
    /** The sends waiting for aided, once some are (at most waitMs). Each is handed out once. */
    async take(waitMs, signal) {
        if (this.queue.length === 0 && waitMs > 0) {
            await new Promise((resolve) => {
                const t = setTimeout(finish, waitMs);
                function finish() {
                    clearTimeout(t);
                    signal?.removeEventListener("abort", finish);
                    resolve();
                }
                signal?.addEventListener("abort", finish);
                this.takers.push(finish);
            });
        }
        return this.queue.splice(0);
    }
    /** aided's answer to a send: sent, or refused with why. */
    done(id, ok, sent, error) {
        const w = this.waiting.get(id);
        if (!w)
            return false;
        if (ok && typeof sent.messageId === "string" && typeof sent.to === "string")
            w.resolve({ messageId: sent.messageId, to: sent.to });
        else
            w.reject(new BridgeError(error?.code || "refused", error?.message || "Aide refused the message"));
        return true;
    }
    /** Fails every send still waiting (the plugin is going). */
    close() {
        for (const [id, w] of [...this.waiting]) {
            this.waiting.delete(id);
            w.reject(new BridgeError("unavailable", "the Aide plugin stopped"));
        }
        this.queue.length = 0;
        for (const t of this.takers.splice(0))
            t();
    }
}
/** The tool a session's model calls to write through Aide. */
export function replyTool(outbox) {
    return {
        name: "aide_message_send",
        description: "Send a message through Aide to one of the owner's agents (an agent such as Muse, not another session). " +
            "Without `to`, it answers the agent that last wrote to this session through Aide. " +
            "Works only in a session the owner has taken over in the Aide app; the owner's Aide sends it, signed as this computer.",
        parameters: {
            type: "object",
            additionalProperties: false,
            properties: {
                text: { type: "string", description: "The message." },
                to: { type: "string", description: "The agent: its name or address (c_…). Leave it out to answer the agent that last wrote here." },
                reply_to: { type: "string", description: "The id (msg_…) of the message this answers. Leave it out to answer the last one that came through Aide." },
            },
            required: ["text"],
        },
        output: {
            schema: {
                type: "object",
                additionalProperties: false,
                properties: { message_id: { type: "string" }, to: { type: "string" } },
                required: ["message_id", "to"],
            },
            render: (_args, value) => [{ type: "text", text: `Sent ${value.message_id} to ${value.to} through Aide.` }],
        },
        async execute(args, exec) {
            const sessionId = exec.agent?.id || exec.agent?.session?.id;
            if (!sessionId)
                throw new Error("aide_message_send runs in a session");
            const a = (args ?? {});
            const sent = await outbox.send({ sessionId, text: String(a.text ?? ""), ...(typeof a.to === "string" && a.to ? { to: a.to } : {}), ...(typeof a.reply_to === "string" && a.reply_to ? { replyTo: a.reply_to } : {}) }, exec.signal);
            return { message_id: sent.messageId, to: sent.to };
        },
        presentCall: (args) => ({ card: "generic", title: "Send through Aide", kind: "other", rawInput: args }),
    };
}
