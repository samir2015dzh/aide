// The line a tool call read from DSH's or Claude Desktop's records shows by. The machine works it out
// from the whole arguments (ToolCall.title); a machine from before that sends only the raw arguments,
// cut at 500 characters (often mid-JSON), and this does what it can from them: the description, a file,
// a command or what is searched for, read even from JSON cut short. "" when they say none of it: the row
// then shows the tool's name alone, the arguments folded under it, never spread across it.

const field = (raw: string, keys: string[]): string => {
  for (const k of keys) {
    // A string value, whole or cut short: "key":"value… (escapes kept as they read).
    const m = new RegExp(`"${k}"\\s*:\\s*"((?:[^"\\\\]|\\\\.)*)`).exec(raw);
    if (m && m[1].trim()) return unescape(m[1]).trim();
  }
  return "";
};

const unescape = (s: string) => s.replace(/\\n/g, "\n").replace(/\\t/g, " ").replace(/\\"/g, '"').replace(/\\\\/g, "\\");

const firstLine = (s: string) => s.split("\n")[0].trim();

const clip = (s: string) => (s.length > 120 ? s.slice(0, 119) + "…" : s);

export function toolTitle(call: { title?: string; summary: string }): string {
  if (call.title) return call.title;
  const raw = call.summary.trim();
  if (!raw.startsWith("{")) return clip(firstLine(raw)); // a summary of its own (Aide's sessions): as it reads
  const d = field(raw, ["description"]);
  if (d) return clip(firstLine(d));
  const pattern = field(raw, ["pattern", "query"]);
  if (pattern) return clip(pattern);
  const path = field(raw, ["file_path", "filePath", "path", "notebook_path"]);
  if (path) return clip(path);
  const cmd = field(raw, ["command", "cmd"]);
  if (cmd) return clip(firstLine(cmd));
  const url = field(raw, ["url"]);
  return url ? clip(url) : "";
}
