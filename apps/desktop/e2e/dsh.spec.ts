import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fixture, launch, snap, navTo, pick } from "./app";

// DSH 会话 › taking one over (docs/dsh-takeover.md): read only until the owner turns 接管 on
// for it (asked first, with what it means); then a message goes to DSH through the Aide plugin
// (here the real plugin in a stand-in DSH Host, which writes it into the session as DSH would) and
// comes back marked as sent from Aide. With DSH Desktop gone, the send fails saying so and the
// text stays. Off again, read only. With no session of DSH's found, the page names the DSH home it read.

test.describe.configure({ timeout: 120_000 });

// This computer not turned on as a machine (Settings › Machines, its sharing switch): it is none of the
// owner's machines then, so its sessions are not listed. The pages that list them (this one, and the Claude
// app's) say so instead of looking empty, with the button that turns it on; turned on, the sessions are back.
// First in this file: the test after it ends with DSH's sessions gone. Whatever happens here, this computer is
// a machine again when it ends, as the next test needs.
test("this computer not turned on as a machine: the session pages say so, and turn it on", async () => {
  const { app, page } = await launch();
  const call = (method: string, body: object) =>
    page.evaluate(
      async ([m, b]) => (await fetch(`app://aide/api/aide.local.v1.LocalService/${m}`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(b) })).json(),
      [method, body] as const,
    );
  await navTo(page, /^DSH 会话/);
  const list = page.getByRole("navigation", { name: "DSH 会话" });
  const row = list.getByRole("button", { name: /看一下 README/ });
  await expect(row).toBeVisible({ timeout: 30_000 });
  const said = "这台电脑还没有作为机器开启";
  await expect(list.getByRole("status").filter({ hasText: said })).toHaveCount(0);

  const status = (await call("GetStatus", {})) as { localMachineName?: string };
  const name = status.localMachineName ?? "";
  await call("SetLocalMachine", { enabled: false, machineName: name });
  try {
    await expect(list.getByRole("status").filter({ hasText: said })).toBeVisible({ timeout: 30_000 });
    await snap(page, "dsh-local-machine-off");
    // The account menu's entry is there only while a machine is found to have DSH (the owner's ruling, 2026-11-21:
    // not set up, not shown). This computer was the only one, so the entry goes; the page that is open stays,
    // with the notice. The entry is back once this computer is on and its agents are found again.
    const entry = async () => {
      await page.locator(".side-foot").first().click();
      const n = await page.getByRole("menu", { name: "账号与设置" }).getByRole("menuitem", { name: /^DSH 会话/ }).count();
      await page.keyboard.press("Escape");
      return n;
    };
    await expect.poll(entry, { timeout: 30_000 }).toBe(0);

    await list.getByRole("status").filter({ hasText: said }).getByRole("button", { name: "在这台电脑上开启" }).click();
    await expect(list.getByRole("status").filter({ hasText: said })).toHaveCount(0, { timeout: 30_000 });
    await expect(row).toBeVisible({ timeout: 30_000 });
    await expect.poll(entry, { timeout: 30_000 }).toBe(1);
  } finally {
    await call("SetLocalMachine", { enabled: true, machineName: name }).catch(() => {});
    await app.close();
  }
});

