import type { AgentStatus } from "@aide/proto/machine";

const osTitles: Record<string, string> = { darwin: "macOS", macos: "macOS", linux: "Linux", windows: "Windows", ios: "iOS", android: "Android" };

/** A machine's "darwin/arm64" as "macOS · arm64"; a trusted device's "macos" as "macOS". */
export function platformTitle(platform: string): string {
  const [os, arch] = platform.split("/");
  return [osTitles[os] ?? os, arch].filter(Boolean).join(" · ");
}

/**
 * Whether path is absolute on a machine of this platform ("windows/amd64":
 * a drive, C:\…, or a share, \\server\share; elsewhere /…). "~" counts as
 * absolute: the machine starts it at its user's home.
 */
export function isAbsolutePath(path: string, platform: string): boolean {
  if (path === "~" || /^~[\\/]/.test(path)) return true;
  if (platform.split("/")[0] === "windows") return /^[A-Za-z]:[\\/]/.test(path) || /^\\\\[^\\]+\\[^\\]+/.test(path);
  return path.startsWith("/");
}

/** A path's last folder, on a machine of any platform ("/" or "\"). */
export function folderName(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).pop() ?? "";
}

/** The coding agents Aide helps install: shown even when missing. */
const suggestedAgents = ["claude", "codex"];

/**
 * A machine's coding agents as tiles: ready ones first, then installed but
 * not logged in, then the suggested ones that are missing; by name within each.
 */
