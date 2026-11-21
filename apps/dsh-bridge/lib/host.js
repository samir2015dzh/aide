// What the bridge asks of the DSH Host it runs in, through the services DSH
// itself exposes in-process (the same ones Agents Anywhere's bridge calls):
// the session controller's prompt, in queue mode, and the live Agent's
// cancel. Nothing here reads or writes DSH's files, and nothing takes a
// session's write lock behind DSH's back: a prompt goes to the Host that
// owns the session, as one typed in its own window would.
/** A refusal the bridge reports with its code (BridgeError on the wire). */
export class BridgeError extends Error {
    code;
    constructor(code, message) {
        super(message);
        this.code = code;
    }
}
/** The Host as DSH's own services give it. */
export class DshHost {
    ctx;
    constructor(ctx) {
        this.ctx = ctx;
    }
    controller() {
        const c = this.ctx.get("sessionController");
        return typeof c?.prompt === "function" ? c : undefined;
    }
    agents() {
        const a = this.ctx.get("agents");
        return typeof a?.get === "function" ? a : undefined;
    }
    services() {
        return { sessionController: this.controller() !== undefined, agents: this.agents() !== undefined };
    }
    state(sessionId) {
        const agent = this.agents()?.get(sessionId);
        return { live: agent !== undefined, status: agent?.status ?? "" };
    }
    get(name) {
        return (this.ctx.get(name) ?? undefined);
    }
    async options(sessionId) {
        const llm = this.get("llm");
        const presets = this.get("permissionPresets");
        const models = [];
        for (const p of llm?.listProviders() ?? []) {
            let list = [];
            try {
                list = await llm.listModels(p.id);
            }
            catch {
                continue; // a provider DSH can't list now (no key, offline): left out, as DSH's own picker does
            }
            for (const m of list) {
                let efforts = [];
                try {
                    efforts = ((await llm.resolveModelInfo(p.id, m.id)).reasoning?.efforts ?? []).map((e) => e.id);
                }
                catch {
                    // its efforts not known: the model's default only
                }
                models.push({ provider: p.id, providerName: p.name || p.id, model: m.id, modelName: m.name || m.id, efforts });
            }
        }
        const permissions = (presets?.catalog().options ?? [])
            .filter((o) => o.value !== "custom")
            .map((o) => ({ id: o.value, name: o.name || o.value, description: o.description ?? "" }));
        const current = {};
        const agent = this.agents()?.get(sessionId);
        if (agent?.session !== undefined) {
            const next = this.get("sessionProjections")?.snapshot(agent.session, ["modelSelection"]).values.modelSelection?.next;
            if (next)
                current.model = { provider: next.provider, model: next.model, effort: next.reasoningEffort ?? "" };
            const perm = presets?.current(agent.session);
            if (perm)
                current.permission = perm;
        }
        return { models, permissions, current };
    }
    /** Sets the model and permission preset selections name on the session, as DSH's own pickers do (and Agents Anywhere). */
    async select(sessionId, sel, signal) {
        const controller = this.controller();
        if (typeof controller.resolveAgent !== "function")
            throw new BridgeError("unavailable", "this DSH can't set a session's model or permissions from a plugin");
        const found = await controller.resolveAgent(sessionId);
        if ("error" in found)
            throw new BridgeError("not-found", "DSH could not open this session to set its model or permissions");
        const agent = found.agent;
        signal.throwIfAborted();
        if (sel.model) {
            const llm = this.get("llm");
            if (!llm || typeof controller.selectModel !== "function")
                throw new BridgeError("unavailable", "this DSH has no model service a plugin can use");
            const want = { provider: sel.model.provider, model: sel.model.model, ...(sel.model.effort ? { reasoningEffort: sel.model.effort } : {}) };
            try {
                await llm.resolveCallConfig(want);
            }
            catch {
                throw new BridgeError("bad-request", `DSH does not offer ${sel.model.provider}/${sel.model.model}${sel.model.effort ? ` (${sel.model.effort})` : ""}`);
            }
            const now = this.get("sessionProjections")?.snapshot(agent.session, ["modelSelection"]).values.modelSelection?.next;
            if (!now || now.provider !== want.provider || now.model !== want.model || (now.reasoningEffort ?? "") !== (sel.model.effort ?? "")) {
                await controller.selectModel({ sessionId, ...want });
            }
        }
        if (sel.permission) {
            const presets = this.get("permissionPresets");
            if (!presets || !presets.names.includes(sel.permission) || sel.permission === "custom")
                throw new BridgeError("bad-request", `DSH has no permission preset ${sel.permission}`);
            if (presets.current(agent.session) !== sel.permission) {
                const commands = this.get("commands");
                if (!commands?.find(agent, "permission"))
                    throw new BridgeError("unavailable", "this session has no /permission command");
                const r = await commands.execute(agent, `/permission ${sel.permission}`, [], signal);
                if (r?.result.kind !== "success")
                    throw new BridgeError("failed", `DSH did not switch the permission preset to ${sel.permission}`);
            }
        }
    }
    async prompt(sessionId, requestId, text, signal, selections) {
        const controller = this.controller();
        if (!controller)
            throw new BridgeError("unavailable", "this DSH has no session controller (a profile without the Web/Desktop surface)");
        if (selections?.model || selections?.permission) {
            try {
                await this.select(sessionId, selections, signal);
            }
            catch (e) {
                if (signal.aborted)
                    throw new BridgeError("timeout", "DSH did not take the model or permissions in time");
                throw e instanceof BridgeError ? e : new BridgeError(dshCode(e), dshMessage(e));
            }
        }
        try {
            await controller.prompt({ sessionId, requestId, mode: "queue", content: [{ type: "text", text }] }, signal);
        }
        catch (e) {
            if (signal.aborted)
                throw new BridgeError("timeout", "DSH did not accept the message in time");
            throw new BridgeError(dshCode(e), dshMessage(e));
        }
        // Written through now, as DSH's own window does after a prompt.
        const agent = this.agents()?.get(sessionId);
        const sessions = this.ctx.get("sessions");
        if (agent?.session !== undefined && typeof sessions?.flush === "function") {
            await sessions.flush(agent.session).catch(() => { });
        }
    }
    cancel(sessionId) {
        const agent = this.agents()?.get(sessionId);
        if (!agent)
            return false;
        agent.cancel({ kind: "user" });
        return true;
    }
}
// DSH's remote errors carry a code such as session/agent-busy or
// session/not-found; anything else is just "failed".
function dshCode(e) {
    const code = typeof e === "object" && e !== null ? e.code : undefined;
    if (typeof code !== "string")
        return "failed";
    if (code.endsWith("not-found"))
        return "not-found";
    if (code.endsWith("agent-busy"))
        return "busy";
    if (code.endsWith("writer-held") || code.endsWith("owned"))
        return "held";
    return "failed";
}
function dshMessage(e) {
    const m = e instanceof Error ? e.message : String(e);
    const code = typeof e === "object" && e !== null ? e.code : undefined;
    return typeof code === "string" && !m.includes(code) ? `${code}: ${m}` : m;
}
