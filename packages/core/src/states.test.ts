import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { GetStatusResponseSchema, MachineSchema, StateLimitsSchema } from "@aide/proto/local";
import { FailureKind, FailureSchema, SessionLink, SessionProgressSchema, SessionSchema, SessionState, SessionStatus } from "@aide/proto/session";
import cases from "../../../internal/sessionstate/testdata/cases.json" with { type: "json" };
import { describe, expect, test } from "vitest";
import { i18n } from "./i18n";
import { defaultStateLimits, livenessOf, lostDuring, lostLine, sessionStateOf, stateLimitsOf, stateLine } from "./states";

// The cases internal/sessionstate (Go) and the Android app are checked against too.
type Case = {
  name: string;
  session: { status: string; failure: string; last_event_ago: number | null; updated_ago: number | null };
  machine: { known: boolean; local: boolean; unreachable: string; syncing: boolean; heard_ago: number | null; unreachable_since_ago: number | null };
  want: { state: string; link: string; quiet_seconds: number; silent_seconds: number };
};
const table = cases as {
  limits: { lost_after_seconds: number; stall_after_seconds: number };
  cases: Case[];
};

const now = new Date("2026-11-19T12:00:00Z");
const ago = (s: number | null) => (s === null ? undefined : new Date(now.getTime() - s * 1000));
const ts = (s: number | null) => (s === null ? undefined : timestampFromDate(ago(s)!));
const name = (e: Record<string, string | number>, v: number) => {
  const k = Object.keys(e).find((x) => e[x] === v);
  return k === undefined || k === "UNSPECIFIED" ? "unknown" : k.toLowerCase();
};

describe("the shared cases", () => {
  const limits = { lostAfterMs: table.limits.lost_after_seconds * 1000, stallAfterMs: table.limits.stall_after_seconds * 1000 };
  for (const c of table.cases) {
    test(c.name, () => {
      const s = create(SessionSchema, {
        status: SessionStatus[c.session.status as keyof typeof SessionStatus],
        updatedAt: ts(c.session.updated_ago),
        progress: create(SessionProgressSchema, {
          lastEventAt: ts(c.session.last_event_ago),
          lastFailure: c.session.failure ? create(FailureSchema, { kind: FailureKind[c.session.failure as keyof typeof FailureKind] }) : undefined,
        }),
      });
      const m = c.machine;
      const v = sessionStateOf(s, { known: m.known, local: m.local, unreachable: m.unreachable, syncing: m.syncing, heardAt: ago(m.heard_ago), unreachableSince: ago(m.unreachable_since_ago) }, limits, now);
      expect(name(SessionState as never, v.state)).toBe(c.want.state);
      expect(name(SessionLink as never, v.link)).toBe(c.want.link);
      expect(Math.floor(v.quietMs / 1000)).toBe(c.want.quiet_seconds);
      expect(Math.floor(v.silentMs / 1000)).toBe(c.want.silent_seconds);
    });
  }
});

describe("what screens show", () => {
  test("the line, in both languages", () => {
    const run = create(SessionSchema, { status: SessionStatus.RUNNING, progress: create(SessionProgressSchema, { lastEventAt: timestampFromDate(new Date(now.getTime() - 12 * 60_000)) }) });
    const local = { known: true, local: true, unreachable: "", syncing: false };
    const lost = { known: true, local: false, unreachable: "offline", syncing: false, heardAt: new Date(now.getTime() - 7 * 60_000) };
    void i18n.changeLanguage("en");
    expect(stateLine(sessionStateOf(run, local, defaultStateLimits, now))).toBe("Running · quiet 12 min");
    expect(stateLine(sessionStateOf(run, lost, defaultStateLimits, now))).toBe("Lost · last known Running · 7 min ago");
    void i18n.changeLanguage("zh-CN");
    expect(stateLine(sessionStateOf(run, local, defaultStateLimits, now))).toBe("运行中 · 12 分钟无新事件");
    expect(stateLine(sessionStateOf(run, lost, defaultStateLimits, now))).toBe("失联 · 最后已知 运行中 · 7 分钟前");
    // A DSH session of a machine gone, in DSH's own word for its state.
    expect(lostLine("在 DSH 中打开", 3 * 60_000)).toBe("失联 · 最后已知 在 DSH 中打开 · 3 分钟前");
    expect(lostLine("运行中", 0)).toBe("失联 · 最后已知 运行中");
    void i18n.changeLanguage("en");
  });

  test("a machine as the core sends it", () => {
    const heard = new Date(now.getTime() - 40_000);
    const m = create(MachineSchema, { machineId: "m", unreachable: "offline", heardAt: timestampFromDate(heard) });
    expect(livenessOf(m)).toEqual({ known: true, local: false, unreachable: "offline", syncing: false, heardAt: heard, unreachableSince: undefined });
    expect(livenessOf(undefined).known).toBe(false);
  });

  test("the core's limits, else the defaults", () => {
    expect(stateLimitsOf(null)).toEqual(defaultStateLimits);
    expect(stateLimitsOf(create(GetStatusResponseSchema, { stateLimits: create(StateLimitsSchema, { lostAfterSeconds: 90, stallAfterSeconds: 600 }) }))).toEqual({ lostAfterMs: 90_000, stallAfterMs: 600_000 });
  });

  test("the lost spans during a session", () => {
    const at = (min: number) => timestampFromDate(new Date(now.getTime() - min * 60_000));
    const m = create(MachineSchema, { lostSpans: [{ from: at(300), to: at(290) }, { from: at(30), to: at(20) }, { from: at(5) }] });
    const s = create(SessionSchema, { createdAt: at(60) });
    expect(lostDuring(m, s).map((x) => [x.from.getTime(), x.to?.getTime()])).toEqual([
      [now.getTime() - 30 * 60_000, now.getTime() - 20 * 60_000],
      [now.getTime() - 5 * 60_000, undefined],
    ]);
  });
});
