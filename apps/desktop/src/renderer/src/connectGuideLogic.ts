import type { AgentProblem, AgentStatus, CustomAgent } from "@aide/proto/machine";

// The Connect guide's logic (ConnectGuide.tsx): a separate line for each
// coding agent the owner connects (DeepSeek Harness, Claude, Codex) and one
// for any other ACP agent (a custom agent kept on the machine); what the
// machine's last check of the line's agent says, and what to try when it is
// not there; the custom agent's form.

export type Line = "deepseek" | "claude" | "codex" | "antigravity" | "custom";

/** The lines in the order the guide shows them. */
export const lines: readonly Line[] = ["deepseek", "claude", "codex", "antigravity", "custom"];

// AgentProblem's values (its enum is not loaded by the tests).
const notFound = 1 as AgentProblem;
const appWithoutCli = 2 as AgentProblem;

/** What the machine's checks say of an agent, for the guide's status line. */
export type Detected =
  /** The machine can't be asked: it is offline. */
  | "offline"
  /** Not known yet: the machine is checking, or has not said. */
  | "checking"
  /** Found and signed in. */
  | "ready"
  /** Found; whether it is signed in shows only in a session (an ACP agent's, 工单 #112). */
  | "unconfirmed"
  /** Found, not signed in. */
  | "signIn"
  /** Its desktop app is there, not the command Aide runs. */
  | "appOnly"
  /** Found, but it can't run (AgentStatus.detail says why). */
  | "broken"
  /** Not found. */
  | "missing";

/**
 * What an agent's status says; known: the machine has said which agents it
 * has (an agent it does not list is not there).
 */
export function detect(a: AgentStatus | undefined, online: boolean, known: boolean): Detected {
  if (!online) return "offline";
  if (!a) return known ? "missing" : "checking";
  if (a.available) return a.loggedIn ? "ready" : a.loginUnconfirmed ? "unconfirmed" : "signIn";
  if (a.checking) return "checking";
  if (a.problem === appWithoutCli) return "appOnly";
  if (a.problem === notFound || a.problem === (0 as AgentProblem)) return "missing";
  return "broken";
}

/**
 * What to try, as keys of the connect texts, for an agent that is not ready:
 * the likely causes first, each something the person can do. A broken
 * agent's own reason comes first, from the machine (whyBroken).
 */
export function fixes(line: Line, d: Detected): string[] {
  switch (d) {
    case "missing":
      return line === "custom" ? ["fix.customMissing", "fix.checkAgain"] : [`${line}.fixInstall`, `${line}.fixWhere`, "fix.checkAgain"];
    case "appOnly":
      return line === "claude" ? ["claude.fixCodeTab", "fix.checkAgain"] : line === "deepseek" ? ["deepseek.fixAppOnly", "fix.checkAgain"] : [`${line}.fixInstall`, "fix.checkAgain"];
    case "signIn":
      return [`${line}.fixSignIn`, "fix.checkAgain"];
    case "broken":
      return ["fix.broken", "fix.checkAgain"];
    case "offline":
      return ["fix.offline"];
    default:
      return [];
  }
}

/** Claude Code's answer says it is not signed in ("Not logged in · Please run /login"): a failure, whatever its exit said. */
export function notLoggedIn(reply: string): boolean {
  return /not logged in|please run \/login|invalid api key/i.test(reply);
}

/** The command that runs a program at path in a terminal: PowerShell's call operator on Windows. */
export function terminalCommand(path: string, args: string, windows: boolean): string {
  const quoted = `"${path}"`;
  return [windows ? `& ${quoted}` : quoted, args].filter(Boolean).join(" ");
}

// The custom agent's form.

/** A variable as typed; saved names a secret the machine keeps, whose value is not shown. */
export type EnvRow = { name: string; value: string; secret: boolean; saved?: boolean };

export type CustomForm = { name: string; command: string; args: string; env: EnvRow[]; dir: string };

/** Named like a key, a token or a password: the machine keeps its value in its credential store (as aided's looksSecret). */
export const looksSecret = (name: string) => /KEY|TOKEN|SECRET|PASSW|CREDENTIAL|AUTH|COOKIE|PRIVATE/i.test(name);

/** The built-in agents' names, which a custom agent can't take (as aided's AgentNames). */
export function nameProblem(name: string, builtIn: readonly string[]): "name" | "builtIn" | null {
  if (!/^[a-z][a-z0-9-]{0,31}$/.test(name)) return "name";
  return builtIn.includes(name) || name === "fake" ? "builtIn" : null;
}

/**
 * The arguments as typed: separated by spaces, one with spaces in quotes
 * ("…" or '…'). Backslashes are kept as they are (Windows paths).
 */
export function splitArgs(s: string): string[] {
  const out: string[] = [];
  let cur = "";
  let quote = "";
  let has = false;
  for (const c of s) {
    if (quote) {
      if (c === quote) quote = "";
      else cur += c;
    } else if (c === '"' || c === "'") {
      quote = c;
      has = true;
    } else if (/\s/.test(c)) {
      if (has || cur) out.push(cur);
      cur = "";
      has = false;
    } else {
      cur += c;
      has = true;
    }
  }
  if (has || cur) out.push(cur);
  return out;
}

/** The arguments as the form shows them: one with a space or nothing in quotes. */
export function joinArgs(args: readonly string[]): string {
  return args.map((a) => (a === "" || /\s/.test(a) ? (a.includes('"') ? `'${a}'` : `"${a}"`) : a)).join(" ");
}

/** The form for a new agent, or for changing one the machine has (its secrets kept unless retyped). */
export function formOf(a?: CustomAgent): CustomForm {
  return {
    name: a?.name ?? "",
    command: a?.command ?? "",
    args: joinArgs(a?.args ?? []),
    env: (a?.env ?? []).map((e) => ({ name: e.name, value: e.value, secret: e.secret, saved: e.secret })),
    dir: a?.dir ?? "",
  };
}

/** What is wrong with the form, as a key of connect.custom.problem, or null. */
export function formProblem(f: CustomForm, builtIn: readonly string[]): string | null {
  const n = nameProblem(f.name.trim(), builtIn);
  if (n) return n;
  if (!f.command.trim()) return "command";
  const rows = f.env.filter((e) => e.name.trim() || e.value);
  const seen = new Set<string>();
  for (const e of rows) {
    const name = e.name.trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) return "envName";
    if (seen.has(name.toUpperCase())) return "envTwice";
    seen.add(name.toUpperCase());
    if ((e.secret || looksSecret(name)) && !e.value && !e.saved) return "envValue";
  }
  return null;
}

/** The agent the form describes, as the machine takes it (a saved secret left empty is kept). */
export function agentOf(f: CustomForm): { name: string; command: string; args: string[]; env: { name: string; value: string; secret: boolean }[]; dir: string } {
  return {
    name: f.name.trim(),
    command: f.command.trim(),
    args: splitArgs(f.args),
    env: f.env.filter((e) => e.name.trim() || e.value).map((e) => ({ name: e.name.trim(), value: e.value, secret: e.secret || looksSecret(e.name.trim()) })),
    dir: f.dir.trim(),
  };
}
