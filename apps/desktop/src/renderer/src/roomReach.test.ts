import assert from "node:assert/strict";
import { test } from "node:test";
import { reachProblem, unreachableKey } from "./roomReach.ts";

// 工单 #159 第 1 条: a member checked and reachable says nothing; one that may not take its turn says why, a personal
// agent nothing takes letters for with when it was last there.
test("why a member may not take its turn", () => {
  assert.equal(reachProblem({ problem: 0 }), "");
  assert.equal(reachProblem({ problem: 3 }), "noInbox");
  assert.equal(reachProblem({ problem: 4 }), "notTaking");
  assert.equal(reachProblem({ problem: 4, lastSeen: { seconds: 1n, nanos: 0 } }), "notTakingSeen");
  assert.equal(reachProblem({ problem: 6 }), "machineOffline");
  assert.equal(reachProblem({ problem: 10 }), "agentUnconfirmed");
  assert.equal(reachProblem({ problem: 99 }), "unknown");
});

test("the set the owner confirms is the members that may not take their turn, in any order", () => {
  const a = { botKey: "c:c_muse", problem: 4 };
  const b = { botKey: "m:m_vps1/claude", problem: 10 };
  const ok = { botKey: "c:c_ok", problem: 0 };
  assert.equal(unreachableKey([ok]), "");
  assert.equal(unreachableKey([b, ok, a]), unreachableKey([a, b]));
  assert.notEqual(unreachableKey([a]), unreachableKey([a, b]));
});
