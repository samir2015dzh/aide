import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { DeviceSchema, ExecLogEntrySchema } from "@aide/proto/local";
import { AgentStatusSchema, DSHBridgeSchema, DSHBridgeState, ExecOutcome, ExecRecordSchema, RoomDeliverySchema, RoomDeliveryState, RoomMemberSchema, RoomMessageSchema, RoomMode, RoomSchema } from "@aide/proto/machine";
import { SessionEventSchema, SessionSchema, SessionStatus } from "@aide/proto/session";
import { expect, test, type ElectronApplication, type Locator, type Page } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { startDaemon, type Daemon } from "../perf/daemon";
import { ids, titles } from "../perf/world";
import { launch, navTo, sessionList, snap } from "./app";

// 工单 #132（含补充 P28）+ 工单 #134（含补充一/二）+ 工单 #135（P03/P04/P08/P02/P17/P25/P33）+ 工单 #138（P14/P15）+ Fluent 2 视觉与动效回归：
// 1. #134 侧栏展开/收起图标位置守恒（切换钮/概览/任务动态/搜索/账号）、左上角菜单不重复、概览/任务动态无空顶栏、会话页顶栏带 agent 上下文且按钮贴齐右缘；
// 2. #135 状态口径统一（P03/P04/P08/P02/P17/P25/P33）：已完成对话不报「正在聊」且绿点、归档对话顶条/卡片口径一致、离线设备显示「工作机器 · 离线」、收起态三态点色分离、DSH 未确认统一口径、全部对话计数透明、失败消息视觉隔离；
// 3. #132 会话页与个人 agent 主页（Muse）统一居中阅读列、无边框 Markdown 回复流、真实 700 加粗、行内代码、代码块、GFM 表格与时间戳；
// 4. #138 执行记录摘要层（多行 PowerShell 折叠与查看详情、退出码并入摘要行）与连接记录同源去重（不重复显示窗口发现/应用后台发现与右侧已恢复）；
// 5. #128 第二条 连接后台服务等待图标持续动效与等待时长。

test.describe.configure({ mode: "serial" });

const REPORT_MARKDOWN = [
  "# 批 M 交付验收报告",
  "",
  "本次升级围绕 **Fluent 2 视觉质感** 与 **长报告排版可读性** 展开，覆盖 `apps/desktop` 与 `apps/android` 双端。",
  "",
  "## 一、核心改动一览",
  "",
  "1. 修正 **加粗字重被压平** 的问题：正文 `<strong>` 使用 `700` 字重，标题 `h1–h3` 逐级区分字号与留白。",
  "2. 补齐行内代码（例如 `markdown.css`、`MarkdownParser.kt`）的浅底、描边与等宽字族。",
  "   - 支持嵌套子列表与清晰的标记颜色。",
  "   - 支持宽表格横向滚动，不撑破对话列。",
  "",
  "> 验收口径：对照 Claude 本体渲染同一份长报告，扫一眼就能区分标题、重点加粗、代码与表格结构。",
  "",
  "### 1. 双端对照表",
  "",
  "| 模块 | 桌面端实现 | 安卓端实现 | 状态 | 耗时 |",
  "| :--- | :--- | :--- | :---: | ---: |",
  "| **排版层级** | `markdown.css` 统一 `700` 加粗与标题阶梯 | `MarkdownText.kt` 完整块级/行内渲染 | 通过 | `18ms` |",
  "| **表格渲染** | 粘性表头 + 斑马纹 + 边框 + 横向滚动 | 自适应列宽 `Layout` + 表头底色 + 横向滚动 | 通过 | `24ms` |",
  "| **连接动效** | `Mark variant=\"wait\"` 公转 + 呼吸 + 计时 | `ActivityIndicator` 流畅 Fluent 弧线环 | 通过 | `12ms` |",
  "",
  "### 2. 示例片段",
  "",
  "```ts",
  "export function verifyTypography(weight: number): boolean {",
  "  return weight >= 700;",
  "}",
  "```",
].join("\n");

let daemon: Daemon;
let app: ElectronApplication;
let page: Page;

const extraArgs = process.platform === "linux" ? ["--no-sandbox"] : [];

