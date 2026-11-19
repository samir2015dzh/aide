// @hermesh/dsh-bridge: a DSH plugin (Cordis) that lets this computer's
// Hermesh take over DSH sessions: hermeshd asks it, over a local endpoint
// only this user can reach, to queue the owner's message into a session or
// stop its turn, and the Host that owns the session does it, as its own
// window would. Turned off or removed in DSH's Plugins page (or by
// hermeshd), it leaves nothing running.
import { homedir } from "node:os";
import { isAbsolute, join } from "node:path";
import { DshHost } from "./host.js";
import { BridgeServer } from "./server.js";
export const name = "hermesh-dsh-bridge";
export const VERSION = "0.1.0";
/** Where the bridge meets hermeshd: <DSH_HOME>/hermesh-bridge, as hermeshd finds DSH's home. */
export function bridgeHome(env = process.env) {
    let dsh = env.DSH_HOME?.trim() ?? "";
    if (dsh === "~" || dsh.startsWith("~/") || dsh.startsWith("~\\"))
        dsh = join(homedir(), dsh.slice(1));
    if (dsh === "" || !isAbsolute(dsh))
        dsh = join(homedir(), ".dsh");
    return join(dsh, "hermesh-bridge");
}
export function apply(ctx) {
    const server = new BridgeServer(new DshHost(ctx), { home: bridgeHome(), version: VERSION });
    // A bridge that cannot start must not take the Host down with it.
    const started = server.start().catch((e) => {
        console.warn(`[hermesh-dsh-bridge] not started: ${e instanceof Error ? e.message : String(e)}`);
    });
    ctx.effect(() => async () => {
        await started;
        await server.close();
    }, "hermesh-dsh-bridge.close");
}
