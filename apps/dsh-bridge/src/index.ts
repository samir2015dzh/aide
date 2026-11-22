// @aide/dsh-bridge: a DSH plugin (Cordis) that lets this computer's
// Aide take over DSH sessions: aided asks it, over a local endpoint
// only this user can reach, to queue the owner's message into a session or
// stop its turn, and the Host that owns the session does it, as its own
// window would. Turned off or removed in DSH's Plugins page (or by
// aided), it leaves nothing running.

import { homedir } from "node:os";
import { isAbsolute, join } from "node:path";
import { DshHost, type DshContext } from "./host.ts";
import { replyTool } from "./outbox.ts";
import { BridgeServer } from "./server.ts";

export const name = "aide-dsh-bridge";
export const VERSION = "0.3.0";

interface Tools {
  register(definition: unknown): () => void;
}

interface Context extends DshContext {
  effect(execute: () => () => Promise<void> | void, label?: string): unknown;
}

/** Where the bridge meets aided: <DSH_HOME>/aide-bridge, as aided finds DSH's home. */
export function bridgeHome(env: NodeJS.ProcessEnv = process.env): string {
  let dsh = env.DSH_HOME?.trim() ?? "";
  if (dsh === "~" || dsh.startsWith("~/") || dsh.startsWith("~\\")) dsh = join(homedir(), dsh.slice(1));
  if (dsh === "" || !isAbsolute(dsh)) dsh = join(homedir(), ".dsh");
  return join(dsh, "aide-bridge");
}

export function apply(ctx: Context) {
  const server = new BridgeServer(new DshHost(ctx), { home: bridgeHome(), version: VERSION });
  // A bridge that cannot start must not take the Host down with it.
  const started = server.start().catch((e: unknown) => {
    console.warn(`[aide-dsh-bridge] not started: ${e instanceof Error ? e.message : String(e)}`);
  });
  // The tool a taken-over session answers with (aide_message_send), where
  // this DSH has a tool registry: Aide refuses it for any other session.
  let unregister: (() => void) | undefined;
  const tools = ctx.get("tools") as Tools | undefined;
  if (typeof tools?.register === "function") {
    try {
      unregister = tools.register(replyTool(server.outbox));
    } catch (e) {
      console.warn(`[aide-dsh-bridge] aide_message_send not registered: ${e instanceof Error ? e.message : String(e)}`);
    }
  }
  ctx.effect(
    () => async () => {
      unregister?.();
      await started;
      await server.close();
    },
    "aide-dsh-bridge.close",
  );
}
