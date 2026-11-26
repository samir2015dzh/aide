import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { SessionEventSchema } from "@aide/proto/session";
import { expect, test, type ElectronApplication, type Page } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { startDaemon, type Daemon } from "../perf/daemon";
import { ids, titles } from "../perf/world";
import { launch, sessionList, snap } from "./app";

// 工单 #132 + Fluent 2 UI/motion polish: verifies Markdown typography hierarchy (h1-h4, bold 700,
// inline code badge, fenced code block, aligned GFM table without horizontal overflow), theme token
// sync for manual light/dark toggle, compositor-only page-stage entrance, and #128 item 2 animated
// Mark wait indicator + elapsed wait time on the connecting screen.

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
  ({ app, page } = await launch(daemon.socket, { args: extraArgs }));
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setContentSize(1280, 860));
});

test.afterAll(async () => {
  await app?.close();
  await daemon?.close();
});

test("工单 #132: Markdown 排版层级、真实加粗、行内代码与表格渲染（浅/深双色）", async () => {
  await expect(page.locator(".ov-tile").first()).toBeVisible({ timeout: 15_000 });
  await snap(page, "desktop-overview-polish-light");
  await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
  await snap(page, "desktop-overview-polish-dark");
  await page.evaluate(() => document.documentElement.removeAttribute("data-theme"));

  await sessionList(page);
  await page.locator(".srow", { has: page.locator(".stitle", { hasText: titles.bigIdle }) }).first().click();
  await expect(page.locator(".ctitle h2")).toHaveText(titles.bigIdle);

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