test.beforeAll(async () => {
  daemon = await startDaemon(path.join(fs.mkdtempSync(path.join(os.tmpdir(), "aide-ui132-")), "d.sock"));
  const now = Date.now();
  // 为 #135 准备同屏对照数据：Claude Code（已登录蓝点）+ DSH（未确认黄点）+ 离线工作机器（Mac mini）
  const localMachine = daemon.world.machines.find((m) => m.machineId === "m-local")!;
  localMachine.status!.agents = [
    create(AgentStatusSchema, { name: "claude", available: true, loggedIn: true, version: "2.1.3", checkedAt: timestampFromMs(now - 10_000) }),
    create(AgentStatusSchema, {
      name: "deepseek",
      available: true,
      loggedIn: false,
      loginUnconfirmed: true,
      version: "1.4.0",
      checkedAt: timestampFromMs(now - 10_000),
      dshBridge: create(DSHBridgeSchema, { state: DSHBridgeState.DSH_BRIDGE_STATE_RUNNING, version: "0.3.0", latestVersion: "0.3.0" }),
    }),
    create(AgentStatusSchema, { name: "codex", available: true, loggedIn: false, version: "1.0.0", checkedAt: timestampFromMs(now - 10_000) }),
    // 审计 B（P25）：ACP agent 的「未确认」与 DSH 同色
    create(AgentStatusSchema, { name: "gemini", available: true, loggedIn: false, loginUnconfirmed: true, version: "0.9.0", checkedAt: timestampFromMs(now - 10_000) }),
  ];
  // 离线机器设备（故意让 device.machineOnline 过期为 true，验证 Account 页与 st.machines 同源显示离线 #135 P08）
  daemon.world.devices.push(
    create(DeviceSchema, { keyId: "d-3", label: "Mac mini", platform: "darwin", machineId: "m-mini", machineOnline: true }),
  );
  // 为 Claude Code（m-local）准备 1 条已完成 + 1 条已归档的 owner 会话（验证 P03/P04/P33）
  daemon.world.sessions.set("s-owner-done", create(SessionSchema, {
    sessionId: "s-owner-done",
    machineId: "m-local",
    agent: "claude",
    projectId: "aide",
    createdBy: "owner",
    title: "测试已完成对话",
    status: SessionStatus.IDLE,
    createdAt: timestampFromMs(now - 44 * 60_000),
    updatedAt: timestampFromMs(now - 10_000),
    progress: { turnCount: 1, lastEventSeq: 1n },
  }));
  daemon.world.sessions.set("s-owner-arch", create(SessionSchema, {
    sessionId: "s-owner-arch",
    machineId: "m-local",
    agent: "claude",
    projectId: "aide",
    createdBy: "owner",
    title: "已归档历史对话",
    status: SessionStatus.ARCHIVED,
    createdAt: timestampFromMs(now - 120 * 60_000),
    updatedAt: timestampFromMs(now - 60 * 60_000),
    progress: { turnCount: 1, lastEventSeq: 1n },
  }));
  daemon.world.events.set(ids.bigIdle, [
    create(SessionEventSchema, { seq: 1n, ts: timestampFromMs(now - 5000), turnId: "t1", kind: { case: "turnStarted", value: { turn: 1 } } }),
    create(SessionEventSchema, { seq: 2n, ts: timestampFromMs(now - 4000), turnId: "t1", kind: { case: "userMessage", value: { text: "请输出一份带多级标题、加粗、行内代码、代码块和表格的验收报告。" } } }),
    create(SessionEventSchema, { seq: 3n, ts: timestampFromMs(now - 2000), turnId: "t1", kind: { case: "agentMessage", value: { text: REPORT_MARKDOWN } } }),
    create(SessionEventSchema, { seq: 4n, ts: timestampFromMs(now - 1000), turnId: "t1", kind: { case: "turnCompleted", value: { turn: 1, isError: false } } }),
  ]);
  const museRoom = create(RoomSchema, {
    roomId: "r-muse-direct",
    name: "Muse",
    hostMachineId: "m-local",
    direct: true,
    mode: RoomMode.MANUAL,
    members: [create(RoomMemberSchema, { botKey: "c:c-3", address: "c-3", name: "Muse" })],
    updatedAt: timestampFromMs(now - 30_000),
  });
  daemon.world.directRooms.set("c-3", {
    room: museRoom,
    messages: [
      create(RoomMessageSchema, {
        messageId: "m-0",
        from: "owner",
        fromName: "你",
        text: "早先一条发送失败的尝试",
        at: timestampFromMs(now - 180_000),
        deliveries: [create(RoomDeliverySchema, { botKey: "c:c-3", state: RoomDeliveryState.FAILED, reason: "没发出去：暂时无法连接" })],
      }),
      create(RoomMessageSchema, {
        messageId: "m-1",
        from: "owner",
        fromName: "你",
        text: "帮我总结一下今天的 #132 和 #134 改造验收要点。",
        at: timestampFromMs(now - 120_000),
        deliveries: [create(RoomDeliverySchema, { botKey: "c:c-3", state: RoomDeliveryState.REPLIED })],
      }),
      create(RoomMessageSchema, {
        messageId: "m-2",
        from: "c:c-3",
        fromName: "Muse",
        text: REPORT_MARKDOWN,
        at: timestampFromMs(now - 60_000),
      }),
    ],
  });
  daemon.world.execLog = [
    create(ExecLogEntrySchema, {
      machineId: "m-local",
      machineName: "MacBook Pro",
      record: create(ExecRecordSchema, {
        clientId: "c-0",
        clientLabel: "Claude",
        command: [
          "powershell -NoProfile -Command \"",
          "$ErrorActionPreference = 'Stop'",
          "# collect active service summary",
          "Get-Service | Where-Object { $_.Status -eq 'Running' } | Select-Object -First 8 Name, DisplayName",
          "$procs = Get-Process | Sort-Object CPU -Descending | Select-Object -First 5",
          "$procs | Format-Table Name, CPU -AutoSize",
          "Write-Host 'audit complete'",
          "\"",
        ].join("\n"),
        workdir: "/workspace/aide",
        outcome: ExecOutcome.EXITED,
        exitCode: 0,
        startedAt: timestampFromMs(now - 90_000),
        endedAt: timestampFromMs(now - 88_400),
      }),
    }),
    create(ExecLogEntrySchema, {
      machineId: "m-build",
      machineName: "build-server",
      record: create(ExecRecordSchema, {
        clientId: "c-3",
        clientLabel: "Muse",
        command: "git fetch origin --prune",
        outcome: ExecOutcome.EXITED,
        exitCode: 1,
        startedAt: timestampFromMs(now - 240_000),
        endedAt: timestampFromMs(now - 239_200),
      }),
    }),
    // 审计 B：被拒绝的多行脚本——折叠态也要看得到拒绝原因
    create(ExecLogEntrySchema, {
      machineId: "m-local",
      machineName: "MacBook Pro",
      record: create(ExecRecordSchema, {
        clientId: "c-0",
        clientLabel: "Claude",
        command: ["bash -c '", "set -e", "rm -rf build", "make all", "'"].join("\n"),
        outcome: ExecOutcome.REFUSED,
        reason: "这台机器已暂停，不接新的执行",
        startedAt: timestampFromMs(now - 300_000),
      }),
    }),
  ];
  ({ app, page } = await launch(daemon.socket, { args: extraArgs }));
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setContentSize(1280, 860));
});

