// What a coding agent that is not installed says when clicked (工单 #180 A): no row of the "not installed" list is
// one that does nothing. One with a Connect guide line opens it; any other says, in place, what it is, which command
// the machine looks for (as aided's acp.Agents), its official page, and its entry in the official ACP registry, which
// Aide can install as one of the machine's own agents.

export type MissingAgent = {
  /** The command a new terminal's PATH must have (aided's acp.Agents: its Command). */
  command: string;
  /** Its desktop app, which alone is not enough (aided's acp.Agents: its DesktopApps). */
  app?: string;
  /** Its official page (the official ACP registry's website, else its repository). */
  site: string;
  /** Its id in the official ACP registry. */
  registryId: string;
};

export const missingAgents: Record<string, MissingAgent> = {
  gemini: { command: "gemini", site: "https://geminicli.com", registryId: "gemini" },
  cursor: { command: "cursor-agent", app: "Cursor", site: "https://cursor.com/docs/cli/acp", registryId: "cursor" },
  copilot: { command: "copilot", site: "https://github.com/features/copilot/cli/", registryId: "github-copilot-cli" },
  opencode: { command: "opencode", site: "https://opencode.ai", registryId: "opencode" },
  qwen: { command: "qwen", site: "https://qwenlm.github.io/qwen-code-docs/en/users/overview", registryId: "qwen-code" },
  goose: { command: "goose", site: "https://block.github.io/goose/", registryId: "goose" },
  auggie: { command: "auggie", site: "https://www.augmentcode.com/", registryId: "auggie" },
  kimi: { command: "kimi", site: "https://moonshotai.github.io/kimi-cli/", registryId: "kimi" },
  kiro: { command: "kiro-cli", app: "Kiro", site: "https://kiro.dev/docs/cli/acp/", registryId: "kiro" },
  qoder: { command: "qodercli", app: "Qoder", site: "https://qoder.com", registryId: "qoder" },
  codebuddy: { command: "codebuddy", site: "https://www.codebuddy.cn/cli/", registryId: "codebuddy-code" },
  grok: { command: "grok", site: "https://x.ai/cli", registryId: "grok-build" },
};

/** What clicking a not-installed agent does: open its Connect guide line (one of guideLines), or say in place what to do. */
export function missingAction(name: string, guideLines: readonly string[]): { guide: string } | { info: MissingAgent } | null {
  if (guideLines.includes(name)) return { guide: name };
  const info = missingAgents[name];
  return info ? { info } : null;
}
