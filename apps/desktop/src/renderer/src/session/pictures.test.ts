import assert from "node:assert/strict";
import { test } from "node:test";
import type { AttachmentInfo, SessionEvent } from "@aide/proto/session";
import { asWritten, firstPictures, isLocal, shownPictures } from "./pictures.ts";

const info = (path: string, sha256: string) => ({ path, sha256 }) as AttachmentInfo;

test("a reply's pictures are files on the machine, never web addresses or inline data (工单 #158)", () => {
  for (const src of ["/tmp/a.png", "shots/b.png", "file:///tmp/e.png", "C:\\shots\\w.png", "C:/shots/w.png"]) assert.ok(isLocal(src), src);
  for (const src of ["https://example.com/x.png", "http://example.com/x.png", "data:image/png;base64,AAAA", "//example.com/x.png", ""]) assert.ok(!isLocal(src), src);
  assert.equal(asWritten("shots/b%20c.png"), "shots/b c.png");
  assert.equal(asWritten("100%.png"), "100%.png");
});

test("the same picture twice in a reply is drawn once: where it first is", () => {
  const shot = info("/tmp/a.png", "aa");
  const same = info("/tmp/copy.png", "aa");
  const text = "![one](/tmp/a.png) then ![again](/tmp/a.png) and ![copy](/tmp/copy.png) ![b](<shots/b c.png>) ![b2](shots/b%20c.png)";
  const first = firstPictures(text, new Map([["/tmp/a.png", shot], ["/tmp/copy.png", same]]));
  assert.deepEqual([...first], [["aa", 0], ["shots/b c.png", text.indexOf("![b]")]]);
  // Before its turn kept them, by path.
  assert.deepEqual([...firstPictures(text, undefined)].map(([k]) => k), ["/tmp/a.png", "/tmp/copy.png", "shots/b c.png"]);
});

test("each reply of a turn finds the pictures the turn kept", () => {
  const shot = info("/tmp/a.png", "aa");
  const ev = (seq: bigint, turnId: string, kind: { case: string; value: object }) =>
    ({ seq, turnId, kind: { ...kind, value: { shown: [], ...kind.value } } }) as unknown as SessionEvent;
  const events = [
    ev(1n, "1", { case: "agentMessage", value: { text: "no picture" } }),
    ev(2n, "1", { case: "turnCompleted", value: { turn: 1 } }),
    ev(3n, "2", { case: "agentMessage", value: { text: "![a](/tmp/a.png)" } }),
    ev(4n, "2", { case: "agentMessage", value: { text: "and again ![a](/tmp/a.png)" } }),
    ev(5n, "2", { case: "turnCompleted", value: { turn: 2, shown: [shot] } }),
  ];
  const shown = shownPictures(events);
  assert.equal(shown.get(1n), undefined);
  assert.equal(shown.get(3n)?.get("/tmp/a.png")?.sha256, "aa");
  assert.equal(shown.get(4n), shown.get(3n));
});
