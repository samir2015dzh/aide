import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { MachineSchema, ProjectSchema } from "@aide/proto/local";
import { SessionSchema, SessionStatus, type Session } from "@aide/proto/session";
import { describe, expect, test } from "vitest";
import { botConversations, changedParams, currentConversation, paramsChanged, sessionsByProject, stuckWhy, withContext } from "./conversations";

const at = (m: number) => timestampFromDate(new Date(Date.UTC(2026, 10, 22, 10, m)));
const s = (id: string, over: Partial<Session> = {}): Session =>
  create(SessionSchema, { sessionId: id, machineId: "m1", agent: "claude", projectId: "web", createdBy: "owner", status: SessionStatus.IDLE, updatedAt: at(0), ...over } as never);

describe("a bot's conversations (工单 #67)", () => {
  const b = { machineId: "m1", agent: "claude" };
  const list = [
    s("old", { updatedAt: at(1) }),
    s("new", { updatedAt: at(5) }),
    s("arch", { status: SessionStatus.ARCHIVED, updatedAt: at(9) }),
    s("muse", { createdBy: "c_muse", updatedAt: at(7) }),
    s("codex", { agent: "codex" }),
    s("there", { machineId: "m2" }),
  ];
  const c = botConversations(b, list);

  test("the owner's own, newest first; archived apart; others' work apart; other agents and machines not", () => {
    expect(c.open.map((x) => x.sessionId)).toEqual(["new", "old"]);
    expect(c.archived.map((x) => x.sessionId)).toEqual(["arch"]);
    expect(c.others.map((x) => x.sessionId)).toEqual(["muse"]);
  });

  test("rule 1: it goes on in the one chosen, else the one last active; rule 2: a new one when asked", () => {
    expect((currentConversation(c, undefined) as Session).sessionId).toBe("new");
    expect((currentConversation(c, "old") as Session).sessionId).toBe("old");
    expect((currentConversation(c, "arch") as Session).sessionId).toBe("arch");
    expect(currentConversation(c, "new")).toBe("new");
    expect((currentConversation(c, "deleted") as Session).sessionId).toBe("new");
    expect(currentConversation(botConversations(b, []), undefined)).toBeUndefined();
  });

  test("rule 4: another project, model, effort or mode starts a new one, and says which changed", () => {
    const x = s("x", { model: "opus", effort: "high" });
    expect(paramsChanged(x, { projectId: "web", model: "opus", effort: "high", mode: "" })).toBe(false);
    expect(paramsChanged(x, { projectId: "api", model: "opus", effort: "high", mode: "" })).toBe(true);
    expect(changedParams(x, { projectId: "web", model: "sonnet", effort: "low", mode: "plan" })).toEqual(["model", "effort", "mode"]);
  });

  test("rule 5: one that can't go on says why; rule 6: archived is the machine's own archive", () => {
    const m = create(MachineSchema, { machineId: "m1", status: { agents: [{ name: "claude" }] } } as never);
    const projects = [create(ProjectSchema, { machineId: "m1", projectId: "web" })];
    expect(stuckWhy(s("a"), projects, m)).toBeUndefined();
    expect(stuckWhy(s("a", { status: SessionStatus.ARCHIVED }), projects, m)).toBe("archived");
    expect(stuckWhy(s("a", { projectId: "gone" }), projects, m)).toBe("projectGone");
    expect(stuckWhy(s("a", { agent: "codex" }), projects, m)).toBe("agentGone");
  });

  test("a new conversation with an old one's context: its title, what it came to, then the new words", () => {
    expect(withContext(s("a", { result: "改好了 CI" }), "修 CI", "再加个测试")).toBe("接着之前的对话《修 CI》继续。\n它当时的结论：\n改好了 CI\n\n再加个测试");
    expect(withContext(s("a"), "修 CI", "再加个测试")).toBe("接着之前的对话《修 CI》继续。\n\n再加个测试");
  });
});

describe("the session page's list (工单 #90)", () => {
  test("the same agent's sessions on the same machine, by project, latest activity first; archived ones but the open one left out", () => {
    const open = s("cur", { projectId: "api", status: SessionStatus.ARCHIVED, updatedAt: at(2) });
    const groups = sessionsByProject(open, [
      open,
      s("w1", { projectId: "web", updatedAt: at(3) }),
      s("w2", { projectId: "web", updatedAt: at(8), createdBy: "c_muse" }),
      s("a1", { projectId: "api", updatedAt: at(1) }),
      s("gone", { projectId: "api", status: SessionStatus.ARCHIVED, updatedAt: at(9) }),
      s("codex", { agent: "codex", updatedAt: at(9) }),
      s("there", { machineId: "m2", updatedAt: at(9) }),
    ]);
    expect(groups.map((g) => [g.projectId, g.sessions.map((x) => x.sessionId)])).toEqual([["web", ["w2", "w1"]], ["api", ["cur", "a1"]]]);
  });
});
