import { t } from "./i18n";

// DSH's permission presets (工单 #89): DSH's own, as they are — none made up by Aide. A session's preset is one of its
// parameters (DSH's modes, chosen as it starts); a project can have one its DSH sessions start with when none is chosen
// (Project.dshPreset). Other agents keep their own modes, untouched by this.

/** DSH's name among the coding agents. */
export const dshAgent = "deepseek";

/** Coding agents that use the three-tier permission presets (DSH + Antigravity, 工单 #89 / #111). */
export const usesPermissionPresets = (agent: string) => agent === dshAgent || agent === "antigravity";

/** DSH's own presets (@deepseek-ai/dsh-permission-presets), least allowed first; any other its configuration has keeps its name. */
const dshPresets = ["read-only", "workspace-write", "danger-full-access"] as const;
type DSHPreset = (typeof dshPresets)[number];
const isDSHPreset = (id: string): id is DSHPreset => (dshPresets as readonly string[]).includes(id);

/** A mode as people call it: a DSH preset by the name DSH gives it ("仅可查看"), else its agent's name for it, else its id. */
export function modeLabel(agent: string, m: { id: string; name?: string }): string {
  if (usesPermissionPresets(agent) && isDSHPreset(m.id)) return t(`dshPreset.${m.id}`);
  return m.name || m.id;
}

/** The preset a project's DSH/Antigravity sessions start with when none is chosen ("": agent's own); "" for another agent. */
export function projectPreset(agent: string, p?: { dshPreset?: string }): string {
  return usesPermissionPresets(agent) ? (p?.dshPreset ?? "") : "";
}

/** Whether a session's mode allows more than its project's preset: a preset further down the list, or one that can't be told apart. */
export function aboveProject(agent: string, mode: string, preset: string): boolean {
  if (!usesPermissionPresets(agent) || !mode || !preset || mode === preset) return false;
  const a = isDSHPreset(mode) ? dshPresets.indexOf(mode) : -1;
  const b = isDSHPreset(preset) ? dshPresets.indexOf(preset) : -1;
  return a < 0 || b < 0 || a > b;
}

/**
 * What a box handing work to agent says of its mode next to the project's preset (工单 #89: never past it silently):
 * the "default" choice is the project's preset, named; a choice above it is said so before the task starts.
 */
export function modeChoice(agent: string, mode: string, project: { name?: string; projectId: string; dshPreset?: string } | undefined, modes: readonly { id: string; name?: string }[]) {
  const preset = projectPreset(agent, project);
  const name = (id: string) => modeLabel(agent, modes.find((m) => m.id === id) ?? { id });
  return {
    /** The default choice's label, when the project sets one ("项目默认：仅可查看"); undefined: the box's own. */
    defaultLabel: preset ? t("permission.projectDefault", { preset: name(preset) }) : undefined,
    /** Said over the box when the choice is above the project's. */
    above: aboveProject(agent, mode, preset) ? t("permission.above", { project: project?.name?.trim() || project?.projectId || "", preset: name(preset), mode: name(mode) }) : undefined,
  };
}
