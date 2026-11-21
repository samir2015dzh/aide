import assert from "node:assert/strict";
import { test } from "node:test";
import { groupByMachine, machineTag, manyMachines, showGroups } from "./machineScope.ts";

const here = { machineId: "m1", machineName: "this-pc", local: true };
const work = { machineId: "m2", machineName: "work-pc", local: false };
const box = { machineId: "m3", machineName: "box", local: false };

test("with more than one machine, sessions are told apart by machine; with one, nothing shows", () => {
  assert.equal(manyMachines([here]), false);
  assert.equal(manyMachines([]), false);
  assert.equal(manyMachines([here, work]), true);
  assert.equal(showGroups([here]), false);
  assert.equal(showGroups([here, work]), true);
});

test("sessions in groups by machine: this computer's first, then by name, each in the order given", () => {
  const sessions = [
    { id: "a", m: "m2" },
    { id: "b", m: "m1" },
    { id: "c", m: "m3" },
    { id: "d", m: "m2" },
    { id: "e", m: "m1" },
    { id: "f", m: "gone" },
  ];
  const groups = groupByMachine(sessions, (s) => s.m, [work, box, here]);
  assert.deepEqual(
    groups.map((g) => [g.machineId, g.name, g.local, g.items.map((s) => s.id).join("")]),
    [
      ["m1", "this-pc", true, "be"],
      ["m3", "box", false, "c"],
      ["gone", "gone", false, "f"],
      ["m2", "work-pc", false, "ad"],
    ],
  );
  assert.deepEqual(groupByMachine([], (s: { m: string }) => s.m, [here, work]), []);
});

test("a session's machine tag: its name, and whether it is this computer", () => {
  assert.deepEqual(machineTag([here, work], "m2"), { name: "work-pc", local: false });
  assert.deepEqual(machineTag([here, work], "m1"), { name: "this-pc", local: true });
  assert.deepEqual(machineTag([here], "m9"), { name: "m9", local: false });
});
