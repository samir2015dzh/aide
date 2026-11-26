// What this device keeps of conversations between starts (工单 #115), as plain
// functions: no module is imported, so that node --test loads this file as the
// other page logic.

/** A conversation's events as kept on disk: whole says they start at its start. */
export type KeptRecord = { whole: boolean; events: string[] };

/**
 * The record kept for a conversation: its last max events, whole only when
 * nothing before them was left out.
 */
export function keptRecord(whole: boolean, events: string[], max: number): string {
  const cut = events.length > max;
  return JSON.stringify({ whole: whole && !cut, events: cut ? events.slice(-max) : events });
}

/** What keptRecord wrote; the first format, a bare list, never says it is whole. */
export function readKeptRecord(raw: string | null): KeptRecord {
  if (!raw) return { whole: false, events: [] };
  try {
    const parsed = JSON.parse(raw) as unknown;
    const list = Array.isArray(parsed) ? parsed : parsed && typeof parsed === "object" ? (parsed as { events?: unknown }).events : undefined;
    if (!Array.isArray(list)) return { whole: false, events: [] };
    const whole = !Array.isArray(parsed) && (parsed as { whole?: unknown }).whole === true;
    return { whole, events: list.filter((x): x is string => typeof x === "string") };
  } catch {
    return { whole: false, events: [] };
  }
}

/**
 * Where a conversation's stream picks up: after the last event held when the
 * events held are the whole conversation, else from its start (the held ones
 * still show meanwhile; the stream's overlap with them is dropped by seq).
 */
export function resumeAfter(seqs: readonly bigint[], whole: boolean): bigint {
  if (!whole) return 0n;
  return seqs.reduce((m, s) => (s > m ? s : m), 0n);
}

/**
 * The tray panel, settings, update and about windows load the same bundle as
 * the main window (App.tsx routes them by hash): only the main window, with
 * no hash, sends the outbox and writes it, so that no two windows resend the
 * same messages or overwrite each other's newer queue.
 */
export function isMainWindowHash(hash: string): boolean {
  return hash === "" || hash === "#";
}
