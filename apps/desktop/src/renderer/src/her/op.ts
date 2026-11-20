import type { Op } from "@aide/her-protocol";

/** How a page reaches its assistant's conversation: session/op, true when it went. */
export type SendOp = (op: Op) => Promise<boolean>;
