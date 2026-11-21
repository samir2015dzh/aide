// Whether her can be built for a target here, and what it takes (package-win.mjs).
//
// The script used to run `rustup target add <triple>` whatever the machine had, so a standalone Rust (one
// that ships the target and has no rustup) stopped there. Now: a Rust whose standard library for the
// target is installed needs nothing; one without it adds it with rustup when there is one; with neither,
// the build stops saying what is missing and how to get it, rather than failing to start a program.

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

/** Runs a program and returns what it printed; throws when it can't be started or fails. */
const defaultRun = (cmd, args, cwd) => execFileSync(cmd, args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] });

/** Whether rustc, as run in cwd (where a rust-toolchain file picks it), has its standard library for triple. */
export function targetInstalled(triple, { cwd, run = defaultRun, exists = fs.existsSync } = {}) {
  let sysroot;
  try {
    sysroot = String(run("rustc", ["--print", "sysroot"], cwd)).trim();
  } catch {
    return false;
  }
  return sysroot !== "" && exists(path.join(sysroot, "lib", "rustlib", triple, "lib"));
}

/** Whether a program answers --version here. */
export function hasCommand(cmd, { cwd, run = defaultRun } = {}) {
  try {
    run(cmd, ["--version"], cwd);
    return true;
  } catch {
    return false;
  }
}

/**
 * What building for triple takes: { do: "ready" } (installed), { do: "rustup" } (add it with rustup), or
 * { do: "fail", message } saying what is missing and how to get it.
 */
export function planRustTarget(triple, deps = {}) {
  if (targetInstalled(triple, deps)) return { do: "ready" };
  if (hasCommand("rustup", deps)) return { do: "rustup" };
  return {
    do: "fail",
    message:
      `Rust can't build for ${triple} here: its standard library is not installed, and there is no rustup to add it. ` +
      `Install rustup (https://rustup.rs) and run "rustup target add ${triple}", or use a Rust that ships ${triple}.`,
  };
}
