import { describe, expect, test } from "vitest";
import { admitAttachments, attachmentKind, attachmentNote, maxAttachments, shownAsImage, sizeText } from "./attachments";

const f = (name: string, type: string, size = 10) => ({ name, type, size });

describe("attachments (工单 #91)", () => {
  test("images, audio and every other file, by type or, without one, by name", () => {
    expect(attachmentKind(f("shot.png", "image/png"))).toBe("image");
    expect(attachmentKind(f("memo.m4a", "audio/x-m4a"))).toBe("audio");
    expect(attachmentKind(f("memo.m4a", ""))).toBe("audio");
    expect(attachmentKind(f("notes.txt", "text/plain"))).toBe("file");
    expect(attachmentKind(f("build.tar.gz", ""))).toBe("file");
  });

  test("an image is shown as one when the agent can look at it; otherwise it gets the file, said so", () => {
    expect(shownAsImage(f("a.png", "image/png", 3 << 20))).toBe(true);
    expect(attachmentNote(f("a.png", "image/png", 4 << 20))).toMatch(/3\.5 MB/);
    expect(attachmentNote(f("a.heic", "image/heic"))).not.toBe("");
    expect(attachmentNote(f("a.txt", "text/plain"))).toBe("");
  });

  test("what does not fit is refused by kind and name; the rest is taken", () => {
    const have = Array.from({ length: maxAttachments - 1 }, (_, i) => f(`${i}.txt`, "text/plain"));
    const r = admitAttachments(have, [f("one.png", "image/png"), f("two.mp3", "audio/mpeg")]);
    expect(r.take.map((x) => x.name)).toEqual(["one.png"]);
    expect(r.refused).toEqual([expect.stringContaining("two.mp3")]);
    expect(r.refused[0]).toMatch(/^音频/);
    const big = admitAttachments([], [f("huge.zip", "application/zip", 33 << 20), f("small.txt", "text/plain")]);
    expect(big.take.map((x) => x.name)).toEqual(["small.txt"]);
    expect(big.refused[0]).toContain("32 MB");
  });

  test("sizes as people read them", () => {
    expect([sizeText(820), sizeText(12 * 1024), sizeText(3.4 * 1024 * 1024), sizeText(32 << 20)]).toEqual(["820 B", "12 KB", "3.4 MB", "32 MB"]);
  });
});
