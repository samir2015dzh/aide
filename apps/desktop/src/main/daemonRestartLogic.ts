import type { DaemonRestartResult } from "../shared/bridge.ts";

export type DaemonRestartMsgKey =
  | "daemon.needsApproval"
  | "daemon.restartDev"
  | "daemon.restartFailed"
  | "daemon.restartTimedOut";

export type DaemonRestartDeps<TStatus = unknown> = {
  installed: boolean;
  needsApproval: () => boolean;
  restart: () => Promise<void>;
  probe: () => Promise<TStatus | null>;
  onRecovered?: (status: TStatus) => void;
  timeoutMs?: number;
  intervalMs?: number;
  t: (key: DaemonRestartMsgKey) => string;
};

export function errorText(err: unknown): string {
  if (err && typeof err === "object") {
    const e = err as { stderr?: unknown; stdout?: unknown; message?: unknown };
    const stderr = typeof e.stderr === "string" ? e.stderr.trim() : "";
    if (stderr) return stderr;
    const msg = typeof e.message === "string" ? e.message.trim() : "";
    if (msg) return msg;
  }
  return String(err ?? "").trim();
}

export async function runDaemonRestart<TStatus>(deps: DaemonRestartDeps<TStatus>): Promise<DaemonRestartResult> {
  const tr = deps.t;
  if (deps.needsApproval()) {
    return { ok: false, needsApproval: true, reason: tr("daemon.needsApproval"), canOpenLogs: true };
  }

  if (!deps.installed) {
    // Development / externally supervised daemon: probe first in case it is already back.
    const already = await deps.probe();
    if (already) {
      deps.onRecovered?.(already);
      return { ok: true };
    }
    return { ok: false, reason: tr("daemon.restartDev"), canOpenLogs: true };
  }

  try {
    await deps.restart();
  } catch (err) {
    const detail = errorText(err);
    return {
      ok: false,
      reason: detail || tr("daemon.restartFailed"),
      canOpenLogs: true,
    };
  }

  const deadline = Date.now() + (deps.timeoutMs ?? 12_000);
  const step = deps.intervalMs ?? 300;
  while (true) {
    const status = await deps.probe();
    if (status) {
      deps.onRecovered?.(status);
      return { ok: true };
    }
    if (Date.now() >= deadline) {
      return { ok: false, reason: tr("daemon.restartTimedOut"), canOpenLogs: true };
    }
    await new Promise((r) => setTimeout(r, step));
  }
}
