import type { Locator, Page } from "@playwright/test";

/** The sidebar, shown first when it is hidden (工单 #141: no rail; hidden is hidden, the title bar's button shows it). */
export async function showSidebar(page: Page): Promise<void> {
  const show = page.getByRole("button", { name: /^(显示边栏|Show Sidebar)$/ });
  await page.locator("aside.side, .win-titlebar").first().waitFor({ timeout: 30_000 });
  if (!(await page.locator("aside.side").count()) && (await show.count())) await show.click();
  await page.locator("aside.side").first().waitFor({ timeout: 30_000 });
}

/**
 * Opens a page: the overview and the task feed from the sidebar's top (工单 #102); the others (every session, the
 * devices and the rest) from the account menu at the sidebar's foot (工单 #63). A hidden sidebar is shown first.
 */
export async function navTo(page: Page, name: RegExp): Promise<void> {
  await showSidebar(page);
  const top = page.locator("nav.side-nav").getByRole("button", { name });
  if (await top.count()) return top.first().click();
  await page.locator(".side-foot").first().click();
  await page.getByRole("menu", { name: /^(账号与设置|Account)/ }).getByRole("menuitem", { name }).first().click();
}

/** The session list (工单 #63: the page 全部任务, opened from the account menu; its list stays beside a session opened from it). */
export async function sessionList(page: Page): Promise<Locator> {
  const list = page.getByRole("list", { name: /^(任务|Tasks)$/ });
  if (!(await list.isVisible())) await navTo(page, /^(全部任务|All Tasks)/);
  await list.waitFor();
  return list;
}
