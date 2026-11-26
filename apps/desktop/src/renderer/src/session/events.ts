import { useEffect, useRef, useState } from "react";
import { fromJsonString, toJsonString } from "@bufbuild/protobuf";
import { EventView, SessionEventSchema, type SessionEvent } from "@aide/proto/session";
import { mergeSessionEvents } from "@aide/core";
import { local } from "../api";
import { keptRecord, readKeptRecord, resumeAfter } from "./keep";
import { followAgainIn } from "./retry";

/** A session's stored events, and the reply the agent is still writing ("" when none). */
export type Conversation = { events: SessionEvent[]; draft: string };

/**
 * The conversations opened last, the newest last: going back to one shows
 * it at once (also while its machine cannot be reached), and its stream
 * picks up after what it holds. Also persisted to localStorage (工单 #115)
 * so restarting the app shows them at once: a conversation kept whole
 * resumes after its last event; one kept only in part (its last
 * persistMaxEvents) is read again from its start, so that no earlier turn
 * goes missing.
 */
export type Held = { events: SessionEvent[]; whole: boolean };
const kept = new Map<string, Held>();
const keepAtMost = 10;
const persistPrefix = "aide.sessionEvents.v1:";
const persistIndexKey = "aide.sessionEvents.v1:index";
const persistMaxEvents = 250;

function loadPersisted(id: string): Held {
  try {
    if (typeof localStorage === "undefined") return { events: [], whole: false };
    const rec = readKeptRecord(localStorage.getItem(persistPrefix + id));
    const events = rec.events.map((s) => fromJsonString(SessionEventSchema, s));
    return { events: mergeSessionEvents([], events).events, whole: rec.whole };
  } catch {
    return { events: [], whole: false };
  }
}

function savePersisted(id: string, held: Held) {
  try {
    if (typeof localStorage === "undefined") return;
    localStorage.setItem(persistPrefix + id, keptRecord(held.whole, held.events.map((e) => toJsonString(SessionEventSchema, e)), persistMaxEvents));
    const rawIdx = localStorage.getItem(persistIndexKey);
    const idx: string[] = rawIdx ? (JSON.parse(rawIdx) as string[]).filter((x) => x !== id) : [];
    idx.push(id);
    while (idx.length > keepAtMost) {
      const evicted = idx.shift();
      if (evicted) localStorage.removeItem(persistPrefix + evicted);
    }
    localStorage.setItem(persistIndexKey, JSON.stringify(idx));
  } catch {
    // Storage quota or unavailable; in-memory cache still holds the conversation.
  }
}

function cached(id: string): Held {
  const mem = kept.get(id);
  if (mem) return mem;
  const disk = loadPersisted(id);
  if (disk.events.length > 0) kept.set(id, disk);
  return disk;
}

function keep(id: string, held: Held) {
  kept.delete(id);
  kept.set(id, held);
  if (kept.size > keepAtMost) kept.delete(kept.keys().next().value!);
  savePersisted(id, held);
}

/** Forgets every conversation kept on this device, in memory and on disk (signing out). */
export function clearSessionEventCache() {
  kept.clear();
  dropPersistedSessionEvents();
}

/** Frees the disk copies of conversations (the ones in memory stay): storage is needed for something else. */
export function dropPersistedSessionEvents() {
  try {
    if (typeof localStorage === "undefined") return;
    const gone: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k?.startsWith(persistPrefix)) gone.push(k);
    }
    for (const k of gone) localStorage.removeItem(k);
  } catch {
    // Nothing kept to remove.
  }
}

/**
 * A session's events (BASIC: a long tool output as its preview, read whole
 * when its row opens), followed while the page shows it: the stream picks
 * up after the last event it got when it breaks (as the Mac app did), strictly
 * deduplicating by server seq and waking immediately when connectivity returns
 * (工单 #115). last is the session's last event as the list knows it: the events
 * up to it show as soon as they are all in.
 */
export function useSessionEvents(sessionId: string, last: bigint): Conversation {
  const [got, setGot] = useState<Conversation & { id: string }>(() => ({ id: sessionId, events: cached(sessionId).events, draft: "" }));
  const known = useRef(last);
  const wakeRef = useRef<(() => void) | null>(null);
  known.current = last;
  useEffect(() => {
    wakeRef.current?.();
  }, [last]);

  useEffect(() => {
    const stop = new AbortController();
    const held = cached(sessionId);
    // Held in part: read again from the start, the held tail showing meanwhile.
    let whole = held.whole || held.events.length === 0;
    const firstHeld = held.events.find((e) => e.seq > 0n)?.seq ?? 0n;
    let caughtUp = held.events.length > 0 && held.whole;
    if (held.events.length > 0) setGot((g) => (g.id === sessionId ? g : { id: sessionId, events: held.events, draft: "" }));
    let batch: SessionEvent[] = [];
    let draft = "";
    let timer: ReturnType<typeof setTimeout> | undefined;
    const flush = () => {
      timer = undefined;
      const add = batch;
      batch = [];
      setGot((g) => {
        const base = g.id === sessionId ? g.events : held.events;
        const merged = add.length ? mergeSessionEvents(base, add).events : base;
        if (merged !== base) keep(sessionId, { events: merged, whole });
        return { id: sessionId, events: merged, draft };
      });
    };

    let wakeSleep: (() => void) | null = null;
    const wake = () => {
      const fn = wakeSleep;
      wakeSleep = null;
      fn?.();
    };
    wakeRef.current = wake;
    const onOnline = () => wake();
    if (typeof window !== "undefined") window.addEventListener("online", onOnline);

    (async () => {
      let after = resumeAfter(held.events.map((e) => e.seq), held.whole);
      let broken = 0; // times in a row the stream broke with nothing received
      while (!stop.signal.aborted) {
        let again = 3000;
        try {
          for await (const e of local.streamSessionEvents({ sessionId, afterSeq: after, follow: true, partial: true, view: EventView.BASIC }, { signal: stop.signal })) {
            broken = 0;
            if (e.kind.case === "agentDraft") {
              draft = e.kind.value.whole ? e.kind.value.text : draft + e.kind.value.text;
              timer ??= setTimeout(flush, 30);
              continue;
            }
            if (e.seq > 0n && e.seq <= after) continue; // strict seq deduplication on reconnect overlap
            if (e.seq > after) after = e.seq;
            // Read from the start up to the held tail: what is held is now the whole conversation.
            if (!whole && e.seq >= firstHeld) whole = true;
            batch.push(e);
            draft = "";
            if (!caughtUp && e.seq >= known.current) {
              caughtUp = true;
              clearTimeout(timer);
              flush();
            } else timer ??= setTimeout(flush, caughtUp ? 30 : 1000);
          }
        } catch {
          again = followAgainIn(broken++);
        }
        if (stop.signal.aborted) break;
        await new Promise<void>((resolve) => {
          const done = () => {
            clearTimeout(t);
            wakeSleep = null;
            stop.signal.removeEventListener("abort", done);
            resolve();
          };
          const t = setTimeout(done, again);
          wakeSleep = done;
          stop.signal.addEventListener("abort", done, { once: true });
        });
      }
    })();
    return () => {
      wakeRef.current = null;
      if (typeof window !== "undefined") window.removeEventListener("online", onOnline);
      stop.abort();
      clearTimeout(timer);
    };
  }, [sessionId]);
  return got.id === sessionId ? got : { events: cached(sessionId).events, draft: "" };
}
