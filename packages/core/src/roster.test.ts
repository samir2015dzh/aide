import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { ClientSchema, MachineSchema, type Client, type Machine } from "@aide/proto/local";
import { SessionSchema, SessionStatus } from "@aide/proto/session";
import { describe, expect, test } from "vitest";
import { allBots, botCounts, botCountsText, codingBotState, personalBotState, roster, whileReconnecting } from "./roster";

const now = new Date("2026-11-22T10:00:00Z");
const fresh = timestampFromDate(new Date("2026-11-22T09:58:00Z"));
const old = timestampFromDate(new Date("2026-11-22T09:20:00Z"));

function machine(id: string, name: string, agents: object[], over: Partial<Machine> = {}): Machine {
  return create(MachineSchema, { machineId: id, machineName: name, online: true, status: { agents: agents.map((a) => ({ checkedAt: fresh, ...a })) }, ...over } as never);
}

function client(id: string, label: string, over: object = {}): Client {
  return create(ClientSchema, { clientId: id, grantId: id, label, ...over } as never);
}

describe("one state per agent (工单 #56)", () => {
  test("checked 40 minutes ago: unknown everywhere, can be sent but not confirmed, never green", () => {
    const m = machine("m1", "work-pc", [{ name: "claude", available: true, loggedIn: true, checkedAt: old }]);
    const s = codingBotState(m, "claude", now)!;
    expect([s.state, s.word, s.usable, s.why]).toEqual(["unknown", "状态未知", "unverified", "unknown"]);
    expect(s.line).toContain("之后没再确认");
    expect(s.line).toContain("已登录");
  });

  test("fresh and signed in: ready; paused: not; signed out: not, said as such", () => {
    const m = machine("m1", "work-pc", [
      { name: "claude", available: true, loggedIn: true, version: "2.1.3" },
      { name: "codex", available: true, loggedIn: true, pause: {} },
      { name: "gemini", available: true, loggedIn: false },
    ]);
    expect(codingBotState(m, "claude", now)).toMatchObject({ state: "ok", word: "已登录", line: "已登录 · 2.1.3", usable: "yes" });
    expect(codingBotState(m, "codex", now)).toMatchObject({ word: "已暂停", usable: "no", why: "paused" });
    expect(codingBotState(m, "gemini", now)).toMatchObject({ state: "missing", word: "未登录", usable: "no", why: "signedOut" });
  });

  test("its machine away: unknown, and work can't go", () => {
    const m = machine("m1", "work-pc", [{ name: "deepseek", available: true, loggedIn: true }], { online: false });
    expect(codingBotState(m, "deepseek", now)).toMatchObject({ state: "unknown", word: "状态未知", usable: "no", why: "offline" });
  });

  test("the counts at the top: ready over all, unknown apart (the owner's screenshot: 1 ready, 3 unknown, of 4)", () => {
    const r = roster(
      [
        machine("m1", "work-pc", [{ name: "claude", available: true, loggedIn: true, checkedAt: old }, { name: "deepseek", available: true, loggedIn: true, checkedAt: old }]),
        machine("m2", "laptop", [{ name: "claude", available: true, loggedIn: true }, { name: "deepseek", available: true, loggedIn: true, checkedAt: old }]),
      ],
      [],
      [],
      now,
    );
    const c = botCounts(allBots(r));
    expect(c).toEqual({ ready: 1, unknown: 3, total: 4 });
    expect(botCountsText(c)).toBe("可用 1 · 未知 3 · 共 4");
  });
});

