import type { AttachmentInfo, SessionEvent } from "@aide/proto/session";

// Which pictures an agent's reply shows (工单 #158), worked out from its text and its turn's events.

/** By the seq of each reply event of a turn: the pictures the turn kept (TurnCompleted.shown), by the path the reply wrote. */
export type ShownPictures = Map<bigint, Map<string, AttachmentInfo>>;

/** Each reply event's turn's kept pictures. */
export function shownPictures(events: SessionEvent[]): ShownPictures {
  const out: ShownPictures = new Map();
  const replies = new Map<string, bigint[]>();
  for (const e of events) {
    if (e.kind.case === "agentMessage") {
      const l = replies.get(e.turnId) ?? [];
      l.push(e.seq);
      replies.set(e.turnId, l);
    } else if (e.kind.case === "turnCompleted" && e.kind.value.shown.length > 0) {
      const m = new Map(e.kind.value.shown.map((f) => [f.path, f]));
      for (const seq of replies.get(e.turnId) ?? []) out.set(seq, m);
    }
  }
  return out;
}

// A Markdown image, ![alt](path "title"), the path maybe in <>: as the machine reads them (internal/machine/shown.go).
const image = /!\[[^\]\n]*\]\(\s*(<[^>\n]+>|[^)\s]+)(?:\s+"[^"\n]*")?\s*\)/g;

/** A file on the machine (a path, a file: URL, a Windows drive path), not a web address or inline data. */
export function isLocal(src: string): boolean {
  if (/^file:/i.test(src) || /^[A-Za-z]:[\\/]/.test(src)) return true;
  return src !== "" && !src.startsWith("//") && !/^[a-z][a-z0-9+.-]*:/i.test(src);
}

/** An image's path as the reply wrote it: the renderer gives it URL-encoded (spaces, non-ASCII names). */
export function asWritten(src: string): string {
  try {
    return decodeURI(src);
  } catch {
    return src;
  }
}

/** Where each picture of a reply first is (its key: the SHA-256 the turn kept it under, else its path). */
export function firstPictures(text: string, kept: Map<string, AttachmentInfo> | undefined): Map<string, number> {
  const first = new Map<string, number>();
  for (const m of text.matchAll(image)) {
    const src = asWritten(m[1].replace(/^<|>$/g, ""));
    const key = kept?.get(src)?.sha256 || src;
    if (!first.has(key)) first.set(key, m.index);
  }
  return first;
}
