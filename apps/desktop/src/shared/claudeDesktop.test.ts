import assert from "node:assert/strict";
import { test } from "node:test";
import { claudeResumeURL, openableOutside } from "./claudeDesktop.ts";

test("claudeResumeURL: a Claude Code session id opens in the Claude desktop app; anything else does not (工单 #131)", () => {
  assert.equal(claudeResumeURL("0F8FAD5B-D9CB-469F-A165-70867728950E"), "claude://resume?session=0f8fad5b-d9cb-469f-a165-70867728950e");
  for (const ref of ["", "local_0f8fad5b-d9cb-469f-a165-70867728950e", "0f8fad5b-d9cb-469f-a165-70867728950e&x=1", "../x"]) {
    assert.equal(claudeResumeURL(ref), "", ref);
  }
});

test("openableOutside: web pages, the Claude app, and its session links only", () => {
  assert.equal(openableOutside("https://example.com/"), true);
  assert.equal(openableOutside("claude://"), true);
  assert.equal(openableOutside("claude://resume?session=0f8fad5b-d9cb-469f-a165-70867728950e"), true);
  for (const u of ["http://example.com/", "claude://resume?session=x", "claude://resume?session=0f8fad5b-d9cb-469f-a165-70867728950e&cwd=/", "file:///etc/passwd", 42]) {
    assert.equal(openableOutside(u), false, String(u));
  }
});
