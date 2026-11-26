import type { SessionEvent } from "@aide/proto/session";
import { userWords } from "./transcript";

/**
 * Weak-network outbound queue & inbound stream catch-up primitives (工单 #115).
 * Shared pure state machine used by Desktop (and mirrored in Android Kotlin):
 * - Every outgoing message lands locally first with a per-session monotonic clientSeq
 *   and stable clientMessageId (used by aided for persistent resend deduplication).
 * - Offline or transient failures stay queued and flush strictly in clientSeq order
 *   once connectivity returns; killing and reopening the app preserves all queued items
 *   and recovers in-flight "sending" items back to "pending" without changing their id.
 * - Inbound session events are strictly deduplicated and ordered by server seq.
 */

export type OutboxStatus = "pending" | "sending" | "sent" | "failed";

export interface OutboxAttachment {
  name: string;
  mimeType: string;
  /** Base64-encoded bytes so the queue survives JSON persistence across app restarts. */
  dataBase64: string;
}

export interface OutboxMessage {
  /** Stable idempotency key sent as SendMessageRequest.client_message_id. */
  id: string;
  sessionId: string;
  /** Monotonic per-session local sequence number sent as SendMessageRequest.client_seq. */
  clientSeq: number;
  text: string;
  attachments: OutboxAttachment[];
  status: OutboxStatus;
  /** Last delivery error shown inline when pending (waiting for network) or failed. */
  error?: string;
  /** ISO UTC timestamps so persistence does not depend on host timezone. */
  createdAt: string;
  updatedAt: string;
}

export interface OutboxState {
  version: 1;
  /** Next clientSeq to allocate per sessionId (never resets while session exists). */
  nextSeq: Record<string, number>;
  items: OutboxMessage[];
}

export function emptyOutboxState(): OutboxState {
  return { version: 1, nextSeq: {}, items: [] };
}

export function parseOutboxState(raw: string | null | undefined): OutboxState {
  if (!raw) return emptyOutboxState();
  try {
    const parsed = JSON.parse(raw) as Partial<OutboxState>;
    if (!parsed || typeof parsed !== "object") return emptyOutboxState();
    const nextSeq: Record<string, number> = {};
    if (parsed.nextSeq && typeof parsed.nextSeq === "object") {
      for (const [k, v] of Object.entries(parsed.nextSeq)) {
        if (typeof v === "number" && Number.isFinite(v) && v >= 1) nextSeq[k] = Math.floor(v);
      }
    }
    const items: OutboxMessage[] = Array.isArray(parsed.items)
      ? parsed.items
          .filter((it): it is OutboxMessage =>
            Boolean(it && typeof it.id === "string" && typeof it.sessionId === "string" && typeof it.clientSeq === "number"),
          )
          .map((it) => ({
            id: it.id,
            sessionId: it.sessionId,
            clientSeq: Math.floor(it.clientSeq),
            text: typeof it.text === "string" ? it.text : "",
            attachments: Array.isArray(it.attachments) ? it.attachments : [],
            status: it.status === "sending" || it.status === "sent" || it.status === "failed" ? it.status : "pending",
            error: typeof it.error === "string" && it.error !== "" ? it.error : undefined,
            createdAt: typeof it.createdAt === "string" ? it.createdAt : new Date(0).toISOString(),
            updatedAt: typeof it.updatedAt === "string" ? it.updatedAt : new Date(0).toISOString(),
          }))
      : [];
    // Ensure nextSeq is always strictly above every existing item's clientSeq.
    for (const it of items) {
      const want = it.clientSeq + 1;
      if ((nextSeq[it.sessionId] ?? 1) < want) nextSeq[it.sessionId] = want;
    }
    return { version: 1, nextSeq, items };
  } catch {
    return emptyOutboxState();
  }
}

export function serializeOutboxState(state: OutboxState): string {
  return JSON.stringify(state);
}

/**
 * Called once on app startup: any item that was mid-flight ("sending") when the
 * process exited is reset to "pending" keeping the exact same `id` so the server
 * deduplicates if the previous request actually succeeded before the crash/kill.
 */
