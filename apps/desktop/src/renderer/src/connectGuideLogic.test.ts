import assert from "node:assert/strict";
import { test } from "node:test";
import type { AgentStatus, CustomAgent } from "@aide/proto/machine";
import { agentOf, detect, fixes, formOf, formProblem, joinArgs, looksSecret, splitArgs, terminalCommand } from "./connectGuideLogic.ts";

const status = (o: Partial<AgentStatus>) => ({ name: "x", available: false, loggedIn: false, checking: false, problem: 0, detail: "", ...o }) as AgentStatus;

test("the status says what the machine's check found", () => {
  assert.equal(detect(undefined, false, true), "offline");
  assert.equal(detect(undefined, true, false), "checking");
  assert.equal(detect(undefined, true, true), "missing");
  assert.equal(detect(status({ available: true, loggedIn: true }), true, true), "ready");
  assert.equal(detect(status({ available: true }), true, true), "signIn");
  assert.equal(detect(status({ checking: true }), true, true), "checking");
  // Being checked again, it stays what it was found to be.
  assert.equal(detect(status({ available: true, loggedIn: true, checking: true }), true, true), "ready");
  assert.equal(detect(status({ problem: 1 }), true, true), "missing");
  assert.equal(detect(status({ problem: 2, detail: "Claude" }), true, true), "appOnly");
  assert.equal(detect(status({ problem: 4 }), true, true), "broken");
  assert.equal(detect(status({ problem: 6, detail: "boom" }), true, true), "broken");
});

test("what to try follows the line and what is wrong", () => {
  assert.deepEqual(fixes("claude", "appOnly"), ["claude.fixCodeTab", "fix.checkAgain"]);
  assert.deepEqual(fixes("claude", "missing"), ["claude.fixInstall", "claude.fixWhere", "fix.checkAgain"]);
  assert.deepEqual(fixes("deepseek", "missing"), ["deepseek.fixInstall", "deepseek.fixWhere", "fix.checkAgain"]);
  assert.deepEqual(fixes("codex", "signIn"), ["codex.fixSignIn", "fix.checkAgain"]);
  assert.deepEqual(fixes("deepseek", "broken"), ["fix.broken", "fix.checkAgain"]);
  assert.deepEqual(fixes("custom", "missing"), ["fix.customMissing", "fix.checkAgain"]);
  assert.deepEqual(fixes("codex", "ready"), []);
});

test("a program is run in a terminal by its path, quoted", () => {
  assert.equal(terminalCommand("C:\\Users\\me\\AppData\\Roaming\\Claude\\claude-code\\2.1.300\\claude.exe", "", true), '& "C:\\Users\\me\\AppData\\Roaming\\Claude\\claude-code\\2.1.300\\claude.exe"');
  assert.equal(terminalCommand("/usr/local/bin/codex", "login", false), '"/usr/local/bin/codex" login');
});

test("arguments split at spaces, quotes keep one together, backslashes stay", () => {
  assert.deepEqual(splitArgs("-y @zed-industries/claude-code-acp"), ["-y", "@zed-industries/claude-code-acp"]);
  assert.deepEqual(splitArgs('  --profile acp  --dir "C:\\My Agents\\x"  '), ["--profile", "acp", "--dir", "C:\\My Agents\\x"]);
  assert.deepEqual(splitArgs(`--name 'a "b"' ""`), ["--name", 'a "b"', ""]);
  assert.deepEqual(splitArgs(""), []);
  for (const args of [["-y", "@zed-industries/claude-code-acp"], ["--dir", "C:\\My Agents\\x", ""], ['a "b"']]) {
    assert.deepEqual(splitArgs(joinArgs(args)), args);
  }
});

test("secret-looking variables are secret, as on the machine", () => {
  for (const n of ["ANTHROPIC_API_KEY", "GITHUB_TOKEN", "DB_PASSWORD", "client_secret"]) assert.ok(looksSecret(n), n);
  for (const n of ["FAKE_ACP_STATE", "HTTPS_PROXY", "NODE_OPTIONS"]) assert.ok(!looksSecret(n), n);
});

test("the form says what is missing, and gives the machine the agent", () => {
  const builtIn = ["claude", "codex", "deepseek"];
  const f = { name: "claude-acp", command: "npx", args: "-y @zed-industries/claude-code-acp", env: [{ name: "ANTHROPIC_API_KEY", value: "sk-ant-1", secret: false }], dir: "" };
  assert.equal(formProblem(f, builtIn), null);
  assert.equal(formProblem({ ...f, name: "Claude ACP" }, builtIn), "name");
  assert.equal(formProblem({ ...f, name: "deepseek" }, builtIn), "builtIn");
  assert.equal(formProblem({ ...f, command: " " }, builtIn), "command");
  assert.equal(formProblem({ ...f, env: [{ name: "1X", value: "v", secret: false }] }, builtIn), "envName");
  assert.equal(formProblem({ ...f, env: [{ name: "A", value: "1", secret: false }, { name: "a", value: "2", secret: false }] }, builtIn), "envTwice");
  assert.equal(formProblem({ ...f, env: [{ name: "ANTHROPIC_API_KEY", value: "", secret: false }] }, builtIn), "envValue");
  // An empty row is no variable.
  assert.equal(formProblem({ ...f, env: [...f.env, { name: "", value: "", secret: false }] }, builtIn), null);
  assert.deepEqual(agentOf({ ...f, env: [...f.env, { name: " MODE ", value: "fast", secret: false }, { name: "", value: "", secret: false }] }), {
    name: "claude-acp",
    command: "npx",
    args: ["-y", "@zed-industries/claude-code-acp"],
    env: [{ name: "ANTHROPIC_API_KEY", value: "sk-ant-1", secret: true }, { name: "MODE", value: "fast", secret: false }],
    dir: "",
  });
});

test("an agent the machine has comes back with its secrets saved, not shown", () => {
  const saved = { name: "claude-acp", command: "npx", args: ["-y", "@zed-industries/claude-code-acp"], env: [{ name: "ANTHROPIC_API_KEY", value: "", secret: true }], dir: "" } as unknown as CustomAgent;
  const f = formOf(saved);
  assert.equal(f.args, "-y @zed-industries/claude-code-acp");
  assert.deepEqual(f.env, [{ name: "ANTHROPIC_API_KEY", value: "", secret: true, saved: true }]);
  assert.equal(formProblem(f, []), null); // left empty, the machine keeps it
  assert.deepEqual(agentOf(f).env, [{ name: "ANTHROPIC_API_KEY", value: "", secret: true }]);
});
