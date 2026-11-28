import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { MachineSchema, type Machine } from "@aide/proto/local";
import { AgentLoginState, AgentProblem, DSHBridgeInstallState, DSHBridgeState } from "@aide/proto/machine";
import { describe, expect, test } from "vitest";
import { agentEntry, bridgeView, clientCategory, grantCategory, grantsByCategory, machineConnections, rowPresence } from "./connections";
import { codingAgentShort, codingAgentTitle } from "./agents";
import { FailureKind, FailureSchema } from "@aide/proto/session";
import { codingBotState, personalBotState } from "./roster";
import { failureAdvice } from "./text";

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
  // 工单 #180: signed out by what its last run said, the row says so in its words, and that a test run checks again.
  test("an agent signed out by its last run says why", () => {
    const seen = machineConnections(machine([{ name: "antigravity", available: true, loggedIn: false, signInSeen: { signedIn: false, why: "authentication required" } }]), now).rows[0];
    expect([seen.text, seen.lacks]).toEqual(["未登录", "Antigravity 上次在 Aide 里运行时说它没登录（authentication required）：可以在连接页点「运行测试」再验一次。"]);
    const plain = machineConnections(machine([{ name: "antigravity", available: true, loggedIn: false }]), now).rows[0];
    expect(plain.lacks).toBe("Antigravity 装了但没登录：这台机器上暂时不能把任务派给它。");
  });

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
    // Prepared or not is Aide's business, not the owner's (工单 #59): not installed either way.
    expect(st({ state: DSHBridgeState.DSH_BRIDGE_STATE_NOT_INSTALLED })[1]).toEqual(["deepseek-bridge", "partial", "未安装"]);
    expect(st({ state: DSHBridgeState.DSH_BRIDGE_STATE_NOT_INSTALLED, packagePath: "/x.tgz" })[1]).toEqual(["deepseek-bridge", "partial", "未安装"]);
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

  test("the install handed to DSH (工单 #59): at it, failed with DSH's reason, waiting for a restart; answering wins", () => {
    const row = (bridge: object) => machineConnections(machine([{ name: "deepseek", available: true, dshBridge: { latestVersion: "0.3.1", ...bridge } }]), now).rows[1];
    const notInstalled = { state: DSHBridgeState.DSH_BRIDGE_STATE_NOT_INSTALLED };
    const at = row({ ...notInstalled, install: { state: DSHBridgeInstallState.DSH_BRIDGE_INSTALL_STATE_RUNNING, sessionId: "s1" } });
    expect([at.state, at.text, at.fix]).toEqual(["checking", "DSH 正在安装…", "installBridge"]);
    const failed = row({ ...notInstalled, install: { state: DSHBridgeInstallState.DSH_BRIDGE_INSTALL_STATE_FAILED, reason: "别的插件的常驻连接器占着 uv.exe" } });
    expect([failed.state, failed.text, failed.lacks]).toEqual(["partial", "DSH 没装成功", "别的插件的常驻连接器占着 uv.exe"]);
    const restart = row({ ...notInstalled, install: { state: DSHBridgeInstallState.DSH_BRIDGE_INSTALL_STATE_RESTART } });
    expect([restart.text, restart.fix]).toEqual(["已装好，等 DSH Desktop 重启", "openDSH"]);
    const done = row({ state: DSHBridgeState.DSH_BRIDGE_STATE_RUNNING, version: "0.3.1", install: { state: DSHBridgeInstallState.DSH_BRIDGE_INSTALL_STATE_FAILED } });
    expect([done.state, done.text]).toEqual(["ok", "已安装 · 0.3.1"]);
  });

  test("one entry per agent, one name, one state (工单 #53): DSH is DSH, DeepSeek only its models", () => {
    expect(codingAgentTitle("deepseek")).toBe("DSH");
    expect(codingAgentShort("deepseek")).toBe("DSH");
    const c = machineConnections(
      machine([
        { name: "claude", available: true, version: "2.1.330", loggedIn: true, desktop: { installed: true, signedIn: true } },
        { name: "deepseek", available: true, loggedIn: true, version: "1.4.0", models: [{ id: "v4", name: "DeepSeek V4" }], dshBridge: { state: DSHBridgeState.DSH_BRIDGE_STATE_NOT_INSTALLED } },
        { name: "codex", available: true, loggedIn: true },
      ]),
      now,
    );
    const claude = agentEntry(c, "claude")!;
    expect(claude.rows.map((r) => r.id)).toEqual(["claude-desktop", "claude-cli"]);
    // Its state is its main row's, the command line Aide runs, on the desktop as on the phone (工单 #56).
    expect([claude.state, claude.text]).toEqual(["ok", "已登录 · 2.1.330"]);
    const dsh = agentEntry(c, "deepseek")!;
    expect(dsh.rows.map((r) => r.title)).toEqual(["DSH", "DSH 的 Aide 插件"]);
    expect(dsh.rows[0].note).toBe("它可用的模型：\nDeepSeek V4");
    // 工单 #136 P18: a line a provider, its models by their IDs (DSH's ["provider","model"], named "Provider · model"),
    // a long list cut with how many more — not raw IDs in a row.
    const many = machineConnections(
      machine([{ name: "deepseek", available: true, loggedIn: true, models: [
        { id: '["deepseek-official","deepseek-v4-flash"]', name: "DeepSeek · deepseek-v4-flash" },
        { id: '["deepseek-official","deepseek-v4-pro"]', name: "DeepSeek · deepseek-v4-pro" },
        ...["a", "b", "c", "d", "e"].map((x) => ({ id: `["magpie","m-${x}"]` })),
      ] }]),
      now,
    );
    expect(agentEntry(many, "deepseek")!.rows[0].note).toBe("它可用的模型：\nDeepSeek：deepseek-v4-flash、deepseek-v4-pro\nmagpie：m-a、m-b、m-c、m-d 等 1 个");
    expect([dsh.state, dsh.text]).toEqual(["partial", "已登录 · 1.4.0"]);
    // 工单 #135 P17：DSH 登录未确认时不得标成绿点已检测到，须统一报未确认 + partial
    const dshUnc = agentEntry(
      machineConnections(machine([{ name: "deepseek", available: true, loggedIn: false, loginUnconfirmed: true, version: "1.4.0", dshBridge: { state: DSHBridgeState.DSH_BRIDGE_STATE_RUNNING, version: "0.3.0" } }]), now),
      "deepseek",
    )!;
    expect([dshUnc.state, dshUnc.text]).toEqual(["partial", "未确认 · 1.4.0"]);
    expect(c.rows.some((r) => r.title.includes("DeepSeek"))).toBe(false);
    expect(agentEntry(c, "codex")!.text).toBe("可以用");
    expect(agentEntry(c, "gemini")).toBeUndefined();
    // Stale: the entry is unknown, never "signed in" beside "unknown".
    const old = machineConnections(machine([{ name: "claude", available: true, loggedIn: true, checkedAt: timestampFromDate(new Date("2026-11-22T08:00:00Z")) }]), now);
    expect([agentEntry(old, "claude")!.state, agentEntry(old, "claude")!.text]).toEqual(["unknown", "状态未知"]);
  });

  test("a finding is not passed off as now: offline, or checked too long ago, it is unknown with when", () => {
    const off = machineConnections(machine([{ name: "codex", available: true, loggedIn: true }], { online: false }), now);
    expect(off.rows[0].state).toBe("unknown");
    expect(off.rows[0].lacks).toContain("上次知道的是「可以用」");
    const old = machineConnections(machine([{ name: "codex", available: true, loggedIn: true, checkedAt: timestampFromDate(new Date("2026-11-22T08:00:00Z")) }]), now);
    expect(old.rows[0].state).toBe("unknown");
    expect(old.rows[0].lacks).toContain("之后没再确认");
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

  test("an agent whose sign-in its check cannot tell is unconfirmed, tasks may go to it (工单 #112)", () => {
    const m = machine([{ name: "gemini", available: true, loggedIn: false, loginUnconfirmed: true, version: "0.9.0" }]);
    const r = machineConnections(m, now).rows[0];
    expect([r.state, r.text, r.fix]).toEqual(["unknown", "未确认", undefined]);
    expect(r.lacks).toContain("通过后会自动更新为已登录");
    expect(codingBotState(m, "gemini", now)!.usable).toBe("unverified");
  });

  // 审计 B / 工单 #135 P25: the dot is read from the presence enum, one colour per label on every page — "未确认" is
  // unconfirmed for DSH, Claude Code and an ACP agent alike (it was yellow for DSH, grey for the others), "已登录" signed
  // in whatever DSH's plugin makes its row's state, "状态未知" unknown.
  test("one presence per label: unconfirmed the same for DSH, Claude Code and ACP agents; signed in the same everywhere", () => {
    const presences = (agents: object[], over: Partial<Machine> = {}) => {
      const m = machine(agents, over);
      const c = machineConnections(m, now);
      return Object.fromEntries(
        [...new Set(c.rows.map((r) => r.agent))].map((a) => {
          const e = agentEntry(c, a)!;
          const b = codingBotState(m, a, now, c)!;
          // A bot's dot (sidebar, bot page) is its main row's (connections card, machine page).
          expect(b.presence).toBe(rowPresence(e.main));
          return [a, [b.word, b.presence]];
        }),
      );
    };
    const bridge = { state: DSHBridgeState.DSH_BRIDGE_STATE_RUNNING, version: "0.3.0" };
    expect(
      presences([
        { name: "claude", available: true, loggedIn: false, loginUnconfirmed: true },
        { name: "deepseek", available: true, loggedIn: false, loginUnconfirmed: true, dshBridge: bridge },
        { name: "gemini", available: true, loggedIn: false, loginUnconfirmed: true },
      ]),
    ).toEqual({ claude: ["未确认", "unconfirmed"], deepseek: ["未确认", "unconfirmed"], gemini: ["未确认", "unconfirmed"] });
    expect(
      presences([
        { name: "claude", available: true, loggedIn: true },
        // Signed in with its plugin not running: its row is partial, its label and dot still "signed in".
        { name: "deepseek", available: true, loggedIn: true, dshBridge: { state: DSHBridgeState.DSH_BRIDGE_STATE_NOT_INSTALLED } },
        { name: "codex", available: true, loggedIn: true },
        { name: "kimi", available: true, loggedIn: false },
      ]),
    ).toEqual({ claude: ["已登录", "signedIn"], deepseek: ["已登录", "signedIn"], codex: ["可以用", "signedIn"], kimi: ["未登录", "signedOut"] });
    // Stale or its machine away: unknown, whatever it last said (unconfirmed included).
    expect(presences([{ name: "gemini", available: true, loggedIn: false, loginUnconfirmed: true }], { online: false })).toEqual({ gemini: ["状态未知", "unknown"] });
    // The plugin's own row: installed works, not running works partly.
    const rows = machineConnections(machine([{ name: "deepseek", available: true, loggedIn: true, dshBridge: { state: DSHBridgeState.DSH_BRIDGE_STATE_NOT_RUNNING, version: "0.3.0" } }]), now).rows;
    expect(rows.map((r) => [r.id, rowPresence(r)])).toEqual([["deepseek", "signedIn"], ["deepseek-bridge", "unconfirmed"]]);
    // Personal agents: online, or not known now.
    const client = (o: object) => ({ clientId: "c1", label: "ChatGPT", ...o }) as never;
    expect(personalBotState(client({ online: true }), now).presence).toBe("online");
    expect(personalBotState(client({ online: false }), now).presence).toBe("unknown");
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
  test("while it waits it offers to stop; it claims no browser, it tells the address and then the code (工单 #118)", () => {
    const r = claude({ state: AgentLoginState.WAITING });
    expect([r.state, r.text, r.fix]).toEqual(["checking", "正在登录…", "claudeLogin"]);
    expect(r.lacks).toContain("等它给出登录地址");
    expect(r.lacks).not.toContain("浏览器");
    expect(claude({ state: AgentLoginState.WAITING, url: "https://sign-in.example/oauth/authorize?x" }).lacks).toContain("在任意设备上打开下面的登录地址");
    expect(claude({ state: AgentLoginState.WAITING, url: "https://sign-in.example/oauth/authorize?x", codeSent: true }).lacks).toContain("正在核对");
  });
  test("one that failed, timed out or was cancelled says so, and can be tried again", () => {
    expect(claude({ state: AgentLoginState.FAILED, detail: "the browser was closed" }).lacks).toContain("the browser was closed");
    expect(claude({ state: AgentLoginState.TIMED_OUT }).lacks).toContain("15 分钟");
    expect(claude({ state: AgentLoginState.CANCELLED }).fix).toBe("claudeLogin");
  });
});

describe("Claude Code's sign-in is its command line's own (工单 #108)", () => {
  const state = (a: object) => {
    const m = machine([{ name: "claude", available: true, version: "2.1.330", desktop: { installed: true, signedIn: true }, ...a }]);
    const r = machineConnections(m, now).rows.find((x) => x.id === "claude-cli")!;
    return { r, bot: codingBotState(m, "claude", now)! };
  };
  test("signed in, signed out, and unconfirmed are three states; the desktop app signed in stands in for none", () => {
    expect(state({ loggedIn: true }).r.state).toBe("ok");
    const out = state({ loggedIn: false });
    expect([out.r.state, out.r.text, out.r.fix, out.bot.usable, out.bot.fix]).toEqual(["missing", "未登录", "claudeLogin", "no", "claudeLogin"]);
    const vague = state({ loggedIn: false, loginUnconfirmed: true });
    expect([vague.r.state, vague.r.text, vague.r.fix]).toEqual(["unknown", "未确认", "claudeLogin"]);
    expect(vague.r.lacks).toContain("不拿 Claude Desktop 的登录代替它");
    expect([vague.bot.word, vague.bot.usable, vague.bot.fix]).toEqual(["未确认", "unverified", "claudeLogin"]);
  });
  test("a session failed not signed in: the card points to signing it in from Aide, never to a terminal", () => {
    const f = create(FailureSchema, { kind: FailureKind.AUTH, message: "Not logged in · Please run /login" });
    expect(failureAdvice(f, "Claude Code", "claude")).toContain("点下面的登录按钮会给出登录地址");
    expect(failureAdvice(f, "Claude Code", "claude")).not.toContain("CLI");
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
