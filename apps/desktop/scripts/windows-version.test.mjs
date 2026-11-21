import { test } from "node:test";
import assert from "node:assert/strict";
import { windowsVersionProblem } from "./windows-version.mjs";

test("a Windows build's version is above 0.0.0, as Squirrel's launcher needs", () => {
  for (const v of ["0.1.0", "0.2.3", "1.0.0", "0.0.1", "0.0.1-local", "0.2.3-rc.1", "0.1.0+abc123"]) {
    assert.equal(windowsVersionProblem(v), "", v);
  }
  // A pre-release of 0.0.0 sorts below it, 0.0.0 itself and with build metadata equal it: the launcher starts none.
  for (const v of ["0.0.0", "0.0.0-local", "0.0.0-dev", "0.0.0+local"]) {
    assert.match(windowsVersionProblem(v), /not above 0\.0\.0.*0\.0\.1-local/, v);
  }
  for (const v of ["", undefined, "v0.2.3", "0.2", "local", "0.02.3"]) {
    assert.match(windowsVersionProblem(v), /not a semantic version/, String(v));
  }
});
