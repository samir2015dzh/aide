import { t } from "./i18n";

// What goes with a message (工单 #91): images, audio and any other file. The machine takes at most 20 files and 32 MiB
// per message, saves each beside the session and lists its path in the message; PNG, JPEG, GIF and WebP images up to
// 3.5 MiB are also shown to the agent as images (internal/machine/attach.go). What does not fit is said, by kind and
// name, before anything is sent: never dropped unsaid.

export const maxAttachments = 20;
export const maxAttachmentBytes = 32 << 20;
export const maxInlineImage = 3584 << 10;
const inlineImages = ["image/png", "image/jpeg", "image/gif", "image/webp"];

export type AttachmentKind = "image" | "audio" | "file";

/** A file about to go with a message. */
export type PendingFile = { name: string; type: string; size: number };

const audioExt = /\.(mp3|wav|m4a|aac|ogg|oga|opus|flac|amr|wma|aiff?|caf|3gp)$/i;
const imageExt = /\.(png|jpe?g|gif|webp|heic|heif|bmp|tiff?|svg|avif)$/i;

export function attachmentKind(f: Pick<PendingFile, "name" | "type">): AttachmentKind {
  if (f.type.startsWith("image/") || (f.type === "" && imageExt.test(f.name))) return "image";
  if (f.type.startsWith("audio/") || (f.type === "" && audioExt.test(f.name))) return "audio";
  return "file";
}

/** Whether the agent is shown it as an image (an agent that takes images): else it gets the file. */
export function shownAsImage(f: Pick<PendingFile, "type" | "size">): boolean {
  return inlineImages.includes(f.type) && f.size <= maxInlineImage;
}

/** A size as people read it: 820 B, 12 KB, 3.4 MB. */
export function sizeText(n: number): string {
  if (n >= 1 << 20) return `${(n / (1 << 20)).toFixed(1).replace(/\.0$/, "")} MB`;
  if (n >= 1 << 10) return `${Math.round(n / (1 << 10))} KB`;
  return `${n} B`;
}

/** How the agent gets it, when that is not what the owner might think (an image it is not shown as one). */
export function attachmentNote(f: PendingFile): string {
  if (attachmentKind(f) !== "image" || shownAsImage(f)) return "";
  return t(inlineImages.includes(f.type) ? "attach.imageTooBig" : "attach.imageFormat");
}

/** Which of the new files fit with those already there; why each other does not, in words naming its kind. */
export function admitAttachments<T extends PendingFile>(have: readonly T[], add: readonly T[]): { take: T[]; refused: string[] } {
  const take: T[] = [];
  const refused: string[] = [];
  let count = have.length;
  let bytes = have.reduce((n, f) => n + f.size, 0);
  for (const f of add) {
    const kind = t(`attach.kind.${attachmentKind(f)}`);
    if (count >= maxAttachments) refused.push(t("attach.tooMany", { kind, name: f.name, max: maxAttachments }));
    else if (bytes + f.size > maxAttachmentBytes) refused.push(t("attach.tooBig", { kind, name: f.name, max: sizeText(maxAttachmentBytes) }));
    else {
      take.push(f);
      count++;
      bytes += f.size;
    }
  }
  return { take, refused };
}

/**
 * Whether a paste attaches its files (工单 #143, after upstream fb7b9fc0): a screenshot, a picture copied or files
 * copied in the file manager do. One with words does not when none of its files is a file on this computer: an app
 * that copies words (a spreadsheet's cells, a slide's text) adds a picture of them, and the owner meant the words.
 */
export function pasteAttaches(text: string, files: number, paths: readonly string[]): boolean {
  if (files === 0) return false;
  return text.trim() === "" || paths.some((p) => p !== "");
}

/**
 * The new files that are not already there (工单 #143): the same file picked, pasted or dropped twice goes once. The
 * same is its name, type and size (a paste gives the same picture a new time each time).
 */
export function freshFiles<T extends PendingFile>(have: readonly T[], add: readonly T[]): T[] {
  const key = (f: T) => `${f.name}\n${f.type}\n${f.size}`;
  const seen = new Set(have.map(key));
  return add.filter((f) => !seen.has(key(f)) && (seen.add(key(f)), true));
}
