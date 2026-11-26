import { create } from "@bufbuild/protobuf";
import { SessionEventSchema } from "@aide/proto/session";
import { describe, expect, it } from "vitest";
import {
  emptyOutboxState,
  enqueueOutbox,
  isTransientSendError,
  markOutboxError,
  markOutboxSending,
  markOutboxSent,
  mergeSessionEvents,
  nextOutboxMessages,
  outboxForSession,
  parseOutboxState,
  reconcileOutboxWithEvents,
  recoverOutboxOnBoot,
  removeOutbox,
  retryOutbox,
  serializeOutboxState,
} from "./outbox";

function userEvent(seq: bigint, text: string) {
  return create(SessionEventSchema, {
    seq,
    kind: { case: "userMessage", value: { text } },
  });
}

describe("outbox (工单 #115)", () => {
  it("enqueues with strictly monotonic per-session clientSeq and preserves FIFO order", () => {
    let st = emptyOutboxState();
    const r1 = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "first", now: "2026-11-25T00:00:01Z" });
    st = r1.state;
    const r2 = enqueueOutbox(st, { id: "m2", sessionId: "s1", text: "second", now: "2026-11-25T00:00:02Z" });
    st = r2.state;
    const r3 = enqueueOutbox(st, { id: "m3", sessionId: "s2", text: "other", now: "2026-11-25T00:00:03Z" });
    st = r3.state;

    expect(r1.item.clientSeq).toBe(1);
    expect(r2.item.clientSeq).toBe(2);
    expect(r3.item.clientSeq).toBe(1);

    // Next eligible sends head of s1 (#1) and head of s2 (#1), never skipping to s1 #2.
    expect(nextOutboxMessages(st).map((x) => x.id)).toEqual(["m1", "m3"]);

    // While m1 is sending, m2 waits behind it.
    st = markOutboxSending(st, "m1");
    expect(nextOutboxMessages(st, "s1")).toEqual([]);
    expect(nextOutboxMessages(st, "s2").map((x) => x.id)).toEqual(["m3"]);

    // Once m1 is sent, m2 becomes eligible.
    st = markOutboxSent(st, "m1");
    expect(nextOutboxMessages(st, "s1").map((x) => x.id)).toEqual(["m2"]);
  });

  it("survives app restart (serialize/parse + recoverOutboxOnBoot) keeping same idempotency id", () => {
    let st = emptyOutboxState();
    st = enqueueOutbox(st, { id: "cmid-1", sessionId: "s1", text: "offline 1" }).state;
    st = enqueueOutbox(st, { id: "cmid-2", sessionId: "s1", text: "offline 2" }).state;
    st = markOutboxSending(st, "cmid-1");

    const raw = serializeOutboxState(st);
    const restored = recoverOutboxOnBoot(parseOutboxState(raw), "2026-11-25T00:01:00Z");

    const list = outboxForSession(restored, "s1");
    expect(list.map((x) => [x.id, x.clientSeq, x.status])).toEqual([
      ["cmid-1", 1, "pending"],
      ["cmid-2", 2, "pending"],
    ]);
    // Next enqueued item after restart continues monotonic sequence (#3).
    const r3 = enqueueOutbox(restored, { id: "cmid-3", sessionId: "s1", text: "offline 3" });
    expect(r3.item.clientSeq).toBe(3);
  });

  it("handles transient vs permanent errors and manual retry/remove", () => {
    let st = emptyOutboxState();
    st = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "hi" }).state;
    st = enqueueOutbox(st, { id: "m2", sessionId: "s1", text: "next" }).state;

    // Transient network error keeps item pending so reconnect auto-flushes.
    st = markOutboxError(st, "m1", "offline", true);
    expect(outboxForSession(st, "s1")[0].status).toBe("pending");
    expect(nextOutboxMessages(st, "s1").map((x) => x.id)).toEqual(["m1"]);

    // Permanent failure blocks m2 until retried or removed.
    st = markOutboxError(st, "m1", "session archived", false);
    expect(outboxForSession(st, "s1")[0].status).toBe("failed");
    expect(nextOutboxMessages(st, "s1")).toEqual([]);

    st = retryOutbox(st, "m1");
    expect(outboxForSession(st, "s1")[0].status).toBe("pending");
    expect(nextOutboxMessages(st, "s1").map((x) => x.id)).toEqual(["m1"]);

    st = removeOutbox(st, "m1");
    expect(nextOutboxMessages(st, "s1").map((x) => x.id)).toEqual(["m2"]);
  });

  it("reconciles sent items once confirmed UserMessage arrives in session stream", () => {
    let st = emptyOutboxState();
    st = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "hello" }).state;
    st = enqueueOutbox(st, { id: "m2", sessionId: "s1", text: "world" }).state;
    st = markOutboxSent(st, "m1");
    st = markOutboxSent(st, "m2");

    st = reconcileOutboxWithEvents(st, "s1", [userEvent(1n, "hello")]);
    expect(outboxForSession(st, "s1").map((x) => x.id)).toEqual(["m2"]);

    st = reconcileOutboxWithEvents(st, "s1", [userEvent(1n, "hello"), userEvent(2n, "world")]);
    expect(outboxForSession(st, "s1")).toEqual([]);
  });

  it("reconciles a message sent with files: its event has the machine's list of them after its words (工单 #143)", () => {
    let st = emptyOutboxState();
    st = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "files" }).state;
    st = markOutboxSent(st, "m1");
    const ev = create(SessionEventSchema, {
      seq: 1n,
      kind: { case: "userMessage", value: { text: "files\n\n[Aide] Attached files, saved on this machine outside the worktree:\n- /m/a/notes.txt (text/plain, 19 bytes)", attachments: [{ name: "notes.txt" }] } },
    });
    st = reconcileOutboxWithEvents(st, "s1", [ev]);
    expect(outboxForSession(st, "s1")).toEqual([]);
  });

  it("classifies transient vs permanent send errors", () => {
    expect(isTransientSendError(new Error("Failed to fetch"))).toBe(true);
    expect(isTransientSendError({ code: "unavailable", message: "offline" })).toBe(true);
    expect(isTransientSendError({ code: "failed_precondition", message: "session is archived" })).toBe(false);
    expect(isTransientSendError({ code: "permission_denied", message: "forbidden" })).toBe(false);
  });
});

describe("mergeSessionEvents (工单 #115)", () => {
  it("deduplicates overlapping reconnect events and keeps ascending seq order", () => {
    const initial = [userEvent(1n, "a"), userEvent(2n, "b")];
    // Reconnect replays seq 2 (overlap) and catches up 3, 4.
    const r1 = mergeSessionEvents(initial, [userEvent(2n, "b"), userEvent(3n, "c"), userEvent(4n, "d")]);
    expect(r1.added).toBe(2);
    expect(r1.lastSeq).toBe(4n);
    expect(r1.events.map((e) => e.seq)).toEqual([1n, 2n, 3n, 4n]);

    // Exact duplicate batch adds nothing and preserves reference.
    const r2 = mergeSessionEvents(r1.events, [userEvent(3n, "c"), userEvent(4n, "d")]);
    expect(r2.added).toBe(0);
    expect(r2.events).toBe(r1.events);
  });
});