test.afterAll(async () => {
  await app?.close();
  await daemon?.close();
});

test("工单 #134: 侧栏两态图标位置守恒、左上角不重复、清掉死空间顶栏与会话顶栏上下文", async () => {
  await expect(page.locator(".ov-tile").first()).toBeVisible({ timeout: 15_000 });
  // 1. 概览页与任务动态页不许出现空 .ctool 死空间行（#134 补充二）
  await expect(page.locator(".content .ctool")).toHaveCount(0);

  // 侧栏只剩显示与隐藏两态（工单 #141 取代 #134 的窄栏口径）：切换钮与搜索框在两态下位置守恒（#134 正文）
  const boxOf = async (loc: ReturnType<Page["locator"]>) => (await loc.boundingBox())!;
  const toggleBefore = await boxOf(page.getByRole("button", { name: "隐藏边栏" }));
  const searchBefore = await boxOf(page.locator(".side-top").getByRole("button", { name: "搜索" }));
  await snap(page, "desktop-134-sidebar-expanded");

  // 2. 隐藏侧栏：没有图标窄栏，左上角只有这一组按钮（#134 补充一），切换钮与搜索框不跳位
  await page.getByRole("button", { name: "隐藏边栏" }).click();
  await expect(page.getByRole("complementary", { name: "边栏" })).toHaveCount(0);
  await expect(page.locator("aside.rail")).toHaveCount(0);
  expect(await page.getByRole("button", { name: "菜单" }).count()).toBeLessThanOrEqual(1);
  expect(await page.getByRole("button", { name: /^(隐藏边栏|显示边栏)$/ }).count()).toBe(1);
  const expandAfter = await boxOf(page.getByRole("button", { name: "显示边栏" }));
  const searchAfter = await boxOf(page.locator(".side-top").getByRole("button", { name: "搜索" }));
  expect(Math.abs(expandAfter.x - toggleBefore.x)).toBeLessThanOrEqual(1);
  expect(Math.abs(expandAfter.y - toggleBefore.y)).toBeLessThanOrEqual(1);
  expect(Math.abs(searchAfter.x - searchBefore.x)).toBeLessThanOrEqual(1);
  expect(Math.abs(searchAfter.y - searchBefore.y)).toBeLessThanOrEqual(1);
  await snap(page, "desktop-134-sidebar-collapsed");

  // 3. 点原位切换钮显示回来
  await page.getByRole("button", { name: "显示边栏" }).click();
  await expect(page.getByRole("complementary", { name: "边栏" })).toBeVisible();
  await expect.poll(async () => Math.round((await page.locator(".side").boundingBox())!.width)).toBe(280);

  // 4. 任务动态页也不许有空 .ctool
  await navTo(page, /^任务动态/);
  await expect(page.getByRole("heading", { name: "任务动态" })).toBeVisible();
  await expect(page.locator(".content .ctool")).toHaveCount(0);
  await navTo(page, /^概览/);
});

