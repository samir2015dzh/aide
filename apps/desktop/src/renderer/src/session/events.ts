import { useEffect, useRef, useState } from "react";
import { fromJsonString, toJsonString } from "@bufbuild/protobuf";
import { EventView, SessionEventSchema, type SessionEvent } from "@aide/proto/session";
import { mergeSessionEvents } from "@aide/core";
import { local } from "../api";
import { followAgainIn } from "./retry";

/** A session's stored events, and the reply the agent is still writing ("" when none). */
export type Conversation = { events: SessionEvent[]; draft: string };

/**
 * The conversations opened last, the newest last: going back to one shows
 * it at once (also while its machine cannot be reached), and its stream
 * picks up after what it holds. Also persisted to localStorage (工单 #115)
 * so restarting the app resumes from lastSeq without gaps or duplicates.
 */
const kept = new Map<string, SessionEvent[]>();
const keepAtMost = 10;
const persistPrefix = "aide.sessionEvents.v1:";
const persistIndexKey = "aide.sessionEvents.v1:index";
const persistMaxEvents = 250;

function loadPersisted(id: string): SessionEvent[] {
  try {
    if (typeof localStorage === "undefined") return [];
    const raw = localStorage.getItem(persistPrefix + id);
    if (!raw) return [];
    const arr = JSON.parse(raw) as unknown;
    if (!Array.isArray(arr)) return [];
    const events: SessionEvent[] = [];
    for (const s of arr) {
      if (typeof s === "string") events.push(fromJsonString(SessionEventSchema, s));
    }
    return mergeSessionEvents([], events).events;
  } catch {
    return [];
  }
}

function savePersisted(id: string, events: SessionEvent[]) {
  try {
    if (typeof localStorage === "undefined") return;
    const slice = events.length > persistMaxEvents ? events.slice(-persistMaxEvents) : events;
    localStorage.setItem(persistPrefix + id, JSON.stringify(slice.map((e) => toJsonString(SessionEventSchema, e))));
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

function cached(id: string): SessionEvent[] {
  const mem = kept.get(id);
  if (mem) return mem;
  const disk = loadPersisted(id);
  if (disk.length > 0) kept.set(id, disk);
  return disk;
}

function keep(id: string, events: SessionEvent[]) {
  kept.delete(id);
  kept.set(id, events);
  if (kept.size > keepAtMost) kept.delete(kept.keys().next().value!);
  savePersisted(id, events);
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
  const [got, setGot] = useState<Conversation & { id: string }>(() => ({ id: sessionId, events: cached(sessionId), draft: "" }));
  const known = useRef(last);
  const wakeRef = useRef<(() => void) | null>(null);
  known.current = last;
  useEffect(() => {
    wakeRef.current?.();
  }, [last]);

  useEffect(() => {
    const stop = new AbortController();
    const held = cached(sessionId);
    let caughtUp = held.length > 0;
    if (caughtUp) setGot((g) => (g.id === sessionId ? g : { id: sessionId, events: held, draft: "" }));
    let batch: SessionEvent[] = [];
    let draft = "";
    let timer: ReturnType<typeof setTimeout> | undefined;
    const flush = () => {
      timer = undefined;
      const add = batch;
      batch = [];
      setGot((g) => {
        const base = g.id === sessionId ? g.events : held;
        const merged = add.length ? mergeSessionEvents(base, add).events : base;
        if (merged !== base) keep(sessionId, merged);
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
      let after = held.reduce((m, e) => (e.seq > m ? e.seq : m), 0n);
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
          const t = setTimeout(() => {
            wakeSleep = null;
            resolve();
          }, again);
          wakeSleep = () => {
            clearTimeout(t);
            resolve();
          };
          stop.signal.addEventListener("abort", () => {
            clearTimeout(t);
            wakeSleep = null;
            resolve();
          }, { once: true });
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
  return got.id === sessionId ? got : { events: cached(sessionId), draft: "" };
}