export function agentTiles(agents: AgentStatus[]): AgentStatus[] {
  const rank = (a: AgentStatus) => (a.available && (a.loggedIn || a.loginUnconfirmed) ? 0 : a.available ? 1 : 2);
  return agents.filter((a) => a.available || suggestedAgents.includes(a.name)).sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

/**
 * A model as people call it: the name its agent gives it (Codex's display
 * names, Claude Code's aliases), else a Claude model's ID read out
 * ("claude-opus-5-5" → "Opus 5.5"), else the ID itself.
 */
export function modelTitle(id: string, models: AgentStatus["models"] = []): string {
  const m = models.find((x) => x.id === id);
  if (m?.resolved) return modelTitle(m.resolved); // an alias: the model it runs here (工单 #99)
  if (m?.name) return m.name;
  const c = /^claude-([a-z]+)-(\d+)(?:-(\d{1,2}))?(?:-\d{8})?(\[1m\])?$/.exec(id);
  if (!c) return id;
  return `${c[1][0].toUpperCase()}${c[1].slice(1)} ${c[2]}${c[3] ? `.${c[3]}` : ""}${c[4] ? " (1M)" : ""}`;
}

type AgentModel = AgentStatus["models"][number];

/**
 * A model in a model list (工单 #99): what to pick (value), the specific model as people call it (title, "Opus 5.5")
 * and in full (detail, "claude-opus-5-5"). unknown: an alias whose specific model its agent did not say here
 * (a Claude Code too old to list them), so all there is to show is its family.
 */
export type ModelChoice = { value: string; title: string; detail: string; unknown: boolean };

/** A model ID in full as it reads: DSH's ["provider","model"] as provider/model, any other as it is. */
function idText(id: string): string {
  if (!id.startsWith("[")) return id;
  try {
    const v: unknown = JSON.parse(id);
    return Array.isArray(v) && v.every((x) => typeof x === "string") ? v.join("/") : id;
  } catch {
    return id;
  }
}

function choiceOf(agent: string, m: AgentModel): ModelChoice {
  if (m.resolved) return { value: m.id, title: modelTitle(m.resolved), detail: m.resolved, unknown: false };
  // Claude Code's aliases (opus, sonnet[1m], …) against its models' IDs (claude-…).
  if (agent === "claude" && !m.id.startsWith("claude-")) return { value: m.id, title: m.name || m.id, detail: "", unknown: true };
  // DSH's: by its display name, its ID in full and where from (its provider; each line it comes by, 工单 #98 补充).
  const title = m.name || modelTitle(m.id);
  const id = idText(m.id);
  return { value: m.id, title, detail: [id === title ? "" : id, m.source].filter(Boolean).join(" · "), unknown: false };
}

/**
 * An agent's models by where they come from (工单 #136 P18: their IDs in a row, wrapped, told nothing): each provider
 * once, in the order first met, with its models' own IDs — DSH's ["provider","model"], or a name "Provider · model"
 * (工单 #99); a model from no provider in particular under "".
 */
export function modelGroups(models: AgentModel[]): { provider: string; models: string[] }[] {
  const out: { provider: string; models: string[] }[] = [];
  for (const m of models) {
    let provider = "";
    let model = m.name || modelTitle(m.id);
    const parts = idText(m.id).split("/");
    const named = (m.name ?? "").split(" · ");
    if (named.length === 2) [provider, model] = named;
    else if (m.id.startsWith("[") && parts.length === 2) [provider, model] = parts;
    const g = out.find((x) => x.provider === provider) ?? out[out.push({ provider, models: [] }) - 1];
    if (!g.models.includes(model)) g.models.push(model);
  }
  return out;
}

/**
 * An agent's models as its model lists show them (工单 #99): each its specific model, as the machine found it. current,
 * a model a conversation runs that is not among them (an alias it ran as, resolved; one listed no more), comes first.
 */
export function modelChoices(agent: string, models: AgentModel[], current = ""): ModelChoice[] {
  const out = models.map((m) => choiceOf(agent, m));
  if (current && !out.some((c) => c.value === current)) out.unshift({ value: current, title: modelTitle(current), detail: idText(current) === modelTitle(current) ? "" : idText(current), unknown: false });
  return out;
}

/**
 * The model an agent's sessions run when none is chosen (工单 #99: the default names its specific model): the
 * machine's default for it, else the agent's own; undefined when neither is known.
 */
export function defaultModelChoice(a: Pick<AgentStatus, "name" | "models" | "defaults" | "own"> | undefined): ModelChoice | undefined {
  const id = a?.defaults?.model || a?.own?.model;
  if (!a || !id) return undefined;
  const m = a.models.find((x) => x.id === id || x.resolved === id);
  return m ? choiceOf(a.name, m) : modelChoices(a.name, [], id)[0];
}

/** A model a conversation runs, as its model list has it: the alias that resolves to it here, else itself. */
export function listedModel(id: string, models: AgentModel[]): string {
  if (!id || models.some((m) => m.id === id)) return id;
  return models.find((m) => m.resolved === id)?.id ?? id;
}

/** Formats a token count as compact context window text (e.g. 200000 -> "200K", 1048576 -> "1M"). */
export function formatContextWindow(tokens: number | undefined): string {
  if (!tokens || tokens <= 0) return "";
  if (tokens >= 1_000_000) {
    const m = Math.round((tokens / 1_000_000) * 10) / 10;
    return Number.isInteger(m) ? `${m}M` : `${m}M`;
  }
  if (tokens >= 1_000) return `${Math.round(tokens / 1_000)}K`;
  return String(tokens);
}

function trimPrice(v: number): string {
  return Number(v.toFixed(4)).toString();
}

/**
 * Concise open model metadata summary (工单 #114): context window, capabilities,
 * and reference per-1M pricing explicitly marked as estimate. Empty for
 * unknown/custom models that had no exact catalog match.
 */
export function modelMetadataSummary(
  m: Pick<AgentModel, "contextWindow" | "maxOutputTokens" | "capabilities" | "inputCostPerMillion" | "outputCostPerMillion"> | undefined,
  estLabel = "est.",
): string {
  if (!m) return "";
  const parts: string[] = [];
  const ctx = formatContextWindow(m.contextWindow);
  if (ctx) parts.push(ctx);
  if (m.capabilities && m.capabilities.length > 0) parts.push(m.capabilities.join("/"));
  if ((m.inputCostPerMillion ?? 0) > 0 || (m.outputCostPerMillion ?? 0) > 0) {
    parts.push(`~$${trimPrice(m.inputCostPerMillion ?? 0)}/$${trimPrice(m.outputCostPerMillion ?? 0)} /1M (${estLabel})`);
  }
  return parts.join(" · ");
}

/** A project as the owner knows it: the name they gave it (工单 #50), else its id. */
export function projectTitle(p: { name?: string; projectId: string }): string {
  return p.name?.trim() || p.projectId;
}

/** A project id the owner most likely did not choose (an agent's default folder name): its screens suggest naming it. */
export function defaultProjectName(p: { name?: string; projectId: string }): boolean {
  return !p.name?.trim() && /^(project|projects|workspace|workspaces|default|untitled|new-project|my-project|code|repo|src|test|tmp)(-\d+)?$/.test(p.projectId);
}