test("工单 #132: 会话页 Markdown 排版层级、真实加粗、行内代码、表格渲染与顶栏上下文（浅/深双色）", async () => {
  await expect(page.locator(".ov-tile").first()).toBeVisible({ timeout: 15_000 });
  await snap(page, "desktop-overview-polish-light");
  await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
  await snap(page, "desktop-overview-polish-dark");
  await page.evaluate(() => document.documentElement.removeAttribute("data-theme"));

  await sessionList(page);
  await page.locator(".srow", { has: page.locator(".stitle", { hasText: titles.bigIdle }) }).first().click();
  await expect(page.locator(".ctitle h2")).toHaveText(titles.bigIdle);

  // 会话页顶栏应承载 agent 上下文，且在 Windows 模式下右侧按钮贴齐右缘（不留 146px 死空间）
  await expect(page.locator(".ctool .ctool-meta")).toBeVisible();
  const ctoolRightGap = await page.evaluate(() => {
    const prev = document.documentElement.getAttribute("data-platform");
    document.documentElement.setAttribute("data-platform", "win");
    const ctool = document.querySelector(".ctool")!.getBoundingClientRect();
    const btns = [...document.querySelectorAll(".ctool > button.tb")];
    const last = btns.at(-1)!.getBoundingClientRect();
    if (prev) document.documentElement.setAttribute("data-platform", prev);
    else document.documentElement.removeAttribute("data-platform");
    return ctool.right - last.right;
  });
  expect(ctoolRightGap).toBeLessThanOrEqual(24);

  const md = page.locator(".am .md").first();
  await expect(md.locator("h1")).toHaveText("批 M 交付验收报告");
  await expect(md.locator("table thead th")).toHaveCount(5);
  await expect(md.locator("table tbody tr")).toHaveCount(3);
  await md.locator(".cblock .hl").first().waitFor({ timeout: 5_000 }).catch(() => {});

  const metrics = await md.evaluate((root) => {
    const h1 = getComputedStyle(root.querySelector("h1")!);
    const h2 = getComputedStyle(root.querySelector("h2")!);
    const h3 = getComputedStyle(root.querySelector("h3")!);
    const p = getComputedStyle(root.querySelector("p")!);
    const strong = getComputedStyle(root.querySelector("p strong")!);
    const inlineCode = getComputedStyle(root.querySelector("p code")!);
    const preCode = getComputedStyle(root.querySelector("pre code")!);
    const th = getComputedStyle(root.querySelector("thead th")!);
    const scroller = root.closest(".cscroll") as HTMLElement;
    return {
      h1Size: parseFloat(h1.fontSize),
      h2Size: parseFloat(h2.fontSize),
      h3Size: parseFloat(h3.fontSize),
      pSize: parseFloat(p.fontSize),
      h1Weight: Number(h1.fontWeight),
      h2Weight: Number(h2.fontWeight),
      strongWeight: Number(strong.fontWeight),
      inlineCodeBg: inlineCode.backgroundColor,
      inlineCodeBorder: parseFloat(inlineCode.borderTopWidth),
      preCodeBg: preCode.backgroundColor,
      thWeight: Number(th.fontWeight),
      thBg: th.backgroundColor,
      chatOverflowX: scroller.scrollWidth - scroller.clientWidth,
    };
  });

  expect(metrics.h1Size).toBeGreaterThan(metrics.h2Size);
  expect(metrics.h2Size).toBeGreaterThan(metrics.h3Size);
  expect(metrics.h3Size).toBeGreaterThan(metrics.pSize);
  expect(metrics.h1Weight).toBeGreaterThanOrEqual(700);
  expect(metrics.h2Weight).toBeGreaterThanOrEqual(700);
  expect(metrics.strongWeight).toBeGreaterThanOrEqual(700);
  expect(metrics.inlineCodeBg).not.toBe("rgba(0, 0, 0, 0)");
  expect(metrics.inlineCodeBorder).toBeGreaterThan(0);
  expect(metrics.preCodeBg).toBe("rgba(0, 0, 0, 0)");
  expect(metrics.thWeight).toBeGreaterThanOrEqual(700);
  expect(metrics.thBg).not.toBe("rgba(0, 0, 0, 0)");
  expect(metrics.chatOverflowX).toBeLessThanOrEqual(2);

  await snap(page, "desktop-markdown-report-light");

  // Manual theme toggle: data-theme="dark" must update all tokens including brand.css and overview.css.
  await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
  const darkTokens = await page.evaluate(() => {
    const s = getComputedStyle(document.documentElement);
    return {
      page: s.getPropertyValue("--surface-page").trim().toLowerCase(),
      tableHead: s.getPropertyValue("--surface-table-head").trim().toLowerCase(),
      accentFg: s.getPropertyValue("--accent-fg").trim().toLowerCase(),
      heat1: s.getPropertyValue("--heat1").trim().toLowerCase(),
    };
  });
  expect(darkTokens.page).toBe("#1b1a19");
  expect(darkTokens.tableHead).toBe("#292827");
  expect(darkTokens.accentFg).toBe("#479ef5");
  expect(darkTokens.heat1).toBe("#363a45");

  await snap(page, "desktop-markdown-report-dark");
  await page.evaluate(() => document.documentElement.removeAttribute("data-theme"));
});

