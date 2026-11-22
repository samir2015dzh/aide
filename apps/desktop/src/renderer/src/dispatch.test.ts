import { test } from "node:test";
import assert from "node:assert/strict";
import { dispatchTargets, orphanPersonas } from "./dispatch.ts";

const machine = (id: string, agents: object[], over: object = {}) => ({ machineId: id, machineName: id, online: true, local: false, status: { agents }, ...over }) as never;
const agent = (name: string, over: object = {}) => ({ name, available: true, loggedIn: true, hidden: false, ...over });

test("every detected agent is listed by machine with no contact (工单 #41); a contact is the same line, not a second", () => {
  const groups = dispatchTargets(
    [machine("work-pc", [agent("claude"), agent("deepseek"), agent("codex", { available: false })]), machine("box", [agent("claude", { loggedIn: false })], { online: true })],
    [{ personaId: "p1", name: "Reviewer", machineId: "work-pc", agent: "claude" } as never],
  );
  assert.deepEqual(
    groups.map((g) => [g.machine.machineId, g.targets.map((t) => [t.agent, t.persona?.name ?? "", t.ready, t.why ?? ""])]),
    [
      ["box", [["claude", "", false, "signedOut"]]],
      ["work-pc", [["claude", "Reviewer", true, ""], ["deepseek", "", true, ""]]],
    ],
  );
});

test("an offline machine's agents show, not ready, after the online ones; a contact whose agent is gone stays, said as such", () => {
  const groups = dispatchTargets([machine("m", [agent("claude")], { online: false }), machine("n", [])], [{ personaId: "p", name: "Old", machineId: "n", agent: "kimi" } as never]);
  assert.deepEqual(groups.map((g) => g.targets.map((t) => [t.agent, t.ready, t.why])), [[["kimi", false, "missing"]], [["claude", false, "offline"]]]);
});

test("hidden or paused agents: hidden left out, paused not ready", () => {
  const groups = dispatchTargets([machine("m", [agent("claude", { hidden: true }), agent("codex", { pause: {} })])], []);
  assert.deepEqual(groups[0].targets.map((t) => [t.agent, t.why]), [["codex", "paused"]]);
  assert.deepEqual(orphanPersonas([machine("m", [])], [{ personaId: "x", machineId: "gone" } as never]).map((p) => p.personaId), ["x"]);
});
