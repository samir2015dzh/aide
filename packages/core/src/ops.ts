import { ExecOutcome, type ExecRecord } from "@aide/proto/machine";
import { dateOf } from "./sessions";

/** How a command of the ops log (docs/machine-ops.md) ended, as its row says it. */
export type OpsResult =
  | { kind: "ok" }
  | { kind: "exit"; code: number }
  | { kind: "timeout" }
  | { kind: "refused" }
  | { kind: "failed" };

export function opsResult(r: ExecRecord): OpsResult {
  switch (r.outcome) {
    case ExecOutcome.EXITED:
      return r.exitCode === 0 ? { kind: "ok" } : { kind: "exit", code: r.exitCode };
    case ExecOutcome.TIMED_OUT:
      return { kind: "timeout" };
    case ExecOutcome.REFUSED:
      return { kind: "refused" };
    default:
      return { kind: "failed" };
  }
}

/** How long it ran, in milliseconds (0 when the machine didn't say). */
export function opsMillis(r: ExecRecord): number {
  const from = dateOf(r.startedAt);
  const to = dateOf(r.endedAt);
  return from && to ? Math.max(0, to.getTime() - from.getTime()) : 0;
}

export type OpsCommandSummary = {
  /** Shell kind if a wrapper script was detected ("powershell" | "shell" | "cmd" | "plain"). */
  shell: "powershell" | "shell" | "cmd" | "plain";
  /** One-line human-readable summary of what the command does (≤ 72 chars). */
  preview: string;
  /** True when the raw command is multi-line or long and should fold into "View details" (#138 P14). */
  collapsible: boolean;
  /** Number of non-empty lines in the raw command. */
  lineCount: number;
};

const PS_WRAPPER = /^(?:powershell(?:\.exe)?|pwsh(?:\.exe)?)\b.*?(?:-c(?:ommand)?|-enc(?:odedcommand)?)\s+/i;
const SH_WRAPPER = /^(?:\/(?:usr\/)?bin\/)?(?:ba|z|da)?sh\s+-[a-z]*c\s+/i;
const CMD_WRAPPER = /^cmd(?:\.exe)?\s+\/[ck]\s+/i;

function stripOuterQuotes(s: string): string {
  const t = s.trim();
  if (t.length >= 2 && ((t.startsWith("\"") && t.endsWith("\"")) || (t.startsWith("'") && t.endsWith("'")))) {
    return t.slice(1, -1).trim();
  }
  return t;
}

/**
 * Produces a concise one-line summary for an ExecRecord command so multi-line PowerShell/shell
 * scripts do not flood the Execution Log screen (#138 P14).
 */
export function opsCommandSummary(command: string): OpsCommandSummary {
  const raw = command.replace(/\r\n/g, "\n").trim();
  const lines = raw.split("\n").map((l) => l.trim()).filter(Boolean);
  const lineCount = Math.max(1, lines.length);

  let shell: OpsCommandSummary["shell"] = "plain";
  let body = raw;
  if (PS_WRAPPER.test(raw) || /^\s*\$\w+\s*=|\b(?:Get-|Set-|New-|Remove-|Select-Object|Invoke-|Write-Host|ConvertTo-Json|Test-Path)\b/.test(raw)) {
    shell = "powershell";
    body = stripOuterQuotes(raw.replace(PS_WRAPPER, ""));
  } else if (SH_WRAPPER.test(raw) || raw.startsWith("#!/")) {
    shell = "shell";
    body = stripOuterQuotes(raw.replace(SH_WRAPPER, ""));
  } else if (CMD_WRAPPER.test(raw)) {
    shell = "cmd";
    body = stripOuterQuotes(raw.replace(CMD_WRAPPER, ""));
  }

  const bodyLines = body
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l && !l.startsWith("#") && !l.startsWith("set -"));
  const meaningful = bodyLines.filter(
    (l) => !/^(?:\$(?:ErrorActionPreference|ProgressPreference|OutputEncoding)\s*=|\[Console\]::OutputEncoding\s*=|chcp\s+\d+)/i.test(l),
  );
  const firstLine = (meaningful[0] ?? bodyLines[0] ?? lines[0] ?? raw).replace(/\s+/g, " ");
  const maxLen = 72;
  const preview = firstLine.length > maxLen ? `${firstLine.slice(0, maxLen - 1)}…` : firstLine;
  const collapsible = lineCount > 1 || raw.length > maxLen;
  return { shell, preview, collapsible, lineCount };
}
