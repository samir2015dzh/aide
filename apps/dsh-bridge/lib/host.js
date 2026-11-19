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
    async prompt(sessionId, requestId, text, signal) {
        const controller = this.controller();
        if (!controller)
            throw new BridgeError("unavailable", "this DSH has no session controller (a profile without the Web/Desktop surface)");
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
