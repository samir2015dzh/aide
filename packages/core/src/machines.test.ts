import { create } from "@bufbuild/protobuf";
import { AgentStatusSchema } from "@aide/proto/machine";
import { describe, expect, test } from "vitest";
import { agentTiles, defaultModelChoice, folderName, isAbsolutePath, listedModel, modelChoices, modelTitle, platformTitle } from "./machines";

describe("machines", () => {
  test("models read as their agent names them, Claude's IDs as people call them", () => {
    const codex = create(AgentStatusSchema, { models: [{ id: "gpt-6-astra", name: "GPT-6 Astra" }, { id: "opus", name: "Opus" }] }).models;
    expect(modelTitle("gpt-6-astra", codex)).toBe("GPT-6 Astra");
    expect(modelTitle("opus", codex)).toBe("Opus");
    expect(modelTitle("claude-opus-5-5")).toBe("Opus 5.5");
    expect(modelTitle("claude-haiku-5-5")).toBe("Haiku 5.5");
    expect(modelTitle("claude-fable-5-1[1m]")).toBe("Fable 5.1 (1M)");
    expect(modelTitle("claude-sonnet-4-20250514")).toBe("Sonnet 4");
    expect(modelTitle("claude-3-5-sonnet-20241022")).toBe("claude-3-5-sonnet-20241022");
    expect(modelTitle("gemini-3-pro")).toBe("gemini-3-pro");
  });

  // 工单 #99: every model in a list is its specific model, as the machine found it; a family name alone never is one.
  test("model lists name each one's specific model", () => {
    const claude = create(AgentStatusSchema, {
      name: "claude",
      models: [
        { id: "opus", name: "Opus", resolved: "claude-opus-5-5" },
        { id: "fable", name: "Fable", resolved: "claude-fable-5-2" },
        { id: "sonnet[1m]", name: "Sonnet (1M context)", resolved: "claude-sonnet-5-5[1m]" },
        { id: "haiku", name: "Haiku" },
      ],
      own: { model: "claude-sonnet-5-5" },
    });
    expect(modelChoices("claude", claude.models)).toEqual([
      { value: "opus", title: "Opus 5.5", detail: "claude-opus-5-5", unknown: false },
      { value: "fable", title: "Fable 5.2", detail: "claude-fable-5-2", unknown: false },
      { value: "sonnet[1m]", title: "Sonnet 5.5 (1M)", detail: "claude-sonnet-5-5[1m]", unknown: false },
      { value: "haiku", title: "Haiku", detail: "", unknown: true }, // a Claude Code too old to say
    ]);
    expect(modelTitle("opus", claude.models)).toBe("Opus 5.5");
    // The default: the agent's own, named; the machine's default for it first.
    expect(defaultModelChoice(claude)).toEqual({ value: "claude-sonnet-5-5", title: "Sonnet 5.5", detail: "claude-sonnet-5-5", unknown: false });
    expect(defaultModelChoice({ ...claude, defaults: { $typeName: "aide.identity.v1.AgentDefaults", agent: "claude", model: "opus", effort: "", speed: 0, mode: "" } })?.title).toBe("Opus 5.5");
    expect(defaultModelChoice(create(AgentStatusSchema, { name: "claude" }))).toBeUndefined();
    // What a conversation runs, as the list has it; one not there is shown too, first.
    expect(listedModel("claude-opus-5-5", claude.models)).toBe("opus");
    expect(listedModel("claude-opus-4-1", claude.models)).toBe("claude-opus-4-1");
    expect(modelChoices("claude", claude.models, "claude-opus-4-1")[0]).toEqual({ value: "claude-opus-4-1", title: "Opus 4.1", detail: "claude-opus-4-1", unknown: false });
    // DSH's (an ACP agent's): its own names, its IDs read out.
    const dsh = [{ id: '["deepseek","deepseek-v4-pro"]', name: "DeepSeek V4 Pro (DeepSeek)" }, { id: "magpie/v4-flash", name: "" }].map((m) => create(AgentStatusSchema, { models: [m] }).models[0]);
    expect(modelChoices("deepseek", dsh)).toEqual([
      { value: '["deepseek","deepseek-v4-pro"]', title: "DeepSeek V4 Pro (DeepSeek)", detail: "deepseek/deepseek-v4-pro", unknown: false },
      { value: "magpie/v4-flash", title: "magpie/v4-flash", detail: "", unknown: false },
    ]);
    // One model by two lines of its provider is one entry, provider-first naming (工单 #117); a custom provider's in its own.
    const lines = [{ id: '["deepseek","deepseek-v4-flash"]', name: "DeepSeek · deepseek-v4-flash", source: "DeepSeek · DeepSeek Account" }, { id: '["magpie","group/deepseek-v4.1"]', name: "magpie · group/deepseek-v4.1", source: "magpie" }].map((m) => create(AgentStatusSchema, { models: [m] }).models[0]);
    expect(modelChoices("deepseek", lines).map((c) => [c.title, c.detail])).toEqual([
      ["DeepSeek · deepseek-v4-flash", "deepseek/deepseek-v4-flash · DeepSeek · DeepSeek Account"],
      ["magpie · group/deepseek-v4.1", "magpie/group/deepseek-v4.1 · magpie"],
    ]);
    expect(defaultModelChoice({ name: "deepseek", models: lines, own: { $typeName: "aide.identity.v1.AgentDefaults", agent: "", model: '["deepseek","deepseek-v4-flash"]', effort: "", speed: 0, mode: "" } })?.title).toBe("DeepSeek · deepseek-v4-flash");
  });

  test("platforms read as OS and architecture", () => {
    expect(platformTitle("darwin/arm64")).toBe("macOS · arm64");
    expect(platformTitle("linux/amd64")).toBe("Linux · amd64");
    expect(platformTitle("macos")).toBe("macOS");
    expect(platformTitle("plan9/386")).toBe("plan9 · 386");
    expect(platformTitle("")).toBe("");
  });

  test("agent tiles: ready, then not logged in, then the suggested missing ones", () => {
    const a = (name: string, available: boolean, loggedIn = false) => create(AgentStatusSchema, { name, available, loggedIn });
    const tiles = agentTiles([a("kimi", false), a("codex", true), a("gemini", true, true), a("claude", false), a("cursor", true, true)]);
    expect(tiles.map((t) => t.name)).toEqual(["cursor", "gemini", "codex", "claude"]);
  });
});

describe("project paths", () => {
  test("are absolute by their machine's platform", () => {
    expect(isAbsolutePath("/home/me/app", "linux/amd64")).toBe(true);
    expect(isAbsolutePath("C:\\code\\app", "linux/amd64")).toBe(false);
    expect(isAbsolutePath("C:\\code\\app", "windows/arm64")).toBe(true);
    expect(isAbsolutePath("d:/code/app", "windows/amd64")).toBe(true);
    expect(isAbsolutePath("\\\\nas\\share\\app", "windows/amd64")).toBe(true);
    expect(isAbsolutePath("/code/app", "windows/amd64")).toBe(false);
    expect(isAbsolutePath("code\\app", "windows/amd64")).toBe(false);
    expect(isAbsolutePath("~/code/app", "darwin/arm64")).toBe(true);
    expect(isAbsolutePath("~\\code\\app", "windows/amd64")).toBe(true);
  });
  test("name their last folder", () => {
    expect(folderName("/home/me/my-app/")).toBe("my-app");
    expect(folderName("C:\\code\\my-app")).toBe("my-app");
    expect(folderName("C:\\")).toBe("C:");
    expect(folderName("")).toBe("");
  });
});