test("工单 #135: 状态口径统一（P03/P04/P08/P17/P25/P33）", async () => {
  await navTo(page, /^概览/);
  await expect(page.locator(".ov-tile").first()).toBeVisible({ timeout: 15_000 });

  // P17 + P25：展开侧栏中 ChatGPT(在线=绿) / Claude Code(已登录=蓝) / DSH(未确认=黄) / Codex(未登录=红) 四态点色与文字一一对应
  const sidebar = page.locator(".side-bots");
  await expect(sidebar.locator(".bot-row", { hasText: "DSH" }).first().locator(".bot-word")).toHaveText("未确认");
  const dotsExpanded = await sidebar.evaluate((root) => {
    const pick = (name: string) => {
      const row = [...root.querySelectorAll(".bot-row")].find((el) => el.querySelector(".bot-l1 b")?.textContent?.trim() === name);
      const dot = row?.querySelector(".dotc") as HTMLElement | null;
      return {
        cls: dot?.className ?? "",
        bg: dot ? getComputedStyle(dot).backgroundColor : "",
        word: row?.querySelector(".bot-word")?.textContent?.trim() ?? "",
      };
    };
    return {
      chatgpt: pick("ChatGPT"),
      claude: pick("Claude Code"),
      dsh: pick("DSH"),
      codex: pick("Codex"),
    };
  });
  expect(dotsExpanded.chatgpt.cls).toContain("on");
  expect(dotsExpanded.claude.cls).toContain("ready");
  expect(dotsExpanded.dsh.cls).toContain("warn");
  expect(dotsExpanded.codex.cls).toContain("bad");
  // 三种正常/半正常状态颜色必须互不相同（P25）
  expect(new Set([dotsExpanded.chatgpt.bg, dotsExpanded.claude.bg, dotsExpanded.dsh.bg, dotsExpanded.codex.bg]).size).toBe(4);


  // 审计 B（P25）：同一文字同一点色，页页相同——侧栏 MacBook Pro 下每个 bot 的「文字 → 点色」与机器页（设置 · 机器）
  // 逐一相同；「未确认」DSH 与 ACP agent（Gemini）同为黄点，「已登录」在两页都是蓝点。
  // [文字, 点色 class] for each item under root: the dot inside the word's element, else the item's first.
  const wordDots = (root: Locator, item: string, word: string) =>
    root.evaluate(
      (el, [item, word]) =>
        [...el.querySelectorAll(item)].map((x) => {
          const w = x.querySelector(word);
          const d = w?.querySelector(".dotc") ?? x.querySelector(".dotc");
          return [w?.textContent?.trim() ?? "", d ? d.className.replace("dotc", "").trim() : "?"] as [string, string];
        }),
      [item, word] as const,
    );
  const sideDots = await wordDots(sidebar.getByRole("group", { name: "MacBook Pro" }), ".bot-row", ".bot-word");
  const sideMap = Object.fromEntries(sideDots);
  expect(sideDots.filter(([w]) => w === "未确认").length).toBeGreaterThanOrEqual(2);
  expect(new Set(sideDots.filter(([w]) => w === "未确认").map(([, d]) => d))).toEqual(new Set(["warn"]));
  expect(sideMap["已登录"]).toBe("ready");
  expect(sideMap["未登录"]).toBe("bad");
  {
    const [machinesWin] = await Promise.all([app.waitForEvent("window"), page.evaluate(() => window.aide?.openSettings("machines"))]);
    try {
      const panel = machinesWin.getByRole("main", { name: "机器" });
      await panel.getByRole("list", { name: "机器" }).getByRole("button", { name: /^MacBook Pro/ }).click();
      const agentsList = panel.getByRole("list", { name: "编码 Agent" });
      await expect(agentsList.locator(".ag-state").first()).toBeVisible();
      const machineDots = await wordDots(agentsList, ".ag", ".ag-state");
      expect(machineDots.length).toBeGreaterThanOrEqual(4);
      for (const [w, d] of machineDots) expect([w, d]).toEqual([w, sideMap[w]]);
    } finally {
      await machinesWin.close();
    }
  }

  // P03 + P04 + P33：打开 MacBook Pro 下的 Claude Code 主页
  const macGroup = sidebar.getByRole("group", { name: "MacBook Pro" });
  await macGroup.locator(".bot-row", { hasText: "Claude Code" }).click();
  const convHead = page.locator(".conv-head");
  // P03：已完成对话不报「正在聊：」，报「最近对话：」，且状态点为绿点 .sdot.d（非灰点 .sdot.i）
  await expect(convHead).toContainText("最近对话：");
  await expect(convHead).not.toContainText("正在聊：");
  await expect(page.locator(".conv-current-card .sdot.d")).toHaveCount(1);
  // P33：主页卡片下方写清总计数拆分（已归档另成一组、默认收起，与工单 #66/#67 规则 6 一致，计数说明讲清口径）
  await expect(page.locator(".conv-count-hint")).toContainText("当前展示 1 条");
  await expect(page.locator(".conv-count-hint")).toContainText("已归档");
  await snap(page, "desktop-135-bot-completed");

  const allBtn = page.locator(".conv-all-btn");
  await allBtn.click();
  const flyout = page.locator(".conv-flyout");
  await expect(flyout).toBeVisible();
  await expect(flyout.getByText("已归档历史对话")).toHaveCount(0);
  await flyout.getByRole("button", { name: /^已归档（1）/ }).click();
  await expect(flyout.getByText("已归档历史对话")).toBeVisible();

  // P04：切换到已归档对话，顶条须报「已归档对话：」而非「正在聊：」，且卡片不再挂「当前」徽标
  await flyout.locator(".conv-row", { hasText: "已归档历史对话" }).locator(".conv-main").click();
  await expect(convHead).toContainText("已归档对话：");
  await expect(convHead).not.toContainText("正在聊：");
  await expect(page.locator(".conv-current-card .conv-badge")).toHaveCount(0);
  await snap(page, "desktop-135-bot-archived");

  // P08：账号与设备页中离线机器显示「工作机器 · 离线」，不再误报「也在执行任务」
  const [settingsWin] = await Promise.all([
    app.waitForEvent("window"),
    page.evaluate(() => window.aide?.openSettings("account")),
  ]);
  try {
    const devList = settingsWin.getByRole("list", { name: "你的设备" });
    const miniRow = devList.getByRole("listitem").filter({ hasText: "Mac mini" });
    await expect(miniRow).toContainText("工作机器 · 离线", { timeout: 10_000 });
    await expect(miniRow).not.toContainText("也在执行任务");
    await snap(settingsWin, "desktop-135-account-devices");
  } finally {
    await settingsWin.close();
  }
});

