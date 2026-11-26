import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { RoomMemberSchema, RoomMessageSchema, RoomMode, RoomSchema } from "@aide/proto/machine";
import { SessionEventSchema } from "@aide/proto/session";
import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { startDaemon, type Daemon } from "../perf/daemon";
import { ids, titles } from "../perf/world";
import { launch, navTo, sessionList, snap } from "./app";

// 工单 #132（含补充 P28）+ 工单 #134（含补充一/二）+ Fluent 2 视觉与动效回归：
// 1. 会话页 Markdown 排版层级、真实 700 加粗、行内代码、代码块与 GFM 表格渲染（浅/深双色）；
// 2. #134 侧栏展开/收起图标位置守恒（切换钮/概览/任务动态/搜索/账号）、左上角菜单不重复、概览/任务动态无空顶栏、会话页顶栏带 agent 上下文且按钮贴齐右缘；
// 3. #132 P28 个人 agent 主页（Muse）与会话页统一居中阅读列、无边框 Markdown 回复流、主蓝右气泡、时间分隔线与消息时间戳、且 .chat-who 文本不被污染；
// 4. #128 第二条 连接后台服务等待图标持续动效与等待时长。

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
        messageId: "m-1",
        from: "owner",
        fromName: "你",
        text: "帮我总结一下今天的 #132 和 #134 改造验收要点。",
        at: timestampFromMs(now - 120_000),
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

  // 记录展开态各固定导航控件的坐标
  const boxOf = async (loc: ReturnType<Page["locator"]>) => (await loc.boundingBox())!;
  const toggleBefore = await boxOf(page.getByRole("button", { name: "收成窄栏" }));
  const searchBefore = await boxOf(page.locator(".side-top").getByRole("button", { name: "搜索" }));
  const overviewBefore = await boxOf(page.locator(".side-nav button").nth(0));
  const tasksBefore = await boxOf(page.locator(".side-nav button").nth(1));
  const accountBefore = await boxOf(page.getByRole("button", { name: "账号与设置" }));
  await snap(page, "desktop-134-sidebar-expanded");

  // 2. 点击收起成窄栏：切换钮、搜索、概览、任务动态、账号锚点必须守恒（不从左上角跳到左下角）
  await page.getByRole("button", { name: "收成窄栏" }).click();
  await expect(page.getByRole("complementary", { name: "窄栏" })).toBeVisible();
  // 等宽度过渡完成
  await expect.poll(async () => Math.round((await page.locator(".rail").boundingBox())!.width)).toBe(72);

  // 窄栏底栏只保留账号，不允许再塞入重复的搜索钮或展开钮
  await expect(page.locator(".rail-foot button")).toHaveCount(1);
  await expect(page.locator(".rail-top")).toHaveCount(0);
  // 窗口左上角最多只有 1 个菜单按钮（Windows）且绝不重复
  expect(await page.getByRole("button", { name: "菜单" }).count()).toBeLessThanOrEqual(1);

  const expandAfter = await boxOf(page.getByRole("button", { name: "展开边栏" }));
  const searchAfter = await boxOf(page.locator(".side-top").getByRole("button", { name: "搜索" }));
  const overviewAfter = await boxOf(page.locator(".rail-nav > button.rb").nth(0));
  const tasksAfter = await boxOf(page.locator(".rail-nav > button.rb").nth(1));
  const accountAfter = await boxOf(page.getByRole("button", { name: "账号与设置" }));

  // 切换钮与搜索框：x / y 完全守恒
  expect(Math.abs(expandAfter.x - toggleBefore.x)).toBeLessThanOrEqual(1);
  expect(Math.abs(expandAfter.y - toggleBefore.y)).toBeLessThanOrEqual(1);
  expect(Math.abs(searchAfter.x - searchBefore.x)).toBeLessThanOrEqual(1);
  expect(Math.abs(searchAfter.y - searchBefore.y)).toBeLessThanOrEqual(1);
  // 概览、任务动态、账号：垂直中心点 Y 守恒（≤ 2px）
  const cy = (b: { y: number; height: number }) => b.y + b.height / 2;
  expect(Math.abs(cy(overviewAfter) - cy(overviewBefore))).toBeLessThanOrEqual(2);
  expect(Math.abs(cy(tasksAfter) - cy(tasksBefore))).toBeLessThanOrEqual(2);
  expect(Math.abs(cy(accountAfter) - cy(accountBefore))).toBeLessThanOrEqual(2);
  await snap(page, "desktop-134-sidebar-collapsed");

  // 3. 点原位切换钮展开回来
  await page.getByRole("button", { name: "展开边栏" }).click();
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

test("工单 #132 补充 P28: 个人 agent 主页与会话页统一居中阅读列、无边框 Markdown 回复流与时间戳", async () => {
  await page.locator(".side-bots .bot-row", { hasText: "Muse" }).first().click();
  const log = page.locator(".bot-chat .chat-log");
  await expect(log.locator(".chat-msg")).toHaveCount(2, { timeout: 10_000 });

  // .chat-who 只含发送者名（不破坏既有 batchj-d 断言），时间戳以同级 .chat-time 呈现，且有 .tdiv 时间分隔线
  await expect(log.locator(".chat-msg:not(.mine) .chat-who")).toHaveText("Muse");
  await expect(log.locator(".tdiv").first()).toBeVisible();
  await expect(log.locator(".chat-msg.mine .chat-time")).toBeVisible();
  await expect(log.locator(".chat-msg:not(.mine) .chat-time")).toBeVisible();

  const layout = await log.evaluate((el) => {
    const col = el.querySelector(".chat-col") as HTMLElement;
    const agentBubble = el.querySelector(".chat-msg:not(.mine) .chat-bubble") as HTMLElement;
    const mineBubble = el.querySelector(".chat-msg.mine .chat-bubble") as HTMLElement;
    const strong = agentBubble.querySelector("p strong") as HTMLElement;
    const th = agentBubble.querySelector("thead th") as HTMLElement;
    return {
      colMaxWidth: parseFloat(getComputedStyle(col).maxWidth),
      agentBg: getComputedStyle(agentBubble).backgroundColor,
      agentWidthRatio: agentBubble.getBoundingClientRect().width / col.getBoundingClientRect().width,
      mineBg: getComputedStyle(mineBubble).backgroundColor,
      strongWeight: Number(getComputedStyle(strong).fontWeight),
      thWeight: Number(getComputedStyle(th).fontWeight),
    };
  });

  expect(layout.colMaxWidth).toBe(760);
  expect(layout.agentBg).toBe("rgba(0, 0, 0, 0)");
  expect(layout.agentWidthRatio).toBeGreaterThan(0.85);
  expect(layout.mineBg).toBe("rgb(0, 120, 212)");
  expect(layout.strongWeight).toBeGreaterThanOrEqual(700);
  expect(layout.thWeight).toBeGreaterThanOrEqual(700);

  await log.evaluate((el) => { el.scrollTop = 0; });
  await snap(page, "desktop-132-personal-agent-light");
  await log.evaluate((el) => { el.scrollTop = el.scrollHeight; });
  await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
  await snap(page, "desktop-132-personal-agent-dark");
  await page.evaluate(() => document.documentElement.removeAttribute("data-theme"));
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
