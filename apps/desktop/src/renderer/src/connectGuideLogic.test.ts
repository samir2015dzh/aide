import assert from "node:assert/strict";
import { test } from "node:test";
import type { AgentStatus, CustomAgent } from "@aide/proto/machine";
import { agentOf, detect, fixes, formOf, formProblem, installs, joinArgs, looksSecret, needs, seenOut, splitArgs, stateKey, terminalCommand } from "./connectGuideLogic.ts";

const status = (o: Partial<AgentStatus>) => ({ name: "x", available: false, loggedIn: false, checking: false, problem: 0, detail: "", ...o }) as AgentStatus;

test("the status says what the machine's check found", () => {
  assert.equal(detect(undefined, false, true), "offline");
  assert.equal(detect(undefined, true, false), "checking");
  assert.equal(detect(undefined, true, true), "missing");
  assert.equal(detect(status({ available: true, loggedIn: true }), true, true), "ready");
  assert.equal(detect(status({ available: true }), true, true), "signIn");
  assert.equal(detect(status({ available: true, loginUnconfirmed: true }), true, true), "unconfirmed"); // 工单 #112
  assert.equal(detect(status({ checking: true }), true, true), "checking");
  // Being checked again, it stays what it was found to be.
  assert.equal(detect(status({ available: true, loggedIn: true, checking: true }), true, true), "ready");
  assert.equal(detect(status({ problem: 1 }), true, true), "missing");
  assert.equal(detect(status({ problem: 2, detail: "Claude" }), true, true), "appOnly");
  assert.equal(detect(status({ problem: 4 }), true, true), "broken");
  assert.equal(detect(status({ problem: 6, detail: "boom" }), true, true), "broken");
});

// 工单 #175/#176: a check with no answer is said apart from a program that fails to start; a found agent whose version
// its check could not read says its version is unknown.
test("the status line says which went wrong, and an unknown version", () => {
  assert.equal(stateKey("broken", status({ problem: 4 })), "state.noAnswer");
  assert.equal(stateKey("broken", status({ problem: 6, detail: "boom" })), "state.broken");
  assert.equal(stateKey("ready", status({ available: true, loggedIn: true, version: "1.2.3" })), "state.readyVersion");
  assert.equal(stateKey("ready", status({ available: true, loggedIn: true })), "state.readyNoVersion");
  assert.equal(stateKey("signIn", status({ available: true }), "claude"), "state.signInClaude");
  assert.equal(stateKey("signIn", status({ available: true }), "codex"), "state.signIn");
});

test("what to try follows the line and what is wrong", () => {
  assert.deepEqual(fixes("claude", "appOnly"), ["claude.fixCodeTab", "fix.checkAgain"]);
  assert.deepEqual(fixes("claude", "missing"), ["claude.fixInstall", "claude.fixWhere", "fix.checkAgain"]);
  assert.deepEqual(fixes("deepseek", "missing"), ["deepseek.fixInstall", "deepseek.fixWhere", "fix.checkAgain"]);
  assert.deepEqual(fixes("codex", "signIn"), ["codex.fixSignIn", "fix.checkAgain"]);
  assert.deepEqual(fixes("deepseek", "broken"), ["fix.broken", "fix.checkAgain"]);
  // The DSH app found where Windows says, its dsh not in it: what to do about that, not how to install DSH.
  assert.deepEqual(fixes("deepseek", "appOnly"), ["deepseek.fixAppOnly", "fix.checkAgain"]);
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

import zhCommon from "../../locales/zh-CN/common.json" with { type: "json" };
import enCommon from "../../locales/en/common.json" with { type: "json" };
import zhConnect from "../../locales/zh-CN/connect.json" with { type: "json" };
import enConnect from "../../locales/en/connect.json" with { type: "json" };

test("#122 onboarding prompts include keepalive, 3-platform inbox watching, and real-message acceptance", () => {
  for (const phrase of ["保活", "信箱盯守", "常驻进程型", "webhook", "会话型", "message_list", "message_wake", "message_ack", "held=true", "真实消息", "显示在线"]) {
    assert.ok(zhCommon.personal.connectMessage.includes(phrase), `zh connectMessage missing ${phrase}`);
  }
  for (const phrase of ["Keepalive", "Inbox watching", "daemon/gateway", "Webhook-woken", "Session-based", "message_list", "message_wake", "message_ack", "held=true", "real message"]) {
    assert.ok(enCommon.personal.connectMessage.includes(phrase), `en connectMessage missing ${phrase}`);
  }
  assert.ok(zhConnect.assistants.connectedBody.includes("保活") && zhConnect.assistants.connectedBody.includes("信箱盯守"));
  assert.ok(enConnect.assistants.connectedBody.includes("keepalive") && enConnect.assistants.connectedBody.includes("inbox watching"));
});

test("needs: Antigravity's say how its server and sign-in stand now, the sign-in apart as its owner's (工单 #133)", () => {
  assert.deepEqual(needs("antigravity", "missing"), ["antigravity.needServerMissing", "antigravity.needSignInLater"]);
  assert.deepEqual(needs("antigravity", "appOnly"), ["antigravity.needServerMissing", "antigravity.needSignInLater"]);
  assert.deepEqual(needs("antigravity", "broken"), ["antigravity.needServerBroken", "antigravity.needSignInLater"]);
  assert.deepEqual(needs("antigravity", "signIn"), ["antigravity.needServerOk", "antigravity.needSignInMissing"]);
  assert.deepEqual(needs("antigravity", "unconfirmed"), ["antigravity.needServerOk", "antigravity.needSignInUnconfirmed"]);
  assert.deepEqual(needs("antigravity", "ready"), ["antigravity.needServerOk", "antigravity.needSignInOk"]);
  assert.deepEqual(needs("antigravity", "checking"), ["antigravity.needServer", "antigravity.needSignIn"]);
  assert.deepEqual(needs("codex", "missing"), ["codex.need1", "codex.need2"]);
  assert.deepEqual(needs("deepseek", "ready"), ["deepseek.need1", "deepseek.need2", "deepseek.need3"]);
  assert.deepEqual(installs, { antigravity: "antigravity-acp" });
});

// 工单 #180: signed out by what its last run said — not by its own check — it says so, a test run checks again first,
// and only then the sign-in; Antigravity's "before you start" says its app's sign-in may not carry over.
test("signed out by its last run: said so, and a test run comes first", () => {
  const seen = status({ available: true, signInSeen: { signedIn: false, why: "not logged in" } as AgentStatus["signInSeen"] });
  assert.equal(seenOut(seen), true);
  assert.equal(seenOut(status({ available: true })), false);
  assert.equal(seenOut(status({ available: true, loginUnconfirmed: true })), false);
  assert.equal(stateKey("signIn", seen, "antigravity"), "state.signInSeen");
  assert.deepEqual(fixes("antigravity", "signIn", true), ["fix.retest", "antigravity.fixSignIn", "fix.checkAgain"]);
  assert.deepEqual(fixes("antigravity", "signIn"), ["antigravity.fixSignIn", "fix.checkAgain"]);
  assert.deepEqual(needs("antigravity", "signIn", true), ["antigravity.needServerOk", "antigravity.needSignInSeen"]);
  assert.deepEqual(needs("antigravity", "unconfirmed"), ["antigravity.needServerOk", "antigravity.needSignInUnconfirmed"]);
});
