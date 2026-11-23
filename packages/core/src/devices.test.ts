import { describe, expect, test } from "vitest";
import { deviceKind, machineKind } from "./devices";

// 工单 #100: a device's or machine's face is what it reports itself to be.
describe("deviceKind", () => {
  test("trusted devices, by the platform they log in with", () => {
    expect(deviceKind("android")).toBe("android");
    expect(deviceKind("android/tablet")).toBe("tablet");
    expect(deviceKind("windows")).toBe("windows");
    expect(deviceKind("macos")).toBe("mac");
    expect(deviceKind("linux")).toBe("linux");
    expect(deviceKind("ios")).toBe("iphone");
    expect(deviceKind("ipados")).toBe("tablet");
  });

  test("machines, by GOOS/GOARCH", () => {
    expect(deviceKind("darwin/arm64")).toBe("mac");
    expect(deviceKind("windows/amd64")).toBe("windows");
    expect(deviceKind("linux/amd64")).toBe("linux");
    expect(deviceKind("freebsd/amd64")).toBe("linux");
  });

  test("what says nothing known is a device, never a letter", () => {
    expect(deviceKind("")).toBe("device");
    expect(deviceKind("plan9/386")).toBe("device");
    expect(deviceKind("  Windows ")).toBe("windows");
  });

  test("a machine keeps its face while it does not answer", () => {
    const devices = [{ keyId: "k1", platform: "windows" }];
    expect(machineKind({ status: { platform: "linux/arm64" }, deviceKeyId: "", local: false }, devices)).toBe("linux");
    expect(machineKind({ deviceKeyId: "k1", local: false }, devices)).toBe("windows");
    expect(machineKind({ deviceKeyId: "", local: true }, devices, "macos")).toBe("mac");
    expect(machineKind({ deviceKeyId: "", local: false }, devices, "macos")).toBe("device");
  });
});
