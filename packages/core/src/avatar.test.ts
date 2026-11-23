import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { ClientSchema, MachineSchema, type Machine } from "@aide/proto/local";
import { SessionSchema, SessionStatus, FailureKind, type Session } from "@aide/proto/session";
import { BOT_COLORS, BOT_SHAPES, GROK_STATES } from "@aide/ui/grok";
import { describe, expect, test } from "vitest";
import { botLook, botMood, botSeed, colorHex, colorOfHex, grantIcon, grantLook, personaLook, reactMs } from "./avatar";
import { allBots, roster, type Bot } from "./roster";

const now = new Date("2026-11-23T10:00:00Z");
const at = (ms: number) => timestampFromDate(new Date(now.getTime() - ms));

function machine(over: Partial<Machine> = {}): Machine {
  return create(MachineSchema, { machineId: "m1", machineName: "work-pc", online: true, status: { agents: [{ name: "claude", available: true, loggedIn: true, checkedAt: at(60_000) }] }, ...over } as never);
}

function session(over: object): Session {
  return create(SessionSchema, { sessionId: "s1", machineId: "m1", agent: "claude", createdBy: "owner", status: SessionStatus.IDLE, progress: { lastEventAt: at(3_600_000) }, ...over } as never);
}

function claude(m = machine(), sessions: Session[] = []): Bot {
  return allBots(roster([m], [], sessions, now))[0];
}

const mood = (b: Bot, sessions: Session[]) => botMood(b, sessions, now.getTime());

describe("the face's state from the real state (工单 #83 §2)", () => {
  test("idle, offline (rests, faded), paused (sleeping), signed out (blocked)", () => {
    expect(mood(claude(), [])).toEqual({ state: "idle", status: "online" });
    expect(mood(claude(machine({ online: false })), [])).toEqual({ state: "idle", status: "offline" });
    const paused = machine({ status: { agents: [{ name: "claude", available: true, loggedIn: true, checkedAt: at(60_000), pause: {} }] } } as never);
    expect(mood(claude(paused), [])).toEqual({ state: "sleeping", status: "online" });
    const out = machine({ status: { agents: [{ name: "claude", available: true, loggedIn: false, checkedAt: at(60_000) }] } } as never);
    expect(mood(claude(out), []).state).toBe("blocked");
  });

  test("at work: what it does now picks the state", () => {
    const run = (progress: object) => mood(claude(), [session({ status: SessionStatus.RUNNING, progress: { lastEventAt: at(1000), ...progress } })]).state;
    expect(run({})).toBe("thinking");
    expect(run({ lastMessageExcerpt: "Here is the fix" })).toBe("streaming");
    expect(run({ phase: "editing", currentTool: "Edit: src/a.ts" })).toBe("coding");
    expect(run({ phase: "running_shell", currentTool: "Bash: go test ./..." })).toBe("running");
    expect(run({ phase: "running", currentTool: "Read: src/a.ts" })).toBe("reading");
    expect(run({ phase: "running", currentTool: "Grep: TODO" })).toBe("searching");
    expect(run({ phase: "running", currentTool: "WebFetch: docs" })).toBe("searching");
    expect(run({ phase: "running", currentTool: "Task: review" })).toBe("working");
    expect(mood(claude(), [session({ status: SessionStatus.QUEUED })]).state).toBe("waiting");
  });

  test("waiting on the owner: blocked, before any running one", () => {
    const s = [session({ status: SessionStatus.RUNNING }), session({ sessionId: "s2", status: SessionStatus.WAITING_INPUT })];
    expect(mood(claude(), s).state).toBe("blocked");
  });

  test("just finished: finished, then idle; just failed: failed, then idle", () => {
    expect(mood(claude(), [session({ progress: { turnCount: 1, lastEventAt: at(2000) } })]).state).toBe("finished");
    expect(mood(claude(), [session({ progress: { turnCount: 1, lastEventAt: at(reactMs + 1000) } })]).state).toBe("idle");
    expect(mood(claude(), [session({ status: SessionStatus.CRASHED, progress: { lastEventAt: at(2000) } })]).state).toBe("failed");
    expect(mood(claude(), [session({ progress: { lastEventAt: at(2000), lastFailure: { kind: FailureKind.AUTH } } })]).state).toBe("failed");
    expect(mood(claude(), [session({ progress: { lastEventAt: at(2000), lastFailure: { kind: FailureKind.INTERRUPTED } } })]).state).toBe("idle");
  });

  test("only the bot's own sessions move it", () => {
    expect(mood(claude(), [session({ agent: "codex", status: SessionStatus.RUNNING })]).state).toBe("idle");
    expect(mood(claude(), [session({ status: SessionStatus.ARCHIVED })]).state).toBe("idle");
  });

  test("every state it picks is the engine's", () => {
    for (const s of ["idle", "blocked", "thinking", "streaming", "coding", "running", "reading", "searching", "working", "waiting", "sleeping", "finished", "failed"]) {
      expect(GROK_STATES).toContain(s);
    }
  });
});

describe("the look: seed by default, the owner's own first (工单 #83 §3)", () => {
  test("26 shapes, 11 colours", () => {
    expect(BOT_SHAPES).toHaveLength(26);
    expect(BOT_COLORS).toHaveLength(11);
  });

  test("the seed's default is stable, and the same seed as Android's (model/Avatar.kt)", () => {
    // The same cases are in the Android tests (AvatarTest.kt): both read the engine's defaultShapeForSeed / defaultColorForSeed.
    const b = claude();
    expect(botSeed(b)).toBe("m:m1/claude");
    expect(botLook(b)).toEqual({ shape: "teardrop", color: "orange" });
    expect(botLook({ key: "c:k1", client: create(ClientSchema, { clientId: "k1" }), personas: [] })).toEqual({ shape: "hex", color: "brown" });
    expect(botLook({ key: "m:m1/deepseek", personas: [] })).toEqual({ shape: "squircle", color: "orange" });
    expect(botLook({ key: "DSH · 工作电脑", personas: [] })).toEqual({ shape: "wedge", color: "blue" });
  });

  test("a contact's look wins; one from before keeps its colour (nearest) and takes the seed's shape", () => {
    const p = { icon: "heart", color: colorHex("magenta") };
    expect(personaLook(p, "x")).toEqual({ shape: "heart", color: "magenta" });
    expect(botLook({ ...claude(), personas: [p as never] })).toEqual({ shape: "heart", color: "magenta" });
    expect(personaLook({ icon: "owl", color: "#10b981" }, "m:m1/claude")).toEqual({ shape: "teardrop", color: "green" });
    expect(colorOfHex("#3b82f6")).toBe("blue");
    expect(colorOfHex("#ef4444")).toBe("red");
    expect(colorOfHex("")).toBeUndefined();
  });

  test("a grant's icon keeps a look; its app's logo or a picture does not", () => {
    expect(grantIcon({ shape: "star", color: "cyan" })).toBe("bot:star:cyan");
    expect(grantLook("bot:star:cyan", "s")).toEqual({ shape: "star", color: "cyan" });
    expect(grantLook("app:claude", "s")).toBeUndefined();
    const g = create(ClientSchema, { clientId: "k1", icon: "bot:house:red" });
    expect(botLook({ key: "c:k1", client: g, personas: [] })).toEqual({ shape: "house", color: "red" });
  });
});
