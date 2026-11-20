import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { dataDirName, dataDirTitle, kept } from "./legacy.ts";

// The data directory an installation from before the rename made, as the app decides it
// (internal/paths TestKept covers the daemon's side, which decides the same).

function root(): string {
  return fs.mkdtempSync(path.join(os.tmpdir(), "kept-"));
}

function write(file: string) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, "x");
}

test("an identity in the old directory is kept, though the new one was made with only the service's definition in it", () => {
  const r = root();
  write(path.join(r, dataDirTitle, "owner", "current"));
  write(path.join(r, "Aide", "service", "aided.xml"));
  assert.equal(kept(path.join(r, "Aide")), path.join(r, dataDirTitle));
});

test("a new computer uses the new directory", () => {
  const r = root();
  assert.equal(kept(path.join(r, "Aide")), path.join(r, "Aide"));
  write(path.join(r, "Aide", "service", "aided.xml"));
  assert.equal(kept(path.join(r, "Aide")), path.join(r, "Aide"));
});

test("the new directory wins once it holds an identity", () => {
  const r = root();
  write(path.join(r, dataDirTitle, "owner", "current"));
  write(path.join(r, "Aide", "machine", "keys", "machine.key"));
  assert.equal(kept(path.join(r, "Aide")), path.join(r, "Aide"));
});

test("a machine's identity, or an owner from before profiles, counts as one", () => {
  for (const f of [path.join("machine", "keys", "machine.key"), path.join("owner", "owner.db")]) {
    const r = root();
    write(path.join(r, dataDirName, f));
    write(path.join(r, "aide", "logs", "setup.log"));
    assert.equal(kept(path.join(r, "aide")), path.join(r, dataDirName), f);
  }
});

test("an old directory with no identity is used only while there is no new one", () => {
  const r = root();
  write(path.join(r, dataDirTitle, "logs", "aided.log"));
  assert.equal(kept(path.join(r, "Aide")), path.join(r, dataDirTitle));
  fs.mkdirSync(path.join(r, "Aide"));
  assert.equal(kept(path.join(r, "Aide")), path.join(r, "Aide"));
});
