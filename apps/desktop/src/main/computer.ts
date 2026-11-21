import { desktopCapturer, shell, systemPreferences } from "electron";
import type { ComputerPermission } from "../shared/bridge";

// Her's computer use on this Mac (docs/her-server.md §4.4). macOS holds
// aided responsible for Her's desktop tools, so whether they have their
// permissions only her can tell (desktop/status, asked by the page); the app
// opens the panes where the owner allows them.

const panes: Record<ComputerPermission, string> = {
  accessibility: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
  screen: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
};

/**
 * Opens a permission's pane. her's own check has listed aided under
 * Accessibility already; macOS lists Aide under Screen Recording only once
 * the app tried to capture, and Aide's switch there is aided's too: one
 * tiny capture does it.
 */
export async function openComputerPermission(kind: unknown) {
  if (process.platform !== "darwin" || (kind !== "accessibility" && kind !== "screen")) return;
  if (kind === "screen" && systemPreferences.getMediaAccessStatus("screen") !== "granted") {
    await desktopCapturer.getSources({ types: ["screen"], thumbnailSize: { width: 1, height: 1 } }).catch(() => []);
  }
  await shell.openExternal(panes[kind]);
}

// CI sample for work order #33, not to be merged.