export function recoverOutboxOnBoot(state: OutboxState, now = new Date().toISOString()): OutboxState {
  let changed = false;
  const items = state.items.map((it) => {
    if (it.status !== "sending") return it;
    changed = true;
    return { ...it, status: "pending" as const, updatedAt: now };
  });
  return changed ? { ...state, items } : state;
}

export function enqueueOutbox(
  state: OutboxState,
  input: {
    id: string;
    sessionId: string;
    text: string;
    attachments?: OutboxAttachment[];
    now?: string;
  },
): { state: OutboxState; item: OutboxMessage } {
  const now = input.now ?? new Date().toISOString();
  const seq = state.nextSeq[input.sessionId] ?? 1;
  const item: OutboxMessage = {
    id: input.id,
    sessionId: input.sessionId,
    clientSeq: seq,
    text: input.text,
    attachments: input.attachments ?? [],
    status: "pending",
    createdAt: now,
    updatedAt: now,
  };
  return {
    item,
    state: {
      version: 1,
      nextSeq: { ...state.nextSeq, [input.sessionId]: seq + 1 },
      items: [...state.items, item],
    },
  };
}

export function markOutboxSending(state: OutboxState, id: string, now = new Date().toISOString()): OutboxState {
  return {
    ...state,
    items: state.items.map((it) => (it.id === id ? { ...it, status: "sending", error: undefined, updatedAt: now } : it)),
  };
}

export function markOutboxSent(state: OutboxState, id: string, now = new Date().toISOString()): OutboxState {
  return {
    ...state,
    items: state.items.map((it) => (it.id === id ? { ...it, status: "sent", error: undefined, updatedAt: now } : it)),
  };
}

/**
 * Records a delivery failure. Transient network/offline errors keep status as "pending"
 * with `error` recorded so the item automatically flushes on reconnect in order;
 * permanent/precondition failures transition to "failed" (manual retry or cancel).
 */
export function markOutboxError(
  state: OutboxState,
  id: string,
  error: string,
  transient: boolean,
  now = new Date().toISOString(),
): OutboxState {
  const nextStatus: OutboxStatus = transient ? "pending" : "failed";
  return {
    ...state,
    items: state.items.map((it) => (it.id === id ? { ...it, status: nextStatus, error, updatedAt: now } : it)),
  };
}

export function retryOutbox(state: OutboxState, id: string, now = new Date().toISOString()): OutboxState {
  return {
    ...state,
    items: state.items.map((it) => (it.id === id && (it.status === "failed" || it.status === "pending") ? { ...it, status: "pending", error: undefined, updatedAt: now } : it)),
  };
}

export function removeOutbox(state: OutboxState, id: string): OutboxState {
  return {
    ...state,
    items: state.items.filter((it) => it.id !== id),
  };
}

/**
 * Returns the next eligible message to send for each session in strict clientSeq order.
 * If a session has an earlier item currently "sending" or "failed", later "pending" items
 * for that same session wait behind it so messages never overtake each other.
 */
export function nextOutboxMessages(state: OutboxState, sessionId?: string): OutboxMessage[] {
  const bySession = new Map<string, OutboxMessage[]>();
  for (const it of state.items) {
    if (sessionId && it.sessionId !== sessionId) continue;
    if (it.status === "sent") continue;
    const list = bySession.get(it.sessionId) ?? [];
    list.push(it);
    bySession.set(it.sessionId, list);
  }
  const ready: OutboxMessage[] = [];
  for (const list of bySession.values()) {
    list.sort((a, b) => a.clientSeq - b.clientSeq);
    const head = list[0];
    if (head && head.status === "pending") ready.push(head);
  }
  return ready;
}

export function outboxForSession(state: OutboxState, sessionId: string): OutboxMessage[] {
  return state.items.filter((it) => it.sessionId === sessionId).sort((a, b) => a.clientSeq - b.clientSeq);
}

/**
 * Removes "sent" outbox entries once the server's confirmed UserMessage event
 * has arrived in the session stream (matching by trimmed text in order),
 * preventing duplicate bubbles while keeping unconfirmed "sent" markers visible
 * if the stream hasn't caught up yet.
 */
