import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { ExecOutcome, ExecRecordSchema } from "@aide/proto/machine";
import { describe, expect, test } from "vitest";
import { opsMillis, opsResult } from "./ops";

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
});
