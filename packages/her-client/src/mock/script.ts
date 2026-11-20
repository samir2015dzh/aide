import type { ErrorInfo } from "@hermesh/her-protocol";

// What a scripted task does, turn by turn: the mock server plays it as her
// would, event by event.

export interface ToolScript {
  name: string;
  args: Record<string, unknown>;
  /** The result's text (what the model would be sent). */
  output: string;
  error?: boolean;
  /** The result's details, for user interfaces. */
  details?: unknown;
  /** Printed while it runs, line by line. */
  progress?: string[];
  /** How long it runs (ms, before the speed factor). */
  ms?: number;
  /** A spawn_agent call: what the sub-agent it starts does. */
  agent?: TaskScript;
  /** A manage_routine create: the routine it makes. */
  routine?: { name: string; when: string; schedule: string; prompt: string };
}

export interface TurnScript {
  think?: string;
  say?: string;
  tools?: ToolScript[];
}

export interface TaskScript {
  turns: TurnScript[];
  /** The task fails after its turns, with this error. */
  fail?: ErrorInfo;
  /** Before its first turn the model request fails this many times and is retried, as her retries a rate limit. */
  retries?: { count: number; error: ErrorInfo };
}
