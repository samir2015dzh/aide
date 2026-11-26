// Pictures this window sent (工单 #143): a message's bubble shows each as a thumbnail, found by its SHA-256 (the
// machine keeps the file, and its name may change; its content does not). Only those sent from here, for as long as
// the window is open: the machine has no way to give a file back yet, so one sent elsewhere shows by name.

const kept = new Map<string, string>();
const most = 60;

async function sha256(data: Uint8Array): Promise<string> {
  const d = new Uint8Array(await crypto.subtle.digest("SHA-256", data as Uint8Array<ArrayBuffer>));
  return Array.from(d, (b) => b.toString(16).padStart(2, "0")).join("");
}

/** Keeps the pictures of a message about to be sent. */
export async function rememberSent(files: { mediaType: string; data: Uint8Array }[]): Promise<void> {
  for (const f of files) {
    if (!f.mediaType.startsWith("image/")) continue;
    const sum = await sha256(f.data);
    if (kept.has(sum)) continue;
    kept.set(sum, URL.createObjectURL(new Blob([f.data as Uint8Array<ArrayBuffer>], { type: f.mediaType })));
    while (kept.size > most) {
      const [old, url] = kept.entries().next().value!;
      URL.revokeObjectURL(url);
      kept.delete(old);
    }
  }
}

/** The thumbnail of a picture this window sent, or "". */
export function sentPicture(sha: string): string {
  return kept.get(sha) ?? "";
}
