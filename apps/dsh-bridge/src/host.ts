// What the bridge asks of the DSH Host it runs in, through the services DSH
// itself exposes in-process (the same ones Agents Anywhere's bridge calls):
// the session controller's prompt, in queue mode, and the live Agent's
// cancel. Nothing here reads or writes DSH's files, and nothing takes a
// session's write lock behind DSH's back: a prompt goes to the Host that
// owns the session, as one typed in its own window would.

/** The parts of a Cordis context the bridge uses. */
export interface DshContext {
  get(name: string): unknown;
}

interface SessionController {
  prompt(request: { sessionId: string; requestId: string; mode: "queue" | "steer"; content: { type: "text"; text: string }[] }, signal: AbortSignal): Promise<unknown>;
}

interface Agent {
  status?: string;
  session?: unknown;
  cancel(reason: { kind: "user" }): unknown;
}

interface Agents {
  get(id: string): Agent | undefined;
}

interface Sessions {
  flush?(session: unknown): Promise<unknown>;
}

/** A refusal the bridge reports with its code (BridgeError on the wire). */
export class BridgeError extends Error {
  readonly code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}

/** What Aide asks: the Host's own view of one session, a prompt, a cancel. */
export interface Host {
  /** Which services the Host has (a profile without them can't take prompts). */
  services(): { sessionController: boolean; agents: boolean };
  /** live: the session's Agent runs in this Host (it holds the session); status: its own word for it. */
  state(sessionId: string): { live: boolean; status: string };
  /** Queues text as the owner's next message in the session (a new turn when idle). */
  prompt(sessionId: string, requestId: string, text: string, signal: AbortSignal): Promise<void>;
  /** Stops the session's current turn; false when no Agent runs it here. */
  cancel(sessionId: string): boolean;
}

/** The Host as DSH's own services give it. */
export class DshHost implements Host {
  private readonly ctx: DshContext;
  constructor(ctx: DshContext) {
    this.ctx = ctx;
  }

  private controller(): SessionController | undefined {
    const c = this.ctx.get("sessionController") as SessionController | undefined;
    return typeof c?.prompt === "function" ? c : undefined;
  }

  private agents(): Agents | undefined {
    const a = this.ctx.get("agents") as Agents | undefined;
    return typeof a?.get === "function" ? a : undefined;
  }

  services() {
    return { sessionController: this.controller() !== undefined, agents: this.agents() !== undefined };
  }

  state(sessionId: string) {
    const agent = this.agents()?.get(sessionId);
    return { live: agent !== undefined, status: agent?.status ?? "" };
  }

  async prompt(sessionId: string, requestId: string, text: string, signal: AbortSignal) {
    const controller = this.controller();
    if (!controller) throw new BridgeError("unavailable", "this DSH has no session controller (a profile without the Web/Desktop surface)");
    try {
      await controller.prompt({ sessionId, requestId, mode: "queue", content: [{ type: "text", text }] }, signal);
    } catch (e) {
      if (signal.aborted) throw new BridgeError("timeout", "DSH did not accept the message in time");
      throw new BridgeError(dshCode(e), dshMessage(e));
    }
    // Written through now, as DSH's own window does after a prompt.
    const agent = this.agents()?.get(sessionId);
    const sessions = this.ctx.get("sessions") as Sessions | undefined;
    if (agent?.session !== undefined && typeof sessions?.flush === "function") {
      await sessions.flush(agent.session).catch(() => {});
    }
  }

  cancel(sessionId: string) {
    const agent = this.agents()?.get(sessionId);
    if (!agent) return false;
    agent.cancel({ kind: "user" });
    return true;
  }
}

// DSH's remote errors carry a code such as session/agent-busy or
// session/not-found; anything else is just "failed".
function dshCode(e: unknown): string {
  const code = typeof e === "object" && e !== null ? (e as { code?: unknown }).code : undefined;
  if (typeof code !== "string") return "failed";
  if (code.endsWith("not-found")) return "not-found";
  if (code.endsWith("agent-busy")) return "busy";
  if (code.endsWith("writer-held") || code.endsWith("owned")) return "held";
  return "failed";
}

function dshMessage(e: unknown): string {
  const m = e instanceof Error ? e.message : String(e);
  const code = typeof e === "object" && e !== null ? (e as { code?: unknown }).code : undefined;
  return typeof code === "string" && !m.includes(code) ? `${code}: ${m}` : m;
}
