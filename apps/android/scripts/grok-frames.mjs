// Frames the bots' face engine draws (工单 #83), made ahead for the app's tests and page pictures, where no engine page
// can run (Robolectric has no browser engine): the same engine file (packages/ui/src/grok/grokMotionEngine.js) driven
// by the same page script (app/src/main/assets/grok/host.js), as the phone runs them.
//
//   node apps/android/scripts/grok-frames.mjs   (from the repository root; writes app/src/test/resources/grok-frames.json.gz)
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import zlib from "node:zlib";

const root = path.resolve(import.meta.dirname, "../../..");
globalThis.G = await import(path.join(root, "packages/ui/src/grok/grokMotionEngine.js"));
vm.runInThisContext(fs.readFileSync(path.join(root, "apps/android/app/src/main/assets/grok/host.js"), "utf8"));

// Every shape at rest (the editor's grid); the shapes a seed picks from in every state a bot shows (the roster).
const states = ["blocked", "running", "thinking", "streaming", "coding", "reading", "searching", "working", "waiting", "sleeping", "finished", "failed"];
const marks = [...G.GROK_ALL_SHAPES.map((s) => [s, "idle"]), ...G.GROK_DEFAULT_SHAPES.flatMap((s) => states.map((st) => [s, st]))];
const out = {};
for (const [shape, state] of marks) {
  const id = `${shape}|${state}`;
  aide.set(id, { shape, seed: id, state, size: 40, playing: true, follow: false, still: false });
  let f;
  for (let i = 0; i < 36; i++) f = aide.tick(1 / 30)[id] ?? f; // 1.2 s in
  aide.drop(id);
  out[id] = f;
}
fs.writeFileSync(path.join(root, "apps/android/app/src/test/resources/grok-frames.json.gz"), zlib.gzipSync(JSON.stringify(out)));
console.log(Object.keys(out).length, "frames");
