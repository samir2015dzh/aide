import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { MachineSchema, type Machine } from "@aide/proto/local";
import { AgentLoginState, AgentProblem, DSHBridgeState } from "@aide/proto/machine";
import { describe, expect, test } from "vitest";
import { bridgeView, clientCategory, grantCategory, grantsByCategory, machineConnections } from "./connections";

const now = new Date("2026-11-22T10:00:00Z");
const checked = timestampFromDate(new Date("2026-11-22T09:58:00Z"));

function machine(agents: object[], over: Partial<Machine> = {}): Machine {
  return create(MachineSchema, {
    machineId: "m1",
    machineName: "work-pc",
    online: true,
    status: { agents: agents.map((a) => ({ checkedAt: checked, ...a })) },
    ...over,
  } as never);
}

describe("connections", () => {
  test("Claude Desktop signed in and the command line not are two rows that say so (工单 #44)", () => {
    const c = machineConnections(machine([{ name: "claude", available: true, version: "2.1.330", loggedIn: false, desktop: { installed: true, signedIn: true } }]), now);
    expect(c.rows.map((r) => [r.id, r.state, r.text])).toEqual([
      ["claude-desktop", "ok", "已登录"],
      ["claude-cli", "missing", "未登录"],
    ]);
    expect(c.rows[1].lacks).toContain("Desktop 的登录不会续给它");
    expect(c.state).toBe("missing");
  });

  test("DSH with its plugin in each of its states (工单 #46)", () => {
    const dsh = (bridge: object) => machineConnections(machine([{ name: "deepseek", available: true, version: "1.4.0", dshBridge: { latestVersion: "0.3.0", ...bridge } }]), now).rows;
    const st = (b: object) => dsh(b).map((r) => [r.id, r.state, r.text]);
    expect(st({ state: DSHBridgeState.DSH_BRIDGE_STATE_NOT_INSTALLED })[1]).toEqual(["deepseek-bridge", "partial", "未备包"]);
    expect(st({ state: DSHBridgeState.DSH_BRIDGE_STATE_NOT_INSTALLED, packagePath: "/x.tgz" })[1]).toEqual(["deepseek-bridge", "partial", "已备包，未安装"]);
    expect(st({ state: DSHBridgeState.DSH_BRIDGE_STATE_RUNNING, version: "0.3.0" })).toEqual([
      ["deepseek", "ok", "已检测到 · 1.4.0"],
      ["deepseek-bridge", "ok", "已安装 · 0.3.0"],
    ]);
    expect(st({ state: DSHBridgeState.DSH_BRIDGE_STATE_RUNNING, version: "0.2.9" })[1][2]).toBe("版本过旧（0.2.9，最新 0.3.0）");
    // Without its plugin, DSH works only in part, and the gap is said.
    const r = dsh({ state: DSHBridgeState.DSH_BRIDGE_STATE_NOT_INSTALLED });
    expect(r[0].state).toBe("partial");
    expect(r[1].lacks).toContain("能用：Aide 发起的 DSH 任务");
    expect(r[1].fix).toBe("prepareBridge");
  });

  test("a finding is not passed off as now: offline, or checked too long ago, it is unknown with when", () => {
    const off = machineConnections(machine([{ name: "codex", available: true, loggedIn: true }], { online: false }), now);
    expect(off.rows[0].state).toBe("unknown");
    expect(off.rows[0].lacks).toContain("上次知道的是「可以用」");
    const old = machineConnections(machine([{ name: "codex", available: true, loggedIn: true, checkedAt: timestampFromDate(new Date("2026-11-22T08:00:00Z")) }]), now);
    expect(old.rows[0].state).toBe("unknown");
    expect(old.rows[0].lacks).toContain("之后没有再检测过");
  });

  test("hidden and absent agents are left out; one found but signed out is missing", () => {
    const c = machineConnections(
      machine([
        { name: "kimi", available: false, problem: AgentProblem.NOT_FOUND },
        { name: "gemini", available: true, hidden: true },
        { name: "codex", available: true, loggedIn: false },
      ]),
      now,
    );
    expect(c.rows.map((r) => [r.id, r.state])).toEqual([["codex", "missing"]]);
  });

  test("bridge view with nothing known", () => {
    expect(bridgeView(undefined)).toBe("unknown");
  });
});

describe("agent categories", () => {
  const names = ["work-pc", "BigBox"];
  test("a coding agent on a machine follows the machine; a personal agent the account (所有者 2026-11-22)", () => {
    expect(grantCategory("DSH on work-pc", names)).toEqual({ category: "local", machine: "work-pc" });
    expect(grantCategory("client@BigBox", names)).toEqual({ category: "local", machine: "BigBox" });
    expect(grantCategory("Codex", names)).toEqual({ category: "local", machine: undefined });
    expect(grantCategory("Muse", names)).toEqual({ category: "personal" });
    expect(grantCategory("Grok Bot (cursor.com)", names)).toEqual({ category: "personal" });
    expect(grantCategory("Claude (claude.ai)", names)).toEqual({ category: "personal" });
    expect(grantCategory("Reviewer", names)).toEqual({ category: "personal" });
  });

  test("grants grouped: personal ones, and coding agents under the machine they run on", () => {
    const m = machine([]);
    const g = grantsByCategory(
      [
        { clientId: "a", label: "Muse" },
        { clientId: "b", label: "DSH on work-pc" },
        { clientId: "c", label: "Codex" },
        { clientId: "d", label: "Gone", revoked: true },
      ] as never,
      [m],
    );
    expect(g.personal.map((c) => c.clientId)).toEqual(["a"]);
    expect(g.local.get("m1")?.map((c) => c.clientId)).toEqual(["b"]);
    expect(g.local.get("")?.map((c) => c.clientId)).toEqual(["c"]);
  });
});

describe("signing Claude Code in from Aide (工单 #51)", () => {
  const claude = (login: object) => machineConnections(machine([{ name: "claude", available: true, loggedIn: false, login }]), now).rows.find((r) => r.id === "claude-cli")!;
  test("while the browser is open there, it says so and offers to stop", () => {
    const r = claude({ state: AgentLoginState.WAITING });
    expect([r.state, r.text, r.fix]).toEqual(["checking", "正在登录…", "claudeLogin"]);
  });
  test("one that failed, timed out or was cancelled says so, and can be tried again", () => {
    expect(claude({ state: AgentLoginState.FAILED, detail: "the browser was closed" }).lacks).toContain("the browser was closed");
    expect(claude({ state: AgentLoginState.TIMED_OUT }).lacks).toContain("10 分钟");
    expect(claude({ state: AgentLoginState.CANCELLED }).fix).toBe("claudeLogin");
  });
});

describe("the kind the owner set (工单 #52)", () => {
  const m = machine([]);
  test("wins over the name, everywhere; none set, the name tells", () => {
    expect(clientCategory({ label: "Muse", category: "local", categoryMachineId: "m1" }, [m])).toEqual({ category: "local", machine: "work-pc", set: true });
    expect(clientCategory({ label: "DSH on work-pc", category: "personal", categoryMachineId: "" }, [m])).toEqual({ category: "personal", machine: undefined, set: true });
    expect(clientCategory({ label: "DSH on work-pc", category: "", categoryMachineId: "" }, [m])).toEqual({ category: "local", machine: "work-pc", set: false });
    const g = grantsByCategory([{ clientId: "a", label: "Muse", category: "local", categoryMachineId: "m1" }] as never, [m]);
    expect(g.local.get("m1")?.map((c) => c.clientId)).toEqual(["a"]);
  });
});