test("工单 #132 补充 P28 + #135 P02: 个人 agent 主页统一版式、时间戳与失败消息视觉隔离", async () => {
  await page.locator(".side-bots .bot-row", { hasText: "Muse" }).first().click();
  const log = page.locator(".bot-chat .chat-log");
  await expect(log.locator(".chat-msg")).toHaveCount(3, { timeout: 10_000 });

  // #135 P02：发送失败的消息气泡有 .failed 隔离态（不再与已回复的蓝气泡长得一模一样），且已回复消息保留正常蓝气泡
  await expect(log.locator(".chat-msg.mine.failed")).toHaveCount(1);
  await expect(log.locator(".chat-msg.mine:not(.failed)")).toHaveCount(1);

  // .chat-who 只含发送者名（不破坏既有 batchj-d 断言），时间戳以同级 .chat-time 呈现，且有 .tdiv 时间分隔线
  await expect(log.locator(".chat-msg:not(.mine) .chat-who")).toHaveText("Muse");
  await expect(log.locator(".tdiv").first()).toBeVisible();
  await expect(log.locator(".chat-msg.mine .chat-time").first()).toBeVisible();
  await expect(log.locator(".chat-msg:not(.mine) .chat-time")).toBeVisible();

  const layout = await log.evaluate((el) => {
    const col = el.querySelector(".chat-col") as HTMLElement;
    const agentBubble = el.querySelector(".chat-msg:not(.mine) .chat-bubble") as HTMLElement;
    const okMineBubble = el.querySelector(".chat-msg.mine:not(.failed) .chat-bubble") as HTMLElement;
    const failedMineBubble = el.querySelector(".chat-msg.mine.failed .chat-bubble") as HTMLElement;
    const strong = agentBubble.querySelector("p strong") as HTMLElement;
    const th = agentBubble.querySelector("thead th") as HTMLElement;
    return {
      colMaxWidth: parseFloat(getComputedStyle(col).maxWidth),
      agentBg: getComputedStyle(agentBubble).backgroundColor,
      agentWidthRatio: agentBubble.getBoundingClientRect().width / col.getBoundingClientRect().width,
      okMineBg: getComputedStyle(okMineBubble).backgroundColor,
      failedMineBg: getComputedStyle(failedMineBubble).backgroundColor,
      strongWeight: Number(getComputedStyle(strong).fontWeight),
      thWeight: Number(getComputedStyle(th).fontWeight),
    };
  });

  expect(layout.colMaxWidth).toBe(760);
  expect(layout.agentBg).toBe("rgba(0, 0, 0, 0)");
  expect(layout.agentWidthRatio).toBeGreaterThan(0.85);
  expect(layout.okMineBg).toBe("rgb(0, 120, 212)");
  expect(layout.failedMineBg).not.toBe(layout.okMineBg);
  expect(layout.strongWeight).toBeGreaterThanOrEqual(700);
  expect(layout.thWeight).toBeGreaterThanOrEqual(700);

  await log.evaluate((el) => { el.scrollTop = 0; });
  await snap(page, "desktop-132-personal-agent-light");
  await log.evaluate((el) => { el.scrollTop = el.scrollHeight; });
  await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
  await snap(page, "desktop-132-personal-agent-dark");
  await page.evaluate(() => document.documentElement.removeAttribute("data-theme"));
});

