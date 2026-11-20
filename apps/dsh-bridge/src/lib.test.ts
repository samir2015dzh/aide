import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

// lib/ is committed: aided carries it (embed.go). It must be what the source builds to.
test("lib/ is the build of src/", () => {
  const here = join(import.meta.dirname, "..");
  const out = mkdtempSync(join(tmpdir(), "hmbridge-lib-"));
  execFileSync(process.execPath, [join(here, "node_modules", "typescript", "bin", "tsc"), "-p", join(here, "tsconfig.build.json"), "--outDir", out]);
  const built = readdirSync(out).sort();
  assert.deepEqual(readdirSync(join(here, "lib")).sort(), built);
  for (const f of built) assert.equal(readFileSync(join(here, "lib", f), "utf8"), readFileSync(join(out, f), "utf8"), `lib/${f} is stale: run pnpm build`);
});
