import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fixture, launch, snap } from "./app";

// DeepSeek 会话 › taking one over (docs/dsh-takeover.md): read only until the owner turns 接管 on
// for it (asked first, with what it means); then a message goes to DSH through the Aide plugin
// (here the real plugin in a stand-in DSH Host, which writes it into the session as DSH would) and
// comes back marked as sent from Aide. With DSH Desktop gone, the send fails saying so and the
// text stays. Off again, read only.

test.describe.configure({ timeout: 120_000 });

test("taking over a DSH session: off by default, asked, a message through the plugin, DSH Desktop gone, off again", async () => {
  const { app, page } = await launch();
  await page.getByRole("button", { name: "DeepSeek 会话" }).click();
  const list = page.getByRole("navigation", { name: "DeepSeek 会话" });
  await list.getByRole("button", { name: /看一下 README/ }).click({ timeout: 30_000 });
  await expect(page.getByText("README 写的是安装步骤。")).toBeVisible();

  // Read only: the switch is off, no box to write in.
  const takeover = page.getByRole("switch", { name: "接管" });
  await expect(takeover).toHaveAttribute("aria-checked", "false");
  await expect(page.getByRole("textbox", { name: "给 DSH 的消息" })).toHaveCount(0);

  // Turning it on asks first; Cancel leaves it off.
  await takeover.click();
  const ask = page.getByRole("dialog", { name: "开启接管？" });
  await expect(ask).toContainText("会话正在运行时，消息会排到这一轮之后");
  await ask.getByRole("button", { name: "取消" }).click();
  await expect(takeover).toHaveAttribute("aria-checked", "false");
  await takeover.click();
  await expect(takeover).toHaveAttribute("aria-checked", "false"); // until confirmed
  await snap(page, "dsh-takeover-ask");
  await ask.getByRole("button", { name: "开启接管" }).click();
  await expect(takeover).toHaveAttribute("aria-checked", "true");

  // The plugin runs: a message goes, and comes back in the conversation marked as from Aide.
  const box = page.getByRole("textbox", { name: "给 DSH 的消息" });
  await expect(box).toBeVisible();
  await expect(page.getByText("DSH Desktop 里还没有 Aide 插件")).toHaveCount(0);
  await box.fill("接着把测试补上");
  await page.getByRole("button", { name: "发送" }).click();
  await expect(page.getByRole("status").filter({ hasText: "已发送。" })).toBeVisible({ timeout: 30_000 });
  await expect(box).toHaveValue("");
  const sent = page.locator(".dsh-msg.user").filter({ hasText: "接着把测试补上" });
  await expect(sent).toBeVisible({ timeout: 30_000 });
  await expect(sent).toContainText("经 Aide 发送");
  await expect(page.locator(".dsh-msg.user").filter({ hasText: "看一下 README" })).not.toContainText("经 Aide 发送");
  await snap(page, "dsh-takeover-sent");

  // DSH Desktop quits: the send says what to do, nothing goes, the text stays.
  process.kill(fixture.dshHost!, "SIGKILL");
  await box.fill("再看一下 CI");
  await page.getByRole("button", { name: "发送" }).click();
  await expect(page.getByRole("alert")).toContainText("没有发出去：", { timeout: 30_000 });
  await expect(page.getByRole("alert")).toContainText("does not answer");
  await expect(box).toHaveValue("再看一下 CI");
  await expect(page.getByRole("status").filter({ hasText: "DSH Desktop 里的 Aide 插件没有响应，发不了消息。" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("button", { name: "发送" })).toBeDisabled();
  await snap(page, "dsh-takeover-gone");

  // Never installed (no rendezvous): preparing backs up DSH's configuration and writes the package,
  // for DSH Desktop's Plugins page to install.
  fs.rmSync(path.join(fixture.dshHome!, "aide-bridge", "bridge.json"));
  await expect(page.getByRole("status").filter({ hasText: "DSH Desktop 里还没有 Aide 插件，发不了消息。" })).toBeVisible({ timeout: 30_000 });
  await page.getByRole("button", { name: "准备安装…" }).click();
  const install = page.getByRole("region", { name: "安装 Aide 插件" });
  await expect(install).toContainText("已把 DSH 的配置文件", { timeout: 30_000 });
  await expect(install).toContainText("没有在 DSH 目录的 profiles/desktop 下找到"); // this DSH home has no Desktop profile
  const pkg = (await install.locator(".dsh-path .mono").textContent()) ?? "";
  expect(pkg.endsWith(".tgz")).toBe(true);
  expect(fs.statSync(pkg).size).toBeGreaterThan(0);
  await snap(page, "dsh-takeover-install");

  // Off again: read only.
  await takeover.click();
  await expect(takeover).toHaveAttribute("aria-checked", "false");
  await expect(box).toHaveCount(0);
  await app.close();
});
