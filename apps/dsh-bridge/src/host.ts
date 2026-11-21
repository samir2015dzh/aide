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
  resolveAgent?(sessionId: string): Promise<{ agent: Agent } | { error: unknown }>;
  selectModel?(request: { sessionId: string; provider: string; model: string; reasoningEffort?: string }): Promise<unknown>;
}

interface Agent {
  id?: string;
  status?: string;
  session?: unknown;
  cancel(reason: { kind: "user" }): unknown;
}

// DSH's model service, permission presets, commands and projections, as Agents Anywhere's bridge uses them.
interface Llm {
  listProviders(): { id: string; name?: string }[];
  listModels(provider: string): Promise<{ id: string; name?: string }[]>;
  resolveModelInfo(provider: string, model: string): Promise<{ reasoning?: { efforts?: { id: string; name?: string }[] } }>;
  resolveCallConfig(selection: { provider: string; model: string; reasoningEffort?: string }): Promise<unknown>;
}

interface PermissionPresets {
  catalog(): { options: { value: string; name: string; description?: string }[] };
  names: readonly string[];
  current(session: unknown): string | undefined;
}

interface Commands {
  find(agent: Agent, name: string): unknown;
  execute(agent: Agent, line: string, args: unknown[], signal: AbortSignal): Promise<{ result: { kind: string } } | undefined>;
}

interface Projections {
  snapshot(session: unknown, keys: string[]): { values: { modelSelection?: { next?: Model & { reasoningEffort?: string } } } };
}

/** A model as DSH names it; effort "" for the model's default. */
export interface Model {
  provider: string;
  model: string;
  effort?: string;
}

/** What the session goes on with, set before a message (unset: as it is). */
export interface Selections {
  model?: Model;
  permission?: string;
}

/** The models and permission presets DSH offers, and what the session has now. */
export interface SessionOptions {
  models: { provider: string; providerName: string; model: string; modelName: string; efforts: string[] }[];
  permissions: { id: string; name: string; description: string }[];
  current: { model?: Model; permission?: string };
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
  /** Queues text as the next message in the session (a new turn when idle), after setting what selections name. */
  prompt(sessionId: string, requestId: string, text: string, signal: AbortSignal, selections?: Selections): Promise<void>;
  /** The models and permission presets the session may be set to, and its own. */
  options(sessionId: string): Promise<SessionOptions>;
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

  private get<T>(name: string): T | undefined {
    return (this.ctx.get(name) ?? undefined) as T | undefined;
  }

  async options(sessionId: string): Promise<SessionOptions> {
    const llm = this.get<Llm>("llm");
    const presets = this.get<PermissionPresets>("permissionPresets");
    const models: SessionOptions["models"] = [];
    for (const p of llm?.listProviders() ?? []) {
      let list: { id: string; name?: string }[] = [];
      try {
        list = await llm!.listModels(p.id);
      } catch {
        continue; // a provider DSH can't list now (no key, offline): left out, as DSH's own picker does
      }
      for (const m of list) {
        let efforts: string[] = [];
        try {
          efforts = ((await llm!.resolveModelInfo(p.id, m.id)).reasoning?.efforts ?? []).map((e) => e.id);
        } catch {
          // its efforts not known: the model's default only
        }
        models.push({ provider: p.id, providerName: p.name || p.id, model: m.id, modelName: m.name || m.id, efforts });
      }
    }
    const permissions = (presets?.catalog().options ?? [])
      .filter((o) => o.value !== "custom")
      .map((o) => ({ id: o.value, name: o.name || o.value, description: o.description ?? "" }));
    const current: SessionOptions["current"] = {};
    const agent = this.agents()?.get(sessionId);
    if (agent?.session !== undefined) {
      const next = this.get<Projections>("sessionProjections")?.snapshot(agent.session, ["modelSelection"]).values.modelSelection?.next;
      if (next) current.model = { provider: next.provider, model: next.model, effort: next.reasoningEffort ?? "" };
      const perm = presets?.current(agent.session);
      if (perm) current.permission = perm;
    }
    return { models, permissions, current };
  }

  /** Sets the model and permission preset selections name on the session, as DSH's own pickers do (and Agents Anywhere). */
  private async select(sessionId: string, sel: Selections, signal: AbortSignal) {
    const controller = this.controller()!;
    if (typeof controller.resolveAgent !== "function") throw new BridgeError("unavailable", "this DSH can't set a session's model or permissions from a plugin");
    const found = await controller.resolveAgent(sessionId);
    if ("error" in found) throw new BridgeError("not-found", "DSH could not open this session to set its model or permissions");
    const agent = found.agent;
    signal.throwIfAborted();
    if (sel.model) {
      const llm = this.get<Llm>("llm");
      if (!llm || typeof controller.selectModel !== "function") throw new BridgeError("unavailable", "this DSH has no model service a plugin can use");
      const want = { provider: sel.model.provider, model: sel.model.model, ...(sel.model.effort ? { reasoningEffort: sel.model.effort } : {}) };
      try {
        await llm.resolveCallConfig(want);
      } catch {
        throw new BridgeError("bad-request", `DSH does not offer ${sel.model.provider}/${sel.model.model}${sel.model.effort ? ` (${sel.model.effort})` : ""}`);
      }
      const now = this.get<Projections>("sessionProjections")?.snapshot(agent.session, ["modelSelection"]).values.modelSelection?.next;
      if (!now || now.provider !== want.provider || now.model !== want.model || (now.reasoningEffort ?? "") !== (sel.model.effort ?? "")) {
        await controller.selectModel({ sessionId, ...want });
      }
    }
    if (sel.permission) {
      const presets = this.get<PermissionPresets>("permissionPresets");
      if (!presets || !presets.names.includes(sel.permission) || sel.permission === "custom") throw new BridgeError("bad-request", `DSH has no permission preset ${sel.permission}`);
      if (presets.current(agent.session) !== sel.permission) {
        const commands = this.get<Commands>("commands");
        if (!commands?.find(agent, "permission")) throw new BridgeError("unavailable", "this session has no /permission command");
        const r = await commands.execute(agent, `/permission ${sel.permission}`, [], signal);
        if (r?.result.kind !== "success") throw new BridgeError("failed", `DSH did not switch the permission preset to ${sel.permission}`);
      }
    }
  }

  async prompt(sessionId: string, requestId: string, text: string, signal: AbortSignal, selections?: Selections) {
    const controller = this.controller();
    if (!controller) throw new BridgeError("unavailable", "this DSH has no session controller (a profile without the Web/Desktop surface)");
    if (selections?.model || selections?.permission) {
      try {
        await this.select(sessionId, selections, signal);
      } catch (e) {
        if (signal.aborted) throw new BridgeError("timeout", "DSH did not take the model or permissions in time");
        throw e instanceof BridgeError ? e : new BridgeError(dshCode(e), dshMessage(e));
      }
    }
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
