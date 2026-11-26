import { useEffect, useState } from "react";
import { local } from "../api";

// Pictures of a session, as their thumbnails (工单 #143, #156, #158): found by their SHA-256 (the machine keeps the
// file, and its name may change; its content does not). One this window sent is here at once; any other (sent from
// another device, sent before a restart, or shown by one of the agent's replies) the machine gives back
// (ReadAttachment) when it is first drawn, and it stays for as long as the window is open.

export type Picture = { url: string; name: string; size: number; sha256: string };

const kept = new Map<string, Picture>();
const most = 80;

async function sha256(data: Uint8Array): Promise<string> {
  const d = new Uint8Array(await crypto.subtle.digest("SHA-256", data as Uint8Array<ArrayBuffer>));
  return Array.from(d, (b) => b.toString(16).padStart(2, "0")).join("");
}

function keep(key: string, p: Picture) {
  kept.set(key, p);
  while (kept.size > most) {
    const [old, q] = kept.entries().next().value!;
    if (![...kept.values()].some((x) => x !== q && x.url === q.url)) URL.revokeObjectURL(q.url);
    kept.delete(old);
  }
}

/** Keeps the pictures of a message about to be sent. */
export async function rememberSent(files: { name?: string; mediaType: string; data: Uint8Array }[]): Promise<void> {
  for (const f of files) {
    if (!f.mediaType.startsWith("image/")) continue;
    const sum = await sha256(f.data);
    if (kept.has(sum)) continue;
    keep(sum, { url: URL.createObjectURL(new Blob([f.data as Uint8Array<ArrayBuffer>], { type: f.mediaType })), name: f.name ?? "", size: f.data.length, sha256: sum });
  }
}

/** The thumbnail of a picture this window sent, or "". */
export function sentPicture(sha: string): string {
  return kept.get(sha)?.url ?? "";
}

// At most this many pictures are asked for at once, so that large ones never hold up a session's events on a slow link.
const atOnce = 3;
let asking = 0;
const queue: (() => void)[] = [];
const asked = new Map<string, Promise<Picture | null>>();

async function slot<T>(f: () => Promise<T>): Promise<T> {
  if (asking >= atOnce) await new Promise<void>((go) => queue.push(go));
  asking++;
  try {
    return await f();
  } finally {
    asking--;
    queue.shift()?.();
  }
}

/** A picture of the session by its SHA-256 or, for one a reply showed that no turn kept yet, by its path; null when the machine has none (or can't say, being too old). */
function fetchPicture(sessionId: string, sha: string, path: string): Promise<Picture | null> {
  const key = sha || `${sessionId}\0${path}`;
  const have = kept.get(key);
  if (have) return Promise.resolve(have);
  let p = asked.get(key);
  if (!p) {
    p = slot(() => local.readAttachment({ sessionId, sha256: sha, path }))
      .then((res) => {
        const pic = { url: URL.createObjectURL(new Blob([res.data as Uint8Array<ArrayBuffer>], { type: res.mediaType })), name: res.name, size: Number(res.size), sha256: res.sha256 };
        keep(key, pic);
        if (!sha) keep(res.sha256, pic);
        return pic;
      })
      .catch(() => null)
      .finally(() => asked.delete(key));
    asked.set(key, p);
  }
  return p;
}

/** A picture to draw: "loading" until it comes, "gone" when the machine can't give it back (or it isn't asked for: off). */
export function usePicture(sessionId: string, sha: string, path: string, off = false): Picture | "loading" | "gone" {
  const key = sha || `${sessionId}\0${path}`;
  const [got, setGot] = useState<{ key: string; pic: Picture | null } | undefined>(() => (kept.has(key) ? { key, pic: kept.get(key)! } : undefined));
  useEffect(() => {
    if (off || !sessionId || (!sha && !path) || got?.key === key) return;
    let live = true;
    void fetchPicture(sessionId, sha, path).then((pic) => live && setGot({ key, pic }));
    return () => {
      live = false;
    };
  }, [key, off, sessionId, sha, path, got?.key]);
  if (off) return "gone";
  if (got?.key !== key) return kept.get(key) ?? "loading";
  return got.pic ?? "gone";
}