describe("the roster (工单 #57)", () => {
  test("personal agents on the account, coding agents under their machine, one name each", () => {
    const machines = [
      machine("m1", "work-pc", [{ name: "claude", available: true, loggedIn: true }, { name: "deepseek", available: true, loggedIn: true }]),
      machine("m2", "laptop", [{ name: "claude", available: true, loggedIn: true }], { online: false }),
    ];
    const clients = [client("c1", "Muse", { online: true }), client("c2", "DSH on work-pc"), client("c3", "Old", { revoked: true })];
    const sessions = [
      create(SessionSchema, { sessionId: "s1", machineId: "m1", agent: "claude", title: "Fix the CI", status: SessionStatus.RUNNING, updatedAt: fresh }),
      create(SessionSchema, { sessionId: "s2", machineId: "m1", agent: "deepseek", title: "Docs", status: SessionStatus.IDLE, updatedAt: old, createdBy: "c1" }),
    ];
    const r = roster(machines, clients, sessions, now);
    expect(r.personal.map((b) => [b.name, b.state.word, b.doing])).toEqual([["Muse", "在线", "最近：Docs · 40 分钟前"]]);
    expect(r.machines.map((g) => [g.machine.machineName, g.bots.map((b) => b.name)])).toEqual([
      ["work-pc", ["Claude Code · work-pc", "DSH · work-pc"]],
      ["laptop", ["Claude Code · laptop"]],
    ]);
    const [claude, dsh] = r.machines[0].bots;
    expect(claude.doing).toBe("1 个进行中 · Fix the CI");
    // DSH's own grant is on DSH, not a second DSH.
    expect(dsh.client?.clientId).toBe("c2");
    expect(r.machines[1].bots[0].state.why).toBe("offline");
  });

  test("a personal agent not online: unknown, with when it was last seen", () => {
    const r = roster([], [client("c1", "Grok Bot", { lastSeen: old })], [], now);
    expect(r.personal[0].state).toMatchObject({ word: "不在线", line: "上次见到：40 分钟前", usable: "unverified" });
  });

  test("a contact is its bot's own name and face, not a second entry (no Hand Work To page beside the roster)", () => {
    const m = machine("m1", "work-pc", [{ name: "claude", available: true, loggedIn: true }]);
    const p = { personaId: "p1", name: "Reviewer", machineId: "m1", agent: "claude" } as never;
    const r = roster([m], [], [], now, [p]);
    expect(r.machines[0].bots.map((b) => [b.name, b.personas.length])).toEqual([["Reviewer", 1]]);
  });
});

describe("grouped by machine (工单 #75)", () => {
  test("each machine is its own group, headed by its name: its rows name the agent alone; one away keeps its group", () => {
    const machines = [
      machine("m1", "work-pc", [{ name: "deepseek", available: true, loggedIn: true }]),
      machine("m2", "laptop", [{ name: "claude", available: true, loggedIn: true }], { online: false }),
    ];
    const r = roster(machines, [client("c2", "Codex on work-pc")], [], now);
    expect(r.machines.map((g) => [g.machine.machineName, g.bots.map((b) => [b.shortName, b.name])])).toEqual([
      ["work-pc", [["DSH", "DSH · work-pc"], ["Codex", "Codex on work-pc"]]],
      ["laptop", [["Claude Code", "Claude Code · laptop"]]],
    ]);
    expect(r.machines[1].bots[0].state.why).toBe("offline");
  });
});

