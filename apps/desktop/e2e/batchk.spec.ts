import { expect, test, type Locator } from "@playwright/test";
import { launch, navTo, snap } from "./app";

// 批 K（工单 #109–#119）的桌面端用例。世界里有第二台机器 work-mac 和它的 billing 项目（desktop_test.go 的 batchk 分支），
// 所以链路图右侧的项目节点带「类型 · 机器名」副标题。

/** Each text line of a node: on one line (its unscaled height under two lines'), and whether it shows whole. */
async function lines(node: Locator) {
  return node.evaluate((el) =>
    [...el.querySelectorAll<HTMLElement>(".lm-t b, .lm-t small")].map((t) => ({
      text: t.textContent ?? "",
      oneLine: t.offsetHeight < parseFloat(getComputedStyle(t).fontSize) * 1.8,
      whole: t.scrollWidth <= t.clientWidth,
    })),
  );
}

// 工单 #119: the subtitle "类型 · 机器" of a node on the map's right never breaks in the middle; it shows whole at the
// usual width, cut with an ellipsis only when there is no room; the right column's nodes are as tall as the left's.
test("the map's project nodes keep their subtitle on one line, whole at the usual width, cut not broken when narrow", async () => {
  const { app, page } = await launch();
  try {
    await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(1280, 860));
    await navTo(page, /^概览/);
    const map = page.getByRole("region", { name: "链路" });
    const billing = map.locator(".lm-node.p", { hasText: "billing" });
    await expect(billing).toBeVisible();
    const sub = billing.locator(".lm-t small");
    await expect(sub).toContainText("work-mac");
    expect(await sub.evaluate((el) => getComputedStyle(el).whiteSpace)).toBe("nowrap");
    for (const l of await lines(billing)) {
      expect(l.oneLine, `${l.text} on one line`).toBe(true);
      expect(l.whole, `${l.text} shown whole`).toBe(true);
    }
    // Every node on the right as tall as the others there and as the grants on the left (before scaling).
    const heights = await map.locator(".lm-node.p, .lm-node.g").evaluateAll((els) => els.map((e) => (e as HTMLElement).offsetHeight));
    expect(new Set(heights).size).toBe(1);
    await expect(sub).toHaveAttribute("title", /work-mac$/);
    await snap(page, "batchk-119-map-wide");

    // Narrow: the stage scales down, the text never breaks (an ellipsis is fine).
    await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(900, 860));
    await expect.poll(() => map.locator(".lmap-stage").evaluate((el) => el.style.transform)).not.toBe("scale(1)");
    for (const l of await lines(billing)) expect(l.oneLine, `${l.text} on one line when narrow`).toBe(true);
    // The legend and the headline count keep each of their items on one line too.
    for (const el of await map.locator(".lmap-foot span, .lmap-pill").all())
      expect(await el.evaluate((e) => getComputedStyle(e).whiteSpace), "legend items do not wrap").toBe("nowrap");
    await snap(page, "batchk-119-map-narrow");
  } finally {
    await app.close();
  }
});
