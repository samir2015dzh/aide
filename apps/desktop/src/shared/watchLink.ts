// The app's link to aided on this computer (工单 #87): one watch (LocalService.Watch) each for a window and for the
// main process, kept up here. A watch that fails or ends is watched again; so is one that hears nothing, not even
// aided's beat (an empty message every 15 s with nothing new, internal/owner screens.go), for watchSilence: it hangs,
// and only a new watch would ever bring news. While it is down the screens say the link is being restored — what they
// last knew is not what is, and nothing is called offline for it — and each break and its end is kept for the
// settings' records (main/linklog.ts).

/** Silence past which a watch hangs: three of aided's beats. */
export const watchSilence = 45_000;

/** Why the link went down: the watch failed or ended (lost), or heard nothing for watchSilence (hung). */
export type LinkDown = "lost" | "hung";

/** One entry of the link's record, newest last: what broke or came back, when, and how long it was down. */
export type LinkEntry = {
  at: number;
  /** aided: this computer's Aide background service; relay: this computer's machine and the relay. */
  link: "aided" | "relay";
  kind: "down" | "back";
  /** For aided going down: lost or hung. */
  why?: LinkDown;
  /** What failed, in the transport's own words (not shown as such: the page says the suspected cause). */
  detail?: string;
  /** Back: how long it was down, in ms. */
  downFor?: number;
  /** Who noticed: the main process, or a window. */
  from: "app" | "window";
};

/** Whether a lost link's error says aided isn't there at all (stopped, restarting) rather than the link breaking. */
export function aidedGone(detail: string | undefined): boolean {
  return /ENOENT|ECONNREFUSED|EPIPE|ECONNRESET|connect E|socket hang up/i.test(detail ?? "");
}

/**
 * Keeps a watch up until signal aborts: open() each time, every message to message(); down() once when it fails, ends
 * or hangs, up() with the first message after; watched again retry ms after it went down.
 */
export async function keepWatching<M>(o: {
  open: (signal: AbortSignal) => AsyncIterable<M>;
  message: (m: M) => void;
  down: (why: LinkDown, detail: string) => void;
  up: () => void;
  signal: AbortSignal;
  silence?: number;
  retry?: number;
}): Promise<void> {
  const silence = o.silence ?? watchSilence;
  let isDown = false;
  while (!o.signal.aborted) {
    const attempt = new AbortController();
    const stop = () => attempt.abort();
    o.signal.addEventListener("abort", stop);
    let heard = Date.now();
    let hung = false;
    let detail = "";
    const dog = setInterval(() => {
      if (Date.now() - heard > silence) {
        hung = true;
        attempt.abort();
      }
    }, Math.min(5_000, Math.max(10, silence / 3)));
    try {
      for await (const m of o.open(attempt.signal)) {
        heard = Date.now();
        if (isDown) {
          isDown = false;
          o.up();
        }
        o.message(m);
      }
    } catch (e) {
      detail = e instanceof Error ? e.message : String(e);
    }
    clearInterval(dog);
    o.signal.removeEventListener("abort", stop);
    if (o.signal.aborted) return;
    if (!isDown) {
      isDown = true;
      o.down(hung ? "hung" : "lost", hung ? "" : detail);
    }
    await new Promise((r) => setTimeout(r, o.retry ?? 1000));
  }
}
