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

const STORAGE_KEY = "aide.outbox.v1";

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

function saveState(st: OutboxState) {
  try {
    if (typeof localStorage !== "undefined") localStorage.setItem(STORAGE_KEY, serializeOutboxState(st));
  } catch {
    // Storage full or unavailable; in-memory queue still works for the live process.
  }
}

type Listener = () => void;

class DesktopOutboxManager {
  private state: OutboxState = loadState();
  private listeners = new Set<Listener>();
  private flushing = new Set<string>();
  private retryTimer: ReturnType<typeof setTimeout> | undefined;
  private retryAttempt = 0;

  constructor() {
    saveState(this.state);
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

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private setState(next: OutboxState) {
    if (next === this.state) return;
    this.state = next;
    saveState(next);
    for (const fn of this.listeners) fn();
  }

  itemsFor(sessionId: string): OutboxMessage[] {
    return outboxForSession(this.state, sessionId);
  }

  enqueue(
    sessionId: string,
    text: string,
    attachments: Array<{ name: string; mediaType: string; data: Uint8Array }>,
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
    const { state, item } = enqueueOutbox(this.state, { id, sessionId, text, attachments: encoded });
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
    if (this.flushing.has(sessionId)) return;
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

export const desktopOutbox = new DesktopOutboxManager();

export function useSessionOutbox(sessionId: string): OutboxMessage[] {
  const [items, setItems] = useState<OutboxMessage[]>(() => desktopOutbox.itemsFor(sessionId));
  useEffect(() => {
    setItems(desktopOutbox.itemsFor(sessionId));
    return desktopOutbox.subscribe(() => setItems(desktopOutbox.itemsFor(sessionId)));
  }, [sessionId]);
  return items;
}
