import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { ExecOutcome, ExecRecordSchema } from "@aide/proto/machine";
import { describe, expect, test } from "vitest";
import { opsCommandSummary, opsMillis, opsResult } from "./ops";

describe("ops log", () => {
  test("how each call ended", () => {
    const r = (outcome: ExecOutcome, exitCode = 0) => opsResult(create(ExecRecordSchema, { outcome, exitCode }));
    expect(r(ExecOutcome.EXITED)).toEqual({ kind: "ok" });
    expect(r(ExecOutcome.EXITED, 7)).toEqual({ kind: "exit", code: 7 });
    expect(r(ExecOutcome.TIMED_OUT, -1)).toEqual({ kind: "timeout" });
    expect(r(ExecOutcome.REFUSED)).toEqual({ kind: "refused" });
    expect(r(ExecOutcome.FAILED)).toEqual({ kind: "failed" });
  });

  test("how long it ran", () => {
    expect(opsMillis(create(ExecRecordSchema, { startedAt: timestampFromMs(1_000), endedAt: timestampFromMs(3_500) }))).toBe(2_500);
    expect(opsMillis(create(ExecRecordSchema, {}))).toBe(0);
  });

  test("summarizes multi-line scripts and keeps short commands inline (#138 P14)", () => {
    expect(opsCommandSummary("git status -sb")).toEqual({
      shell: "plain",
      preview: "git status -sb",
      collapsible: false,
      lineCount: 1,
    });

    const ps = [
      "powershell -NoProfile -Command \"",
      "$ErrorActionPreference = 'Stop'",
      "# inspect running services",
      "Get-Service | Where-Object { $_.Status -eq 'Running' } | Select-Object -First 5",
      "Write-Host 'done'",
      "\"",
    ].join("\n");
    const psSummary = opsCommandSummary(ps);
    expect(psSummary.shell).toBe("powershell");
    expect(psSummary.collapsible).toBe(true);
    expect(psSummary.lineCount).toBe(6);
    expect(psSummary.preview.startsWith("Get-Service | Where-Object")).toBe(true);
    expect(psSummary.preview.length).toBeLessThanOrEqual(72);

    const sh = "bash -lc 'set -euo pipefail\npnpm test\n'";
    const shSummary = opsCommandSummary(sh);
    expect(shSummary.shell).toBe("shell");
    expect(shSummary.collapsible).toBe(true);
    expect(shSummary.preview).toBe("pnpm test");
  });
});