test("工单 #138: 执行记录摘要层、折叠详情与连接记录同源去重（P14/P15）", async () => {
  const now = Date.now();
  // 在首次读取 link-log 前写入 2 对成对重复的 app+window 事件（4 条原始记录 -> 应合并为 2 条）
  const userData = await app.evaluate(({ app: electronApp }) => electronApp.getPath("userData"));
  fs.writeFileSync(
    path.join(userData, "link-log.json"),
    JSON.stringify([
      { at: now - 35_000, link: "aided", kind: "down", why: "lost", detail: "connect ECONNREFUSED", from: "app" },
      { at: now - 34_700, link: "aided", kind: "down", why: "lost", from: "window" },
      { at: now - 30_000, link: "aided", kind: "back", downFor: 5_000, from: "app" },
      { at: now - 29_600, link: "aided", kind: "back", downFor: 5_100, from: "window" },
    ]),
  );

  const [settings] = await Promise.all([
    app.waitForEvent("window"),
    page.evaluate(() => window.aide?.openSettings("ops")),
  ]);
  try {
    await settings.waitForLoadState();
    const opsList = settings.getByRole("list", { name: "执行记录" });
    await expect(opsList.getByRole("listitem")).toHaveCount(3, { timeout: 15_000 });

    // P14：默认折叠态显示人话摘要 + 退出码并入摘要行，不铺满原始 PowerShell 脚本
    const psRow = opsList.getByRole("listitem").nth(0);
    await expect(psRow.locator(".ops-summary")).toHaveText("PowerShell 脚本（8 行）");
    await expect(psRow.locator(".ops-outcome")).toHaveText("成功（退出码 0）");
    await expect(psRow.locator(".ops-preview-sub")).toContainText("Get-Service | Where-Object");
    await expect(psRow.locator(".ops-script")).toHaveCount(0);
    await expect(psRow.locator(".fv-side")).toHaveCount(0);

    const failRow = opsList.getByRole("listitem").nth(1);
    await expect(failRow.locator(".ops-preview")).toHaveText("git fetch origin --prune");
    await expect(failRow.locator(".ops-outcome")).toHaveText("失败（退出码 1）");

    // 审计 B：折叠的脚本被拒绝时，摘要行仍给出拒绝原因
    const refusedRow = opsList.getByRole("listitem").filter({ hasText: "这台机器已暂停" });
    await expect(refusedRow.locator(".ops-script")).toHaveCount(0);
    await expect(refusedRow.locator(".ops-reason")).toHaveText("这台机器已暂停，不接新的执行");

    // P15：成对重复的连接记录合并成 2 条，不出现「窗口发现」「应用后台发现」与右侧同义「已恢复/断开」
    const linkList = settings.getByRole("list", { name: "这台电脑的连接" });
    await expect(linkList.getByRole("listitem")).toHaveCount(2);
    await expect(linkList.getByRole("listitem").nth(0)).toContainText("已自动重新连上这台电脑上的 Aide 后台");
    await expect(linkList.getByRole("listitem").nth(1)).toContainText(/Aide 后台停了或正在重启/);
    await expect(linkList).not.toContainText("窗口发现");
    await expect(linkList).not.toContainText("应用后台发现");
    await expect(linkList.locator(".fv-side")).toHaveCount(0);

    await snap(settings, "desktop-138-ops-collapsed");

    // 展开详情：看到完整原始脚本、目录与复制按钮
    await psRow.getByRole("button", { name: "查看详情" }).click();
    await expect(psRow.locator(".ops-script")).toBeVisible();
    await expect(psRow.locator(".ops-script")).toContainText("Write-Host 'audit complete'");
    await expect(psRow.locator(".ops-detail")).toContainText("目录：/workspace/aide");
    await expect(psRow.getByRole("button", { name: "复制完整命令" })).toBeVisible();
    await snap(settings, "desktop-138-ops-expanded");

    // 收起详情
    await psRow.getByRole("button", { name: "收起详情" }).click();
    await expect(psRow.locator(".ops-script")).toHaveCount(0);
  } finally {
    await settings.close();
  }
});

