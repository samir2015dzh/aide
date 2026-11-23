import type { Locator, Page } from "@playwright/test";

/**
 * Opens a page: the overview and the task feed from the sidebar's top (工单 #102), or the rail's; the others (every
 * session, the devices and the rest) from the account menu at the sidebar's foot (工单 #63), from the rail's when only it shows.
 */
export async function navTo(page: Page, name: RegExp): Promise<void> {
  await page.locator("aside.side, aside.rail").first().waitFor({ timeout: 30_000 });
  const top = page.locator("nav.side-nav, nav.rail-nav").getByRole("button", { name });
  if (await top.count()) return top.first().click();
  await page.locator(".side-foot").first().click();
  await page.getByRole("menu", { name: /^(账号与设置|Account)/ }).getByRole("menuitem", { name }).first().click();
}

/** The session list (工单 #63: the page 全部任务, opened from the account menu; its list stays beside a session opened from it). */
export async function sessionList(page: Page): Promise<Locator> {
  const list = page.getByRole("list", { name: /^(任务|Tasks)$/ });
  await page.locator("aside.side, aside.rail").first().waitFor({ timeout: 30_000 });
  if (!(await list.isVisible())) await navTo(page, /^(全部任务|All Tasks)/);
  await list.waitFor();
  return list;
}
