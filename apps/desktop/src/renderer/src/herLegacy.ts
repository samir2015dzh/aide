import type { Machine } from "@aide/proto/local";

// Her is no longer part of Aide. A machine that ran her once may still hold her data (her assistants,
// conversations and settings), and says so in its status: removing the machine, or its device, still
// offers to erase it there.

/**
 * Whether Her's data is on the machine, as its status says while it answers (an offline machine can't
 * erase it). A machine whose Aide still has Her counts while she is on there, whatever it says of her data.
 */
export function herDataLeft(m: Pick<Machine, "status">): boolean {
  const her = m.status?.her;
  return !!her && (her.enabled || her.data);
}
