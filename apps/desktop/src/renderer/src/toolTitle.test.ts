import assert from "node:assert/strict";
import { test } from "node:test";
import { toolTitle } from "./toolTitle.ts";

test("a tool call shows by the machine's title when it gives one", () => {
  assert.equal(toolTitle({ title: "src/app.ts · +2 −3", summary: '{"file_path":"src/app.ts","old_string":"a"}' }), "src/app.ts · +2 −3");
});

test("from a machine before titles: the description, else a file, a command or a search, even from JSON cut short", () => {
  assert.equal(toolTitle({ summary: '{"command":"cd repo\\nSelect-String x","description":"Extract the build steps"}' }), "Extract the build steps");
  assert.equal(toolTitle({ summary: '{"file_path":"D:/work/readme.md","offset":10}' }), "D:/work/readme.md");
  assert.equal(toolTitle({ summary: '{"command":"go test ./...\\necho done","timeout":12' }), "go test ./...");
  assert.equal(toolTitle({ summary: '{"pattern":"TODO","path":"internal"}' }), "TODO");
  // Cut mid-value at 500 characters: what is there.
  assert.equal(toolTitle({ summary: '{"description":"Check the release workflow for the Windows st' }), "Check the release workflow for the Windows st");
});

test("arguments that say none of it leave the title empty: the name alone shows, never the raw arguments", () => {
  assert.equal(toolTitle({ summary: '{"n":3,"flags":["a","b"]}' }), "");
});

test("a summary of its own (Aide's sessions) reads as it is, on one line", () => {
  assert.equal(toolTitle({ summary: "internal/app.go" }), "internal/app.go");
  assert.equal(toolTitle({ summary: "first\nsecond" }), "first");
});
