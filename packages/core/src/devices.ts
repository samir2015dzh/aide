/**
 * What a device or machine is, for its face (工单 #100): by the platform it reports, a trusted device's ("android",
 * "android/tablet", "windows", "macos", "linux", "ios") or a machine's GOOS/GOARCH ("darwin/arm64"). A Linux one is
 * drawn as a server, the machine a coding agent most often runs on; what says nothing known is a device.
 */
export type DeviceKind = "android" | "iphone" | "tablet" | "windows" | "mac" | "linux" | "device";

export function deviceKind(platform: string): DeviceKind {
  const p = platform.trim().toLowerCase();
  const os = p.split("/")[0];
  if (p.includes("tablet") || os === "ipados" || os === "ipad") return "tablet";
  if (os === "android") return "android";
  if (os === "ios" || os === "iphone") return "iphone";
  if (os === "windows" || os === "win32") return "windows";
  if (os === "darwin" || os === "macos" || os === "mac") return "mac";
  if (os === "linux" || os === "freebsd" || os === "openbsd" || os === "netbsd") return "linux";
  return "device";
}

/**
 * A machine's kind: what it reports (its status, while it answers), else what the device it is logged in with, else
 * for this computer's own machine the platform the app runs on (rootPlatform's), so an offline machine keeps its face.
 */
export function machineKind(m: { status?: { platform: string }; deviceKeyId: string; local: boolean }, devices: readonly { keyId: string; platform: string }[], here = ""): DeviceKind {
  const device = m.deviceKeyId ? devices.find((d) => d.keyId === m.deviceKeyId)?.platform : "";
  return deviceKind(m.status?.platform || device || (m.local ? here : ""));
}
