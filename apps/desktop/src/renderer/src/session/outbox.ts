import { useEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { AttachmentSchema, type SessionEvent } from "@aide/proto/session";
import {
  emptyOutboxState,
  enqueueOutbox,
  isTransientSendError,
  markOutboxError,
  markOutboxSending,
  markOutboxSent,
  nextOutboxMessages,
  outboxForPersist,
  outboxForSession,
  parseOutboxState,
  reconcileOutboxWithEvents,
  recoverOutboxOnBoot,
  removeOutbox,
  retryOutbox,
  serializeOutboxState,
  type OutboxAttachment,
  type OutboxMessage,
  type OutboxState,
} from "@aide/core";
import { local } from "../api";
import { errorText } from "../store";
import { clearSessionEventCache, dropPersistedSessionEvents } from "./events";
import { isMainWindowHash } from "./keep";

const STORAGE_KEY = "aide.outbox.v1";
/** The account the locally kept conversations and queue belong to (owner_id). */
const OWNER_KEY = "aide.localConversations.owner";

/** Only the main window sends and writes the queue (see isMainWindowHash). */
export function isOutboxWindow(): boolean {
  return typeof location === "undefined" || isMainWindowHash(location.hash);
}

function toBase64(bytes: Uint8Array): string {
  let bin = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    bin += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(bin);
}

function fromBase64(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function loadState(): OutboxState {
  try {
    const raw = typeof localStorage !== "undefined" ? localStorage.getItem(STORAGE_KEY) : null;
    return recoverOutboxOnBoot(parseOutboxState(raw));
  } catch {
    return emptyOutboxState();
  }
}

/**
 * Persists the queue: attachments too large to keep (outboxForPersist) stay in memory
 * only; when storage refuses even that (over quota), the queue is kept without any
 * attachment bytes. Returns the messages whose files are in memory only, and why
 * nothing could be kept at all ("" when it was).
 */
function saveState(st: OutboxState): { memoryOnly: Set<string>; error: string } {
  if (typeof localStorage === "undefined") return { memoryOnly: new Set(), error: "" };
  const plan = outboxForPersist(st);
  try {
    localStorage.setItem(STORAGE_KEY, serializeOutboxState(plan.state));
    return { memoryOnly: new Set(plan.memoryOnly), error: "" };
  } catch {
    const bare = serializeOutboxState(outboxForPersist(st, 0).state);
    const all = new Set(st.items.filter((x) => x.attachments.length > 0).map((x) => x.id));
    try {
      localStorage.setItem(STORAGE_KEY, bare);
      return { memoryOnly: all, error: "" };
    } catch {
      // The conversations' cached events are only a cache: the queue goes first.
      dropPersistedSessionEvents();
      try {
        localStorage.setItem(STORAGE_KEY, bare);
        return { memoryOnly: all, error: "" };
      } catch (e) {
        return { memoryOnly: new Set(st.items.map((x) => x.id)), error: errorText(e) };
      }
    }
  }
}

type Listener = () => void;

/** How a queued message is kept: "memory" when it (or its files) would not survive closing the app. */
export type OutboxKeeping = "disk" | "memory";

class DesktopOutboxManager {
  private state: OutboxState = loadState();
  private listeners = new Set<Listener>();
  private flushing = new Set<string>();
  private retryTimer: ReturnType<typeof setTimeout> | undefined;
  private retryAttempt = 0;
  private memoryOnly = new Set<string>();
  /** Why the queue could not be persisted at all ("" when it was). */
  persistError = "";
  /** Only the main window sends and writes the queue (isOutboxWindow); the others only read it. */
  readonly active = isOutboxWindow();

  constructor() {
    if (typeof window !== "undefined") {
      // Another window cleared it (signing out) or, never with one owner, wrote it: take what is there.
      window.addEventListener("storage", (e) => {
        if (e.key !== STORAGE_KEY && e.key !== null) return;
        this.state = loadState();
        for (const fn of this.listeners) fn();
      });
    }
    if (!this.active) return;
    this.persist();
    if (typeof window !== "undefined") {
      window.addEventListener("online", () => {
        this.retryAttempt = 0;
        void this.flushAll();
      });
    }
    if (nextOutboxMessages(this.state).length > 0) {
      queueMicrotask(() => void this.flushAll());
    }
  }

  private persist() {
    if (!this.active) return;
    const r = saveState(this.state);
    this.memoryOnly = r.memoryOnly;
    this.persistError = r.error;
  }

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private setState(next: OutboxState) {
    if (next === this.state) return;
    this.state = next;
    this.persist();
    for (const fn of this.listeners) fn();
  }

  itemsFor(sessionId: string): OutboxMessage[] {
    return outboxForSession(this.state, sessionId);
  }

  /** "memory": closing the app now loses this message's files, or the whole queue could not be kept. */
  keeping(id: string): OutboxKeeping {
    return this.memoryOnly.has(id) ? "memory" : "disk";
  }

  /** Forgets the whole queue (signing out, another account). */
  clear() {
    this.retryAttempt = 0;
    this.setState(emptyOutboxState());
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch {
      // Nothing kept to remove.
    }
  }

  /**
   * afterSeq: the session's last event seq the window knows now, so that an older server
   * (no echoed client_message_id) only matches the message against later events.
   */
  enqueue(
    sessionId: string,
    text: string,
    attachments: Array<{ name: string; mediaType: string; data: Uint8Array }>,
    afterSeq?: bigint,
  ): OutboxMessage {
    const encoded: OutboxAttachment[] = attachments.map((a) => ({
      name: a.name,
      mimeType: a.mediaType,
      dataBase64: toBase64(a.data),
    }));
    const id =
      typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
        ? crypto.randomUUID()
        : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    const { state, item } = enqueueOutbox(this.state, { id, sessionId, text, attachments: encoded, afterSeq });
    this.setState(state);
    void this.flushSession(sessionId);
    return item;
  }

  retry(id: string) {
    const target = this.state.items.find((x) => x.id === id);
    if (!target) return;
    this.retryAttempt = 0;
    this.setState(retryOutbox(this.state, id));
    void this.flushSession(target.sessionId);
  }

  remove(id: string) {
    const target = this.state.items.find((x) => x.id === id);
    this.setState(removeOutbox(this.state, id));
    if (target) void this.flushSession(target.sessionId);
  }

  reconcile(sessionId: string, events: SessionEvent[]) {
    this.setState(reconcileOutboxWithEvents(this.state, sessionId, events));
  }

  async flushAll() {
    const ready = nextOutboxMessages(this.state);
    await Promise.all(ready.map((m) => this.flushSession(m.sessionId)));
  }

  async flushSession(sessionId: string) {
    if (!this.active || this.flushing.has(sessionId)) return;
    this.flushing.add(sessionId);
    try {
      while (true) {
        const ready = nextOutboxMessages(this.state, sessionId);
        const item = ready[0];
        if (!item) break;

        if (typeof navigator !== "undefined" && navigator.onLine === false) {
          this.setState(markOutboxError(this.state, item.id, "offline", true));
          this.scheduleRetry();
          break;
        }

        this.setState(markOutboxSending(this.state, item.id));
        try {
          const atts = item.attachments.map((a) =>
            create(AttachmentSchema, {
              name: a.name,
              mediaType: a.mimeType,
              data: fromBase64(a.dataBase64),
            }),
          );
          await local.sendMessage({
            sessionId: item.sessionId,
            text: item.text,
            attachments: atts,
            clientMessageId: item.id,
            clientSeq: BigInt(item.clientSeq),
          });
          this.retryAttempt = 0;
          this.setState(markOutboxSent(this.state, item.id));
        } catch (err) {
          const transient = isTransientSendError(err) || (typeof navigator !== "undefined" && navigator.onLine === false);
          this.setState(markOutboxError(this.state, item.id, errorText(err), transient));
          if (transient) this.scheduleRetry();
          break;
        }
      }
    } finally {
      this.flushing.delete(sessionId);
    }
  }

  private scheduleRetry() {
    if (this.retryTimer !== undefined) return;
    const delays = [2000, 5000, 10000, 15000];
    const delay = delays[Math.min(this.retryAttempt++, delays.length - 1)];
    this.retryTimer = setTimeout(() => {
      this.retryTimer = undefined;
      void this.flushAll();
    }, delay);
  }
}

let manager: DesktopOutboxManager | undefined;

/** The queue, made on first use (a window that never shows a conversation never makes one). */
export function desktopOutbox(): DesktopOutboxManager {
  manager ??= new DesktopOutboxManager();
  return manager;
}

export function useSessionOutbox(sessionId: string): OutboxMessage[] {
  const [items, setItems] = useState<OutboxMessage[]>(() => desktopOutbox().itemsFor(sessionId));
  useEffect(() => {
    const q = desktopOutbox();
    setItems(q.itemsFor(sessionId));
    return q.subscribe(() => setItems(q.itemsFor(sessionId)));
  }, [sessionId]);
  return items;
}

/** What this device knows of the account, as the status says (GetStatusResponse). */
export type AccountStatus = { ownerId: string; signedOut: boolean; removed: boolean; initialized: boolean; machineOnly: boolean };

/**
 * The conversations kept on this device (the queue, the cached events) are the signed-in
 * account's: signing out, being removed, or another account signing in forgets them.
 * Called by the main window whenever the status changes.
 */
export function guardLocalConversations(status: AccountStatus | null | undefined) {
  if (!status || !isOutboxWindow()) return;
  let kept: string | null = null;
  try {
    kept = localStorage.getItem(OWNER_KEY);
  } catch {
    return;
  }
  const member = (status.initialized || status.machineOnly) && !status.signedOut && !status.removed && status.ownerId !== "";
  const owner = member ? status.ownerId : "";
  if (kept === owner) return;
  // Kept for no account known yet (a first start after an update): it is this account's.
  if (kept !== null || !member) {
    desktopOutbox().clear();
    clearSessionEventCache();
  }
  try {
    localStorage.setItem(OWNER_KEY, owner);
  } catch {
    // Next time again.
  }
}
