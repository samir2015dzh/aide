// What a taken-over DSH session sends through Aide: the session's model calls
// the aide_message_send tool this plugin registers in the Host, the send
// waits here, and aided, which polls the bridge for them (outbox), sends it
// as the machine, signed with the machine's key, and answers (outboxDone).
// The bridge only ever answers aided, so a send goes no further than this
// computer's Aide; Aide decides who it may go to (docs/agent-messages.md
// §15.3). Nothing here trusts the model about who it is: the session is the
// one DSH runs the tool for.

import { randomBytes } from "node:crypto";
import { BridgeError } from "./host.ts";

/** One send a session asked for, as aided takes it. */
export interface Send {
  id: string;
  sessionId: string;
  text: string;
  to?: string;
  replyTo?: string;
}

/** What aided made of a send. */
export interface Sent {
  messageId: string;
  to: string;
}

const MAX_TEXT = 100 * 1024;
const MAX_ADDRESS = 200;

export class Outbox {
  private readonly queue: Send[] = [];
  private readonly waiting = new Map<string, { resolve: (s: Sent) => void; reject: (e: Error) => void }>();
  private takers: (() => void)[] = [];
  private readonly sendTimeoutMs: number;

  constructor(sendTimeoutMs = 30_000) {
    this.sendTimeoutMs = sendTimeoutMs;
  }

  /** Queues a session's send and waits for aided's answer (at most sendTimeoutMs). */
  send(s: Omit<Send, "id">, signal?: AbortSignal): Promise<Sent> {
    const text = typeof s.text === "string" ? s.text : "";
    if (text.trim() === "") return Promise.reject(new BridgeError("bad-request", "the message is empty"));
    if (text.length > MAX_TEXT) return Promise.reject(new BridgeError("bad-request", `the message is ${text.length} characters; at most ${MAX_TEXT}`));
    for (const [k, v] of [["to", s.to], ["reply_to", s.replyTo]] as const) {
      if (v !== undefined && (typeof v !== "string" || v.length > MAX_ADDRESS || /\s/.test(v))) return Promise.reject(new BridgeError("bad-request", `bad ${k}`));
    }
    const send: Send = { id: "out_" + randomBytes(12).toString("hex"), sessionId: s.sessionId, text, ...(s.to ? { to: s.to } : {}), ...(s.replyTo ? { replyTo: s.replyTo } : {}) };
    return new Promise<Sent>((resolve, reject) => {
      const done = () => {
        clearTimeout(timer);
        signal?.removeEventListener("abort", abort);
        this.waiting.delete(send.id);
        const i = this.queue.indexOf(send);
        if (i >= 0) this.queue.splice(i, 1);
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
      for (const t of this.takers.splice(0)) t();
    });
  }

  /** The sends waiting for aided, once some are (at most waitMs). Each is handed out once. */
  async take(waitMs: number, signal?: AbortSignal): Promise<Send[]> {
    if (this.queue.length === 0 && waitMs > 0) {
      await new Promise<void>((resolve) => {
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
  done(id: string, ok: boolean, sent: Partial<Sent>, error?: { code?: string; message?: string }): boolean {
    const w = this.waiting.get(id);
    if (!w) return false;
    if (ok && typeof sent.messageId === "string" && typeof sent.to === "string") w.resolve({ messageId: sent.messageId, to: sent.to });
    else w.reject(new BridgeError(error?.code || "refused", error?.message || "Aide refused the message"));
    return true;
  }

  /** Fails every send still waiting (the plugin is going). */
  close() {
    for (const [id, w] of [...this.waiting]) {
      this.waiting.delete(id);
      w.reject(new BridgeError("unavailable", "the Aide plugin stopped"));
    }
    this.queue.length = 0;
    for (const t of this.takers.splice(0)) t();
  }
}

// The tool as DSH's tool registry takes it (a ToolDefinition: what
// @deepseek-ai/dsh-tools' defineTool returns), written out so the plugin
// needs nothing of DSH's at run time: parameters and output in the JSON
// Schema subset DSH enforces.

interface ToolAgent {
  id?: string;
  session?: { id?: string };
}

/** The tool a session's model calls to write through Aide. */
export function replyTool(outbox: Outbox) {
  return {
    name: "aide_message_send",
    description:
      "Send a message through Aide to one of the owner's agents (an agent such as Muse, not another session). " +
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
      render: (_args: unknown, value: { message_id: string; to: string }) => [{ type: "text", text: `Sent ${value.message_id} to ${value.to} through Aide.` }],
    },
    async execute(args: unknown, exec: { agent?: ToolAgent; signal?: AbortSignal }) {
      const sessionId = exec.agent?.id || exec.agent?.session?.id;
      if (!sessionId) throw new Error("aide_message_send runs in a session");
      const a = (args ?? {}) as { text?: unknown; to?: unknown; reply_to?: unknown };
      const sent = await outbox.send(
        { sessionId, text: String(a.text ?? ""), ...(typeof a.to === "string" && a.to ? { to: a.to } : {}), ...(typeof a.reply_to === "string" && a.reply_to ? { replyTo: a.reply_to } : {}) },
        exec.signal,
      );
      return { message_id: sent.messageId, to: sent.to };
    },
    presentCall: (args: unknown) => ({ card: "generic", title: "Send through Aide", kind: "other", rawInput: args }),
  };
}