test("工单 #128 第二条: 连接后台服务屏等待图标持续动效且超时后显示已等待时长", async () => {
  const deadSocket = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "aide-dead-")), "missing.sock");
  const { app: deadApp, page: deadPage } = await launch(deadSocket, { args: extraArgs, start: true });
  try {
    await expect(deadPage.getByText("正在连接后台服务…")).toBeVisible({ timeout: 15_000 });
    const mark = deadPage.locator(".hm-mark.hm-wait");
    await expect(mark).toBeVisible();

    const anims = await mark.evaluate((el) =>
      [el.querySelector(".hm-orbit"), el.querySelector(".hm-core"), el.querySelector(".hm-sat")].map((node) =>
        (node?.getAnimations() ?? []).map((a) => ({
          state: a.playState,
          props: [...new Set((a.effect as KeyframeEffect).getKeyframes().flatMap((k) => Object.keys(k)))].filter(
            (p) => !["offset", "computedOffset", "easing", "composite"].includes(p),
          ),
        })),
      ),
    );
    for (const group of anims) {
      expect(group.length).toBeGreaterThan(0);
      expect(group.every((a) => a.state === "running")).toBe(true);
      expect(group.every((a) => a.props.every((p) => p === "transform" || p === "opacity"))).toBe(true);
    }

    await expect(deadPage.getByText(/已等待 \d+ 秒/)).toBeVisible({ timeout: 6_000 });
    await snap(deadPage, "desktop-connecting-wait");
  } finally {
    await deadApp.close();
  }
});
