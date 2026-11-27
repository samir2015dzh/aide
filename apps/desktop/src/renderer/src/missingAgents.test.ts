import { test } from "node:test";
import assert from "node:assert/strict";
import { codingAgentIds } from "../../../../../packages/ui/src/agents.ts";
import { lines } from "./connectGuideLogic.ts";
import { missingAction, missingAgents } from "./missingAgents.ts";

// 工单 #180 A: every coding agent Aide supports, when not installed, does something when clicked.
test("every not-installed agent has a guide line or says in place what to do", () => {
  for (const id of codingAgentIds) assert.ok(missingAction(id, lines), `${id} would be a dead row`);
});

test("the in-place ones name a command, an https page and a registry id", () => {
  for (const [id, a] of Object.entries(missingAgents)) {
    assert.ok(a.command && !/\s/.test(a.command), id);
    assert.match(a.site, /^https:\/\//, id);
    assert.ok(a.registryId, id);
  }
});

test("the Connect guide's lines open the guide", () => {
  for (const id of ["claude", "codex", "antigravity", "deepseek"]) assert.deepEqual(missingAction(id, lines), { guide: id });
});
