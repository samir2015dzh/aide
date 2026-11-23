import { describe, expect, test } from "vitest";
import { aboveProject, modeChoice, modeLabel, projectPreset } from "./permissions";

const modes = [{ id: "read-only", name: "read-only" }, { id: "workspace-write", name: "Workspace" }, { id: "danger-full-access", name: "" }, { id: "plan", name: "Plan" }];
const biz = { projectId: "project-2", name: "报表", dshPreset: "read-only" };

describe("DSH's permission presets (工单 #89)", () => {
  test("named as DSH names them; another agent's modes and DSH's other presets by their own names", () => {
    expect(modes.map((m) => modeLabel("deepseek", m))).toEqual(["仅可查看", "工作区内修改", "完全权限", "Plan"]);
    expect(modeLabel("codex", { id: "read-only", name: "Read Only" })).toBe("Read Only");
  });

  test("a project's preset is its DSH sessions' default, for DSH alone", () => {
    expect(projectPreset("deepseek", biz)).toBe("read-only");
    expect(projectPreset("claude", biz)).toBe("");
    expect(projectPreset("deepseek", { dshPreset: "" })).toBe("");
  });

  test("a choice above the project's preset is said so; at or below it, or the default, is not", () => {
    expect(aboveProject("deepseek", "danger-full-access", "read-only")).toBe(true);
    expect(aboveProject("deepseek", "workspace-write", "read-only")).toBe(true);
    expect(aboveProject("deepseek", "read-only", "workspace-write")).toBe(false);
    expect(aboveProject("deepseek", "", "read-only")).toBe(false);
    expect(aboveProject("deepseek", "plan", "read-only")).toBe(true); // can't be told apart: said, never silent
    expect(aboveProject("claude", "bypassPermissions", "read-only")).toBe(false);
    expect(modeChoice("deepseek", "", biz, modes)).toEqual({ defaultLabel: "项目默认：仅可查看", above: undefined });
    expect(modeChoice("deepseek", "danger-full-access", biz, modes).above).toBe("比项目「报表」定的 DSH 权限「仅可查看」高：这次会按你选的「完全权限」来");
    expect(modeChoice("claude", "", biz, modes)).toEqual({ defaultLabel: undefined, above: undefined });
  });
});
