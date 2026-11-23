import type { Persona } from "@aide/proto/identity";
import { SessionState, SessionStatus, type Session } from "@aide/proto/session";
import { BOT_COLORS, resolveBotColor, resolveBotShape, type BotColorId, type BotShape, type GrokStatus } from "@aide/ui/grok";
import type { Bot } from "./roster";
import { lastEventAt, stateOf } from "./states";

// A bot's face (工单 #83): the Grok Bot character ported from BotArk (@aide/ui/grok), in a shape and a colour, moved by
// the bot's real state. An entry of its own (@aide/core/avatar), so only windows that draw bots load the engine.
// Android's model/Avatar.kt is the same table.

export type BotLook = { shape: BotShape; color: BotColorId };

/**
 * The seed a bot's default look comes from: its grant's when it has one (the same face wherever the grant is drawn),
 * else its key. The default is the engine's own (defaultShapeForSeed, defaultColorForSeed).
 */
export function botSeed(b: Pick<Bot, "key" | "client">): string {
  return b.client ? `c:${b.client.clientId}` : b.key;
}

/** The palette colour a stored "#rrggbb" is: its own swatch, else the nearest (a contact's colour from before #83). */
export function colorOfHex(hex: string): BotColorId | undefined {
  const rgb = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16));
  if (!/^#[0-9a-f]{6}$/i.test(hex)) return undefined;
  const [r, g, b] = rgb(hex);
  let best: { id: BotColorId; d: number } | undefined;
  for (const c of BOT_COLORS) {
    const [cr, cg, cb] = rgb(c.swatch);
    const d = (r - cr) ** 2 + (g - cg) ** 2 + (b - cb) ** 2;
    if (!best || d < best.d) best = { id: c.id, d };
  }
  return best?.id;
}

/** The colour as a contact keeps it: its swatch, "#rrggbb" (the relay keeps a contact's colour so). */
export function colorHex(c: BotColorId): string {
  return BOT_COLORS.find((x) => x.id === c)!.swatch;
}

/**
 * A contact's look: its icon is its shape, its colour its swatch. One from before #83 (an emoji's name, one of six
 * colours) keeps its colour, the nearest of the palette, and takes the seed's shape.
 */
export function personaLook(p: Pick<Persona, "icon" | "color">, seed: string): BotLook {
  return { shape: resolveBotShape(seed, p.icon), color: resolveBotColor(seed, colorOfHex(p.color)) };
}

/** A grant's icon for a look (Grant client_icon "bot:<shape>:<colour>"). */
export function grantIcon(l: BotLook): string {
  return `bot:${l.shape}:${l.color}`;
}

/** The look a grant's icon picked, when it is one ("bot:<shape>:<colour>"); its app's logo or a picture is not. */
export function grantLook(icon: string | undefined, seed: string): BotLook | undefined {
  const m = /^bot:([a-z]+):([a-z]+)$/.exec(icon ?? "");
  return m ? { shape: resolveBotShape(seed, m[1]), color: resolveBotColor(seed, m[2]) } : undefined;
}

/** A bot's look: the owner's own (its first contact's, else its grant's) before the seed's (人工值优先, 工单 #52). */
export function botLook(b: Pick<Bot, "key" | "client" | "personas">): BotLook {
  const seed = botSeed(b);
  const p = b.personas[0];
  if (p) return personaLook(p, seed);
  return grantLook(b.client?.icon, seed) ?? { shape: resolveBotShape(seed), color: resolveBotColor(seed) };
}

/** A bot's own sessions, as the roster counts them: a coding agent's on its machine, a grant's those it started. */
export function botSessions(b: Pick<Bot, "key" | "machineId" | "agent" | "client">, sessions: readonly Session[]): Session[] {
  if (b.key.startsWith("m:")) return sessions.filter((s) => s.machineId === b.machineId && s.agent === b.agent);
  return b.client ? sessions.filter((s) => s.createdBy === b.client!.clientId) : [];
}

/** How long a finished or failed turn shows on the face before it rests (the engine's own clip then ends it). */
export const reactMs = 8_000;

/** What the face shows: the engine's state (GROK_STATES), and offline to rest it, faded. */
export type BotMood = { state: string; status: GrokStatus };

/**
 * The face's state from the bot's real state (工单 #83 §2, from #47/#49/#56), first that holds:
 * - away (its machine unreachable, or nothing known of it): idle, resting and faded;
 * - a session waits on the owner's answer: blocked (the bang);
 * - a session runs: by what it does now — an edit coding, a shell command running, reading, searching (a search or
 *   the web), another tool working; no tool: streaming while it writes its reply, thinking before; queued or being
 *   prepared: waiting;
 * - paused by the owner: sleeping;
 * - signed out or not installed (the owner must see to it): blocked;
 * - its last turn ended within reactMs: finished (done) or failed, which the engine plays and then rests in idle;
 * - else idle.
 */
export function botMood(b: Pick<Bot, "key" | "machineId" | "agent" | "client" | "state">, sessions: readonly Session[], now: number): BotMood {
  if (b.state.why === "offline" || b.state.state === "unknown") return { state: "idle", status: "offline" };
  const live = botSessions(b, sessions).filter((s) => s.status !== SessionStatus.ARCHIVED);
  if (live.some((s) => s.status === SessionStatus.WAITING_INPUT)) return { state: "blocked", status: "online" };
  const newest = (a: Session, c: Session) => (lastEventAt(c)?.getTime() ?? 0) - (lastEventAt(a)?.getTime() ?? 0);
  const running = live.filter((s) => s.status === SessionStatus.RUNNING).sort(newest)[0];
  if (running) return { state: runningState(running), status: "online" };
  if (live.some((s) => s.status === SessionStatus.QUEUED || s.status === SessionStatus.PREPARING)) return { state: "waiting", status: "online" };
  if (b.state.why === "paused") return { state: "sleeping", status: "online" };
  if (b.state.why === "signedOut" || b.state.why === "missing") return { state: "blocked", status: "online" };
  const last = [...live].sort(newest)[0];
  const at = last && lastEventAt(last)?.getTime();
  if (last && at && now - at < reactMs) {
    const st = stateOf(last);
    if (st === SessionState.DONE || st === SessionState.DONE_UNSEEN) return { state: "finished", status: "online" };
    if (st === SessionState.FAILED) return { state: "failed", status: "online" };
  }
  return { state: "idle", status: "online" };
}

/** A running session's state by what it does now (SessionProgress phase and current_tool "<name>: <summary>"). */
export function runningState(s: Session): string {
  const p = s.progress;
  const tool = (p?.currentTool ?? "").split(":")[0].trim().toLowerCase();
  if (!tool) return p?.lastMessageExcerpt ? "streaming" : "thinking";
  if (p?.phase === "editing") return "coding";
  if (p?.phase === "running_shell") return "running";
  if (/search|grep|glob|^ls$|find|fetch|web|browse/.test(tool)) return "searching";
  if (/read|view|open|cat/.test(tool)) return "reading";
  return "working";
}
