import { expect, test } from "vitest";
import { dispatchTargets, orphanPersonas } from "./dispatch";

const machine = (id: string, agents: object[], over: object = {}) => ({ machineId: id, machineName: id, online: true, local: false, status: { agents }, ...over }) as never;
const fresh = { seconds: BigInt(Math.floor(Date.now() / 1000) - 60), nanos: 0 };
const agent = (name: string, over: object = {}) => ({ name, available: true, loggedIn: true, hidden: false, models: [], checkedAt: fresh, ...over });

test("every detected agent is listed by machine with no contact (工单 #41); a contact is the same line, not a second", () => {
  const groups = dispatchTargets(
    [machine("work-pc", [agent("claude"), agent("deepseek"), agent("codex", { available: false })]), machine("box", [agent("claude", { loggedIn: false })], { online: true })],
    [{ personaId: "p1", name: "Reviewer", machineId: "work-pc", agent: "claude" } as never],
  );
  expect(groups.map((g) => [g.machine.machineId, g.targets.map((t) => [t.agent, t.persona?.name ?? "", t.ready, t.why ?? ""])])).toEqual([
      ["box", [["claude", "", false, "signedOut"]]],
      ["work-pc", [["claude", "Reviewer", true, ""], ["deepseek", "", true, ""]]],
    ]);
});

test("an offline machine's agents show, not ready, after the online ones; a contact whose agent is gone stays, said as such", () => {
  const groups = dispatchTargets([machine("m", [agent("claude")], { online: false }), machine("n", [])], [{ personaId: "p", name: "Old", machineId: "n", agent: "kimi" } as never]);
  expect(groups.map((g) => g.targets.map((t) => [t.agent, t.ready, t.why]))).toEqual([[["kimi", false, "missing"]], [["claude", false, "offline"]]]);
});

test("hidden or paused agents: hidden left out, paused not ready", () => {
  const groups = dispatchTargets([machine("m", [agent("claude", { hidden: true }), agent("codex", { pause: {} })])], []);
  expect(groups[0].targets.map((t) => [t.agent, t.why])).toEqual([["codex", "paused"]]);
  expect(orphanPersonas([machine("m", [])], [{ personaId: "x", machineId: "gone" } as never]).map((p) => p.personaId)).toEqual(["x"]);
});

test("checked too long ago (工单 #56): it can be sent, but it is not green and says why, in the machine page's words", () => {
  const old = { seconds: BigInt(Math.floor(Date.now() / 1000) - 40 * 60), nanos: 0 };
  const [x] = dispatchTargets([machine("m", [agent("claude", { checkedAt: old })])], [])[0].targets;
  expect([x.ready, x.why, x.state?.usable, x.state?.word]).toEqual([true, "unknown", "unverified", "状态未知"]);
  expect(x.state?.line).toContain("40 分钟前");
});