export function reconcileOutboxWithEvents(state: OutboxState, sessionId: string, events: SessionEvent[]): OutboxState {
  const sentForSession = state.items.filter((it) => it.sessionId === sessionId && it.status === "sent");
  if (sentForSession.length === 0) return state;

  const userTexts = events
    .filter((e) => e.kind.case === "userMessage")
    // The words as written: a message with files has the machine's list of them after its words (工单 #143).
    .map((e) => (e.kind.case === "userMessage" ? userWords(e.kind.value.text, e.kind.value.attachments.length).trim() : ""));

  const toDrop = new Set<string>();
  let searchFrom = 0;
  for (const item of sentForSession.sort((a, b) => a.clientSeq - b.clientSeq)) {
    const needle = item.text.trim();
    if (needle === "") {
      // Attachment-only message: drop once any userMessage exists after enqueue or if stream has events.
      if (userTexts.length > 0) toDrop.add(item.id);
      continue;
    }
    const idx = userTexts.indexOf(needle, searchFrom);
    if (idx >= 0) {
      toDrop.add(item.id);
      searchFrom = idx + 1;
    }
  }
  if (toDrop.size === 0) return state;
  return {
    ...state,
    items: state.items.filter((it) => !toDrop.has(it.id)),
  };
}

/**
 * Merges incoming session events into the existing ordered list, strictly
 * deduplicating by non-zero server sequence number (`seq`) and maintaining
 * ascending `seq` order. Used on reconnect catch-up so overlapping chunks
 * never produce duplicate transcript rows.
 */
export function mergeSessionEvents(
  existing: SessionEvent[],
  incoming: SessionEvent[],
): { events: SessionEvent[]; lastSeq: bigint; added: number } {
  if (incoming.length === 0) {
    const last = existing.reduce((m, e) => (e.seq > m ? e.seq : m), 0n);
    return { events: existing, lastSeq: last, added: 0 };
  }
  const seen = new Set<bigint>();
  let lastSeq = 0n;
  for (const e of existing) {
    if (e.seq > 0n) {
      seen.add(e.seq);
      if (e.seq > lastSeq) lastSeq = e.seq;
    }
  }
  const fresh: SessionEvent[] = [];
  let needsSort = false;
  for (const e of incoming) {
    if (e.seq > 0n) {
      if (seen.has(e.seq)) continue;
      seen.add(e.seq);
      if (e.seq < lastSeq) needsSort = true;
      else lastSeq = e.seq;
    }
    fresh.push(e);
  }
  if (fresh.length === 0) {
    return { events: existing, lastSeq, added: 0 };
  }
  const merged = existing.concat(fresh);
  if (needsSort) {
    merged.sort((a, b) => (a.seq < b.seq ? -1 : a.seq > b.seq ? 1 : 0));
  }
  return { events: merged, lastSeq, added: fresh.length };
}

/** Classifies whether an error from SendMessage is a transient network/offline issue that should auto-retry on reconnect. */
export function isTransientSendError(err: unknown): boolean {
  const msg = (err instanceof Error ? err.message : String(err ?? "")).toLowerCase();
  const code = typeof err === "object" && err !== null && "code" in err ? String((err as { code?: unknown }).code).toLowerCase() : "";
  // Permanent business/precondition errors should not spin in the background.
  if (
    code === "failed_precondition" ||
    code === "3" || // invalid_argument
    code === "9" || // failed_precondition
    code === "7" || // permission_denied
    code === "5" || // not_found
    code === "invalid_argument" ||
    code === "permission_denied" ||
    code === "not_found" ||
    msg.includes("archived") ||
    msg.includes("permission") ||
    msg.includes("not found") ||
    msg.includes("invalid")
  ) {
    return false;
  }
  return (
    code === "unavailable" ||
    code === "14" || // unavailable
    code === "deadline_exceeded" ||
    code === "4" || // deadline_exceeded
    code === "unknown" ||
    code === "2" ||
    code === "canceled" ||
    code === "1" ||
    msg.includes("offline") ||
    msg.includes("network") ||
    msg.includes("fetch") ||
    msg.includes("timeout") ||
    msg.includes("timed out") ||
    msg.includes("unavailable") ||
    msg.includes("connect") ||
    msg.includes("disconnected") ||
    msg.includes("reset") ||
    msg.includes("eof")
  );
}
