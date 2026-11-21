import { test } from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { planRustTarget, targetInstalled } from "./rust-target.mjs";

const triple = "x86_64-pc-windows-msvc";
const sysroot = path.join("tools", "rust");
const lib = path.join(sysroot, "lib", "rustlib", triple, "lib");

/** A machine: which programs answer, and which paths exist. */
function machine({ rustc = true, rustup = false, paths = [] }) {
  const ran = [];
  const run = (cmd, args) => {
    ran.push([cmd, ...args].join(" "));
    if (cmd === "rustc" && rustc) return `${sysroot}\n`;
    if (cmd === "rustup" && rustup) return "rustup 1.28.2";
    throw new Error(`${cmd}: not found`);
  };
  return { run, exists: (p) => paths.includes(p), ran };
}

test("a standalone Rust that ships the target needs no rustup", () => {
  const m = machine({ rustup: false, paths: [lib] });
  assert.equal(targetInstalled(triple, m), true);
  assert.deepEqual(planRustTarget(triple, m), { do: "ready" });
  assert.ok(!m.ran.some((r) => r.startsWith("rustup")), "rustup was not asked");
});

test("with rustup and the target there, nothing changes either", () => {
  assert.deepEqual(planRustTarget(triple, machine({ rustup: true, paths: [lib] })), { do: "ready" });
});

test("the target missing: rustup adds it, as before", () => {
  assert.deepEqual(planRustTarget(triple, machine({ rustup: true })), { do: "rustup" });
});

test("the target missing and no rustup: a clear error, not a program that fails to start", () => {
  const plan = planRustTarget(triple, machine({ rustup: false }));
  assert.equal(plan.do, "fail");
  assert.match(plan.message, /x86_64-pc-windows-msvc/);
  assert.match(plan.message, /rustup target add x86_64-pc-windows-msvc/);
});

test("no rustc at all: the target is not there", () => {
  const m = machine({ rustc: false, rustup: false, paths: [lib] });
  assert.equal(targetInstalled(triple, m), false);
  assert.equal(planRustTarget(triple, m).do, "fail");
});
