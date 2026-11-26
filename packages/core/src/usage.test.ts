import { create } from "@bufbuild/protobuf";
import { QueryWorkResponseSchema, WorkBucket, WorkDimension, type QueryWorkResponse } from "@aide/proto/local";
import { describe, expect, test } from "vitest";
import { usageParts, usageRequest, usageSince, usageYear } from "./usage";

type Day = { date: string; agent: string; tokens: number; sessions: number; machineId: string };
const day = (date: string, agent: string, tokens: number, sessions = 1, machineId = "mac"): Day => ({ date, agent, tokens, sessions, machineId });

describe("usage", () => {
  // Tuesday 20 October 2026.
  const today = new Date(2026, 9, 20, 15, 0);
  // QueryWork's answer, by day (from usageSince) and agent.
  const answer = (days: Day[]): QueryWorkResponse => {
    const [y, m, d] = usageSince(today).split("-").map(Number);
    const bucket = (date: string) => {
      const [a, b, c] = date.split("-").map(Number);
      return Math.round((new Date(a, b - 1, c).getTime() - new Date(y, m - 1, d).getTime()) / 86_400_000);
    };
    return create(QueryWorkResponseSchema, {
      turns: days.map((x) => ({ bucket: bucket(x.date), group: { agent: x.agent, machineId: x.machineId }, turns: x.sessions, inputTokens: BigInt(x.tokens) })),
      sessions: days.map((x) => ({ bucket: bucket(x.date), group: { agent: x.agent, machineId: x.machineId }, sessions: x.sessions })),
    });
  };
  const year = (days: Day[]) => usageYear(answer(days), today);

  test("a year of weeks ends in today's week and starts on a Sunday", () => {
    expect(usageSince(today)).toBe("2025-10-19");
    const y = usageYear(null, today);
    const q = usageRequest(today, 53, "Asia/Shanghai");
    expect(q).toMatchObject({ firstDay: "2025-10-19", days: 367, bucket: WorkBucket.DAY, groupBy: [WorkDimension.AGENT, WorkDimension.MACHINE], timeZone: "Asia/Shanghai" });
    expect(y.cells.length).toBe(53 * 7);
    expect(y.cells[0]?.key).toBe("2025-10-19");
    // Today is the third cell of the last column; the rest of the week is not shown.
    expect(y.cells[52 * 7 + 2]?.key).toBe("2026-10-20");
    expect(y.cells.slice(52 * 7 + 3)).toEqual([null, null, null, null]);
    expect(y.total).toBe(0);
    expect(y.delta).toBeNull();
  });

  const days6 = [
    day("2026-10-20", "claude", 400),
    day("2026-10-20", "codex", 100, 2),
    day("2026-10-19", "claude", 200),
    day("2026-10-18", "claude", 100),
    day("2026-10-10", "codex", 50),
    day("2026-09-10", "claude", 150),
  ];

  test("tokens add up by day and agent, with levels, streak and the change over 30 days", () => {
    const y = year(days6);
    expect(y.today).toBe(500);
    expect(y.total).toBe(1000);
    expect(y.active).toBe(5);
    expect(y.streak).toBe(3);
    expect(y.delta).toBe(467); // 850 in the last 30 days against 150 before
    expect(usageParts(answer(days6), today, "agent").map((p) => [p.id, p.tokens, p.share])).toEqual([
      ["claude", 850, 0.85],
      ["codex", 150, 0.15],
    ]);
    const cell = y.cells.find((c) => c?.key === "2026-10-20");
    expect(cell?.sessions).toBe(3);
    expect(cell?.level).toBe(4);
    expect(y.cells.find((c) => c?.key === "2026-10-10")?.level).toBe(1);
    expect(y.cells.find((c) => c?.key === "2026-10-11")?.level).toBe(0);
  });

  test("a lone day is drawn darkest", () => {
    const y = year([day("2026-10-20", "gemini", 375)]);
    expect(y.cells.find((c) => c?.key === "2026-10-20")?.level).toBe(4);
  });

  test("before today's first turn the streak counts up to yesterday", () => {
    const y = year([day("2026-10-19", "claude", 10), day("2026-10-18", "claude", 10)]);
    expect(y.streak).toBe(2);
  });

  // 分组 (工单 #142): the year's tokens by coding agent or by machine, from the one answer.
  test("by agent and by machine the parts add up to the year's total, and a day's to the day's", () => {
    const days = [
      day("2026-10-20", "claude", 400, 1, "mac"),
      day("2026-10-20", "claude", 300, 1, "vps"),
      day("2026-10-20", "codex", 100, 2, "vps"),
      day("2026-10-19", "gemini", 200, 1, "mac"),
      day("2026-09-10", "codex", 150, 1, "mini"),
    ];
    const y = usageYear(answer(days), today);
    expect(y.total).toBe(1150);
    const agents = usageParts(answer(days), today, "agent");
    expect(agents.map((p) => [p.id, p.tokens])).toEqual([["claude", 700], ["codex", 250], ["gemini", 200]]);
    const machines = usageParts(answer(days), today, "machine");
    expect(machines.map((p) => [p.id, p.tokens])).toEqual([["mac", 600], ["vps", 400], ["mini", 150]]);
    for (const parts of [agents, machines]) {
      expect(parts.reduce((n, p) => n + p.tokens, 0)).toBe(y.total);
      expect(parts.reduce((n, p) => n + p.share, 0)).toBeCloseTo(1);
      for (const c of y.cells) if (c) expect(parts.reduce((n, p) => n + (p.days.get(c.key) ?? 0), 0)).toBe(c.tokens);
    }
    expect(machines.find((p) => p.id === "vps")!.days.get("2026-10-20")).toBe(400);
    // Nothing asked yet, or nothing done: no parts.
    expect(usageParts(null, today, "machine")).toEqual([]);
    expect(usageParts(answer([day("2026-10-20", "claude", 0)]), today, "agent")).toEqual([]);
  });

  test("past the parts named, the least busy are one, Other, and still add up", () => {
    const agents = ["claude", "codex", "gemini", "cursor", "qwen", "opencode", "copilot"];
    const days = agents.map((a, i) => day("2026-10-20", a, 70 - i * 10));
    const parts = usageParts(answer(days), today, "agent");
    expect(parts.map((p) => p.id)).toEqual(["claude", "codex", "gemini", "cursor", ""]);
    expect(parts.at(-1)).toMatchObject({ other: true, tokens: 30 + 20 + 10 });
    expect(parts.at(-1)!.days.get("2026-10-20")).toBe(60);
    expect(parts.reduce((n, p) => n + p.tokens, 0)).toBe(year(days).total);
    // Five or fewer are all named.
    expect(usageParts(answer(days.slice(0, 5)), today, "agent").some((p) => p.other)).toBe(false);
  });
});
