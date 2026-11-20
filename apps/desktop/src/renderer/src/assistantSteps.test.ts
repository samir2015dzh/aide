import assert from "node:assert/strict";
import fs from "node:fs";
import { test } from "node:test";
import { assistantSteps, byMCP, mcpState } from "./assistantSteps.ts";

const locale = (lang: string) => JSON.parse(fs.readFileSync(new URL(`../../locales/${lang}/connect.json`, import.meta.url), "utf8")).assistants;

test("the MCP address: there, not connected yet, a server older than it, or one with MCP off", () => {
  assert.equal(mcpState({ relayConnected: true, mcpUrl: "https://relay.example.com/mcp/k" }), "ready");
  assert.equal(mcpState({ relayConnected: false, mcpUrl: "" }), "offline");
  assert.equal(mcpState(null), "offline");
  assert.equal(mcpState({ relayConnected: true, mcpUrl: "", relayOlderThanMcp: true }), "older");
  assert.equal(mcpState({ relayConnected: true, mcpUrl: "" }), "off");
});

test("an MCP assistant without an address shows no step that pastes one, and says why", () => {
  assert.deepEqual(assistantSteps("chatgpt", "older"), { steps: [], copy: false, note: "mcpOlder" });
  assert.deepEqual(assistantSteps("chatgpt", "off"), { steps: [], copy: false, note: "mcpOff" });
  assert.deepEqual(assistantSteps("claude", "offline"), { steps: [], copy: false, note: "mcpOffline" });
  assert.equal(assistantSteps("claude", "ready").copy, true);
  // The CLI's assistants send a message, which is always there.
  assert.deepEqual(assistantSteps("grok", "off"), { steps: ["cli1", "cli2", "cli3"], copy: true, note: null });
});

test("each MCP assistant has steps of its own, which name its own app's menus and no other's", () => {
  for (const lang of ["zh-CN", "en"]) {
    const a = locale(lang);
    for (const kind of byMCP) {
      const [first] = assistantSteps(kind, "ready").steps;
      const text: string = a.steps[kind];
      assert.ok(text, `${lang}: assistants.steps.${kind}`);
      assert.ok(first === `steps.${kind}`);
      const others = [...byMCP].filter((k) => k !== kind);
      for (const o of others) assert.ok(!text.includes(o === "claude" ? "Claude" : "ChatGPT"), `${lang}: ${kind}'s steps name ${o}`);
    }
    for (const k of ["mcpOlder", "mcpOff"]) assert.ok(a[k] && !a[k].includes("HTTPS"), `${lang}: ${k} isn't put down to HTTPS`);
    assert.ok(/更新|[Uu]pdate/.test(a.mcpOlder), `${lang}: an older server is told to update`);
  }
});