test("taking over a DSH session: off by default, asked, a message through the plugin, DSH Desktop gone, off again", async () => {
  const { app, page } = await launch();
  await navTo(page, /^DSH 会话/);
  const list = page.getByRole("navigation", { name: "DSH 会话" });
  await list.getByRole("button", { name: /看一下 README/ }).click({ timeout: 30_000 });
  await expect(page.getByText("README 写的是安装步骤。")).toBeVisible();

  // Read only: the switch is off, in the message area, whose box is there but can't be written in
  // (the owner's ruling, 2026-11-21: the area never disappears, the switch is beside sending).
  const takeover = page.getByRole("switch", { name: "接管" });
  await expect(takeover).toHaveAttribute("aria-checked", "false");
  const box = page.getByRole("textbox", { name: "给 DSH 的消息" });
  await expect(box).toBeDisabled();
  await expect(box).toHaveAttribute("placeholder", "开启接管后可向此会话发送消息");
  await expect(page.locator(".dsh-head").getByRole("switch")).toHaveCount(0);

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
  await expect(box).toBeEnabled();
  await expect(page.getByText("DSH Desktop 里还没有 Aide 插件")).toHaveCount(0);
  // What the session goes on with: DSH's models (with their efforts) and permission presets, as the plugin lists them.
  const model = page.getByRole("button", { name: "模型", exact: true });
  const perm = page.getByRole("button", { name: "权限", exact: true });
  await expect(model).toBeVisible({ timeout: 30_000 });
  await pick(model, "DeepSeek V4 · high");
  await pick(perm, /^Auto review/);
  await snap(page, "dsh-takeover-picks");
  // Shift+Enter is a new line; Enter sends, as chat apps do.
  await box.fill("接着把测试");
  await box.press("Shift+Enter");
  await expect(box).toHaveValue("接着把测试\n");
  await box.fill("接着把测试补上");
  await box.press("Enter");
  await expect(page.getByRole("status").filter({ hasText: "已发送。" })).toBeVisible({ timeout: 30_000 });
  const actions = fs.readFileSync(fixture.dshLog! + ".actions", "utf8");
  expect(actions).toContain('"reasoningEffort":"high"');
  expect(actions).toContain('"/permission auto"');
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

  // Never installed (no rendezvous): one press hands it to DSH (工单 #59), which installs it in a task of its own
  // and reports with `aided dsh-bridge report`; the page says how it went, DSH's reason as DSH wrote it.
  fs.rmSync(path.join(fixture.dshHome!, "aide-bridge", "bridge.json"));
  await expect(page.getByRole("status").filter({ hasText: "DSH Desktop 里还没有 Aide 插件，发不了消息。" })).toBeVisible({ timeout: 30_000 });
  const install = page.getByRole("region", { name: "安装 Aide 插件" });
  await install.getByRole("button", { name: "让 DSH 装好它" }).click();
  await expect(install.getByRole("button", { name: "看安装任务" })).toBeVisible({ timeout: 30_000 });
  const record = JSON.parse(fs.readFileSync(path.join(fixture.dshHome!, "aide-bridge", "install.json"), "utf8"));
  expect(record.package.endsWith(".tgz")).toBe(true);
  expect(fs.statSync(record.package).size).toBeGreaterThan(0);
  // DSH's report, as `aided dsh-bridge report --failed --reason …` writes it.
  fs.writeFileSync(
    path.join(fixture.dshHome!, "aide-bridge", "install-report.json"),
    JSON.stringify({ ok: false, reason: "另一个插件的常驻连接器正在运行 profile 里的程序，替换不了它的目录", at: new Date().toISOString() }),
  );
  await expect(install.getByRole("status")).toContainText("DSH 没装成功：另一个插件的常驻连接器正在运行 profile 里的程序", { timeout: 30_000 });
  await expect(install.getByRole("button", { name: "再让 DSH 试一次" })).toBeVisible();
  // The way by hand stays, folded: preparing backs up DSH's configuration and shows the package.
  await install.getByText("DSH 装不了时：手动安装").click();
  await expect(install).toContainText("在 DSH Desktop 的「插件」页，从文件或路径安装：");
  const pkg = (await install.locator(".dsh-path .mono").textContent()) ?? "";
  expect(pkg).toBe(record.package);
  await snap(page, "dsh-takeover-install");

  // Off again: read only, the box still there.
  await takeover.click();
  await expect(takeover).toHaveAttribute("aria-checked", "false");
  await expect(box).toBeDisabled();

  // No session of DSH's found: the page says which DSH home it read, and how to point Aide at another.
  fs.rmSync(path.join(fixture.dshHome!, "sessions"), { recursive: true });
  await expect(list).toContainText("你的机器上没有 DSH 自己的会话。", { timeout: 30_000 });
  await expect(list).toContainText("上读取的目录：");
  await expect(list.locator(".dsh-home .mono")).toHaveText(fixture.dshHome!);
  await expect(list).toContainText("DSH_HOME");
  await snap(page, "dsh-none-home");
  await app.close();
});
