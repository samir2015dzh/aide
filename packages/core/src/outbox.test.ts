import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
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
  outboxForPersist,
  nextOutboxMessages,
  outboxForSession,
  parseOutboxState,
  reconcileOutboxWithEvents,
  recoverOutboxOnBoot,
  removeOutbox,
  retryOutbox,
  serializeOutboxState,
} from "./outbox";

function userEvent(seq: bigint, text: string, clientMessageIds: string[] = []) {
  return create(SessionEventSchema, {
    seq,
    kind: { case: "userMessage", value: { text, clientMessageIds } },
  });
}

function answerEvent(seq: bigint, text: string, clientMessageIds: string[]) {
  return create(SessionEventSchema, {
    seq,
    kind: { case: "questionResolved", value: { questionId: "q1", answers: [{ itemId: "i1", text }], clientMessageIds } },
  });
}

const attachment = { name: "notes.txt", mimeType: "text/plain", dataBase64: "aGVsbG8=" };

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

  it("reconciles by the client_message_id the server echoes, whatever the recorded text", () => {
    let st = emptyOutboxState();
    // Sent with a file (the server lists it after the text), a !macro (expanded), an answer.
    st = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "look", attachments: [attachment], afterSeq: 3n }).state;
    st = enqueueOutbox(st, { id: "m2", sessionId: "s1", text: "!fix", afterSeq: 3n }).state;
    st = enqueueOutbox(st, { id: "m3", sessionId: "s1", text: "red", afterSeq: 3n }).state;
    for (const id of ["m1", "m2", "m3"]) st = markOutboxSent(markOutboxSending(st, id), id);

    const events = [
      userEvent(4n, "look\n\n[Aide] Attached files, saved on this machine outside the worktree:\n- notes.txt", ["m1"]),
      userEvent(5n, "Fix the failing test, then run it again.", ["m2"]),
    ];
    st = reconcileOutboxWithEvents(st, "s1", events);
    expect(outboxForSession(st, "s1").map((x) => x.id)).toEqual(["m3"]);

    // An answer to the agent's question records QuestionResolved, not UserMessage.
    st = reconcileOutboxWithEvents(st, "s1", [...events, answerEvent(6n, "Red", ["m3"])]);
    expect(outboxForSession(st, "s1")).toEqual([]);
  });

  it("does not clear a new marker on an older message with the same text", () => {
    let st = emptyOutboxState();
    st = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "continue", afterSeq: 10n }).state;
    st = markOutboxSent(st, "m1");
    // The id-echoing server: an older "continue" (no id, or another id) is not this message.
    const withIds = [userEvent(2n, "continue"), userEvent(8n, "continue", ["older"])];
    st = reconcileOutboxWithEvents(st, "s1", withIds);
    expect(outboxForSession(st, "s1").map((x) => x.id)).toEqual(["m1"]);
    // An older server (no ids anywhere): only events after the one it was enqueued behind.
    const old = [userEvent(2n, "continue"), userEvent(8n, "continue")];
    st = reconcileOutboxWithEvents(st, "s1", old);
    expect(outboxForSession(st, "s1").map((x) => x.id)).toEqual(["m1"]);
    st = reconcileOutboxWithEvents(st, "s1", [...old, userEvent(11n, "continue")]);
    expect(outboxForSession(st, "s1")).toEqual([]);
  });

  it("falls back to text after enqueue on an older server, files listed after the text", () => {
    let st = emptyOutboxState();
    st = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "hello", afterSeq: 0n }).state;
    st = enqueueOutbox(st, { id: "m2", sessionId: "s1", text: "look", attachments: [attachment], afterSeq: 0n }).state;
    st = markOutboxSent(st, "m1");
    st = markOutboxSent(st, "m2");

    st = reconcileOutboxWithEvents(st, "s1", [userEvent(1n, "hello")]);
    expect(outboxForSession(st, "s1").map((x) => x.id)).toEqual(["m2"]);

    st = reconcileOutboxWithEvents(st, "s1", [userEvent(1n, "hello"), userEvent(2n, "look\n\n[Aide] Attached files, saved on this machine outside the worktree:\n- x")]);
    expect(outboxForSession(st, "s1")).toEqual([]);
  });

  it("matches an entry persisted before afterSeq existed by event time on an older server", () => {
    let st = emptyOutboxState();
    st = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "again", now: "2026-11-25T00:10:00Z" }).state;
    st = markOutboxSent(st, "m1");
    const at = (seq: bigint, iso: string) => {
      const e = userEvent(seq, "again");
      e.ts = timestampFromDate(new Date(iso));
      return e;
    };
    st = reconcileOutboxWithEvents(st, "s1", [at(1n, "2026-11-25T00:00:00Z")]);
    expect(outboxForSession(st, "s1").map((x) => x.id)).toEqual(["m1"]);
    st = reconcileOutboxWithEvents(st, "s1", [at(1n, "2026-11-25T00:00:00Z"), at(2n, "2026-11-25T00:10:01Z")]);
    expect(outboxForSession(st, "s1")).toEqual([]);
  });

  it("drops an echoed entry that is still pending (its answer was lost, the server has it)", () => {
    let st = emptyOutboxState();
    st = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "hi" }).state;
    st = markOutboxError(markOutboxSending(st, "m1"), "m1", "timeout", true);
    expect(outboxForSession(st, "s1")[0].attempted).toBe(true);
    st = reconcileOutboxWithEvents(st, "s1", [userEvent(1n, "hi", ["m1"])]);
    expect(outboxForSession(st, "s1")).toEqual([]);
  });

  it("drops attachment bytes once delivered and bounds what is persisted", () => {
    let st = emptyOutboxState();
    const big = { name: "big.bin", mimeType: "application/octet-stream", dataBase64: "A".repeat(100) };
    st = enqueueOutbox(st, { id: "m1", sessionId: "s1", text: "small", attachments: [attachment], now: "2026-11-25T00:00:01Z" }).state;
    st = enqueueOutbox(st, { id: "m2", sessionId: "s1", text: "big", attachments: [big], now: "2026-11-25T00:00:02Z" }).state;
    st = enqueueOutbox(st, { id: "m3", sessionId: "s1", text: "after", now: "2026-11-25T00:00:03Z" }).state;

    // The large one is kept in memory only; the rest of the queue still persists.
    const { state: persisted, memoryOnly } = outboxForPersist(st, 50);
    expect(memoryOnly).toEqual(["m2"]);
    expect(st.items[1].attachments[0].dataBase64).toHaveLength(100);
    const restored = recoverOutboxOnBoot(parseOutboxState(serializeOutboxState(persisted)));
    const list = outboxForSession(restored, "s1");
    expect(list.map((x) => [x.id, x.status, Boolean(x.attachmentsLost)])).toEqual([
      ["m1", "pending", false],
      ["m2", "failed", true],
      ["m3", "pending", false],
    ]);
    expect(list[0].attachments[0].dataBase64).toBe(attachment.dataBase64);
    // A message whose files are gone cannot be retried as it was: the person removes it (and attaches them again).
    expect(outboxForSession(retryOutbox(restored, "m2"), "s1")[1].status).toBe("failed");

    st = markOutboxSent(st, "m1");
    expect(outboxForSession(st, "s1")[0].attachments).toEqual([{ ...attachment, dataBase64: "" }]);
    expect(serializeOutboxState(st)).not.toContain(attachment.dataBase64);
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
