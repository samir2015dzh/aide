// Whether the members picked for a room can take their turn (工单 #159): the parts of the new-room sheet that decide,
// without React.

// RoomMemberProblem's values (proto/aide/machine/v1/machine.proto), by number, as their words' keys under rooms.reach:
// the enum isn't imported, so that node --test loads this file as the other page logic.
const problems: Record<number, string> = {
  1: "noGrant",
  2: "notContact",
  3: "noInbox",
  4: "notTaking",
  5: "relayUnreachable",
  6: "machineOffline",
  7: "machineOutdated",
  8: "agentMissing",
  9: "agentSignedOut",
  10: "agentUnconfirmed",
  11: "agentPaused",
};

/** What a member's check (machine.v1 RoomMemberCheck) says, as read here. */
type Check = { botKey?: string; problem: number; lastSeen?: unknown };

/** The key (under rooms.reach) of why a member may not take its turn; "" when it can. */
export function reachProblem(c: Check): string {
  if (!c.problem) return "";
  const p = problems[c.problem] ?? "unknown";
  return p === "notTaking" && c.lastSeen ? "notTakingSeen" : p;
}

/**
 * The members that may not take their turn, as one key: the owner is told of them and confirms that set before the
 * room is made; another set (a member picked or dropped, a check that says otherwise) asks again.
 */
export function unreachableKey(checks: Check[]): string {
  return checks
    .filter((c) => reachProblem(c) !== "")
    .map((c) => c.botKey ?? "")
    .sort()
    .join(" ");
}
