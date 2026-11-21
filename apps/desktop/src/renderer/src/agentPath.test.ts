import { test } from "node:test";
import assert from "node:assert/strict";
import { shortPath } from "./agentPath.ts";

test("an agent's path shows the home folder as ~", () => {
  assert.equal(shortPath("/Users/someone/.local/bin/codex"), "~/.local/bin/codex");
  assert.equal(shortPath("/home/someone/.npm-global/bin/copilot"), "~/.npm-global/bin/copilot");
  assert.equal(shortPath("C:\\Users\\someone\\AppData\\Roaming\\npm\\codex.cmd"), "~\\AppData\\Roaming\\npm\\codex.cmd");
  assert.equal(shortPath("c:\\users\\someone\\bin\\codex.exe"), "~\\bin\\codex.exe");
  assert.equal(shortPath("C:\\Program Files\\Codex\\codex.exe"), "C:\\Program Files\\Codex\\codex.exe");
  assert.equal(shortPath("/opt/homebrew/bin/codex"), "/opt/homebrew/bin/codex");
  assert.equal(shortPath(""), "");
});
