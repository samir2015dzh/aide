// The steps of connecting an assistant (connect.tsx), apart from React so that they can be tested.

/** The assistants that connect by the server's MCP address rather than the CLI. */
export const byMCP = new Set(["claude", "chatgpt"]);

/**
 * The server's MCP address as this device knows it: "ready" with one; "offline" while this device is
 * not connected to the server (it learns the address when it connects); "older" when the server is
 * older than the address (relayOlderThanMcp: it doesn't know the call, and updating it gives one);
 * "off" when the server gives none because its MCP is off. An older core says neither, and its empty
 * address shows as "off" with the update named too.
 */
export type MCPState = "ready" | "offline" | "older" | "off";

export function mcpState(status: { relayConnected?: boolean; mcpUrl?: string; relayOlderThanMcp?: boolean } | null | undefined): MCPState {
  if (status?.mcpUrl) return "ready";
  if (!status?.relayConnected) return "offline";
  return status.relayOlderThanMcp ? "older" : "off";
}

/**
 * What the line shows for an assistant: its own steps (connect.json assistants.steps.<kind>, each
 * assistant's own menus, never another's), whether there is something to copy, and the note in its
 * place when there isn't. An MCP assistant without an address has no steps to paste one.
 */
export function assistantSteps(kind: string, state: MCPState): { steps: string[]; copy: boolean; note: null | "mcpOlder" | "mcpOff" | "mcpOffline" } {
  if (!byMCP.has(kind)) return { steps: ["cli1", "cli2", "cli3"], copy: true, note: null };
  if (state !== "ready") return { steps: [], copy: false, note: state === "offline" ? "mcpOffline" : state === "older" ? "mcpOlder" : "mcpOff" };
  return { steps: [`steps.${kind}`, "mcp2", "mcp3"], copy: true, note: null };
}

/** The app an MCP assistant is added in, as its steps name it. */
export const mcpApp: Record<string, string> = { claude: "Claude Desktop", chatgpt: "ChatGPT" };