describe("who gets on the roster (工单 #64)", () => {
  test("client@<machine> is Aide's own client, not an agent: set aside under its machine, whatever it was filed as", () => {
    const machines = [
      machine("m1", "Work-PC", [{ name: "claude", available: true, loggedIn: true }, { name: "deepseek", available: true, loggedIn: true }]),
    ];
    const clients = [client("c1", "client@Work-PC", { online: true }), client("c2", "client@laptop", { category: "personal" }), client("c3", "Muse")];
    const r = roster(machines, clients, [], now);
    expect(allBots(r).map((b) => b.name)).toEqual(["Muse", "Claude Code", "DSH"]);
    expect(r.aside.map((a) => [a.client.label, a.machineId])).toEqual([["client@Work-PC", "m1"], ["client@laptop", ""]]);
  });

  test("a grant filed as on a machine that names no coding agent (the old “Her on studio” ghost): set aside", () => {
    const machines = [machine("m1", "studio", [{ name: "claude", available: true, loggedIn: true }])];
    const r = roster(machines, [client("c1", "Her on studio"), client("c2", "Something on nowhere")], [], now);
    expect(r.machines[0].bots.map((b) => b.name)).toEqual(["Claude Code"]);
    expect(r.aside.map((a) => a.client.label)).toEqual(["Her on studio"]);
    // "Something on nowhere": no machine of that name, a name that is no coding agent → personal (the owner's own say aside).
    expect(r.personal.map((b) => b.name)).toEqual(["Something on nowhere"]);
  });

  test("a coding agent's own grant its machine does not list stays a bot; one the owner filed as a coding agent too", () => {
    const machines = [machine("m1", "work-pc", [{ name: "claude", available: true, loggedIn: true }])];
    const r = roster(machines, [client("c1", "Codex on work-pc"), client("c2", "Helper", { category: "local", categoryMachineId: "m1" })], [], now);
    expect(r.machines[0].bots.map((b) => b.name)).toEqual(["Claude Code", "Codex on work-pc", "Helper"]);
    expect(r.aside).toEqual([]);
  });

  test("a coding agent the owner removed from the roster (hidden on its machine, 工单 #82) stays off, its grant with it", () => {
    const machines = [machine("m1", "work-pc", [{ name: "claude", available: true, loggedIn: true }, { name: "deepseek", available: true, loggedIn: true, hidden: true }])];
    const r = roster(machines, [client("c1", "DSH on work-pc")], [], now);
    expect(allBots(r).map((b) => b.name)).toEqual(["Claude Code"]);
    expect(r.aside.map((a) => a.client.label)).toEqual(["DSH on work-pc"]);
  });
});

describe("this computer's agents: its own word, not the relay's (工单 #87)", () => {
  test("this computer's machine off the relay: its agents as they are; another machine off the relay: unknown", () => {
    const here = machine("m1", "work-pc", [{ name: "claude", available: true, loggedIn: true }], { online: false, local: true });
    const there = machine("m2", "laptop", [{ name: "claude", available: true, loggedIn: true }], { online: false });
    expect(codingBotState(here, "claude", now)).toMatchObject({ state: "ok", usable: "yes" });
    expect(codingBotState(there, "claude", now)).toMatchObject({ state: "unknown", why: "offline" });
  });

  test("while the link to aided is restored: every bot reconnecting, never offline", () => {
    const r = whileReconnecting(roster([machine("m1", "work-pc", [{ name: "claude", available: true, loggedIn: true }], { local: true })], [client("c1", "Muse")], [], now));
    for (const b of allBots(r)) expect(b.state).toMatchObject({ word: "正在重连", why: "reconnecting" });
    expect(allBots(r)).toHaveLength(2);
  });
});

describe("a detected agent filed as personal (工单 #93)", () => {
  test("listed with the personal agents, still its machine's", () => {
    const r = roster([machine("m1", "work-pc", [{ name: "claude", available: true, loggedIn: true, category: "personal" }, { name: "codex", available: true, loggedIn: true }])], [], [], now);
    expect(r.personal.map((b) => b.key)).toEqual(["m:m1/claude"]);
    expect(r.personal[0]).toMatchObject({ kind: "local", machineId: "m1", agent: "claude" });
    expect(r.machines[0].bots.map((b) => b.agent)).toEqual(["codex"]);
  });
});

describe("an MCP connection is online by its last call (工单 #94)", () => {
  const now = new Date("2026-11-23T10:00:00Z");
  const called = (min: number, recent: boolean, online = recent) =>
    personalBotState(create(ClientSchema, { clientId: "c1", label: "Muse", online, recent, lastSeen: timestampFromDate(new Date(now.getTime() - min * 60_000)) }), now);
  test("online just after a call, recently active with when for the rest of the window, then offline with when", () => {
    expect(called(1, true)).toMatchObject({ state: "ok", word: "在线", usable: "yes" });
    expect(called(5, true)).toMatchObject({ state: "ok", word: "最近活跃", line: "5 分钟前调用过", usable: "yes" });
    expect(called(15, false)).toMatchObject({ state: "unknown", word: "不在线", line: "上次见到：15 分钟前" });
  });
  test("an agent connected now is online as before", () => {
    expect(personalBotState(create(ClientSchema, { clientId: "c2", label: "DSH on work-pc", online: true }), now)).toMatchObject({ word: "在线", line: "现在连着你的账号" });
  });
});
