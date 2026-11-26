import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

// 工单 #136 P07: a text that says where to go in Aide's settings ("设置 › 访问") names a page the settings window has.
// Directions into another app's settings (macOS's 系统设置, Windows' 设置 › 应用, Claude Desktop's, ChatGPT's) are not
// Aide's and are listed apart.

const locales = path.join(import.meta.dirname, "..", "..", "locales");
const others: Record<string, RegExp> = {
  "zh-CN": /系统设置 › |设置 › 应用 ›|Claude Desktop 里打开 设置 › |ChatGPT 里打开 设置 › /,
  en: /System Settings › |Settings › Apps ›|Settings › Login Items|In Claude Desktop, open Settings › |In ChatGPT, open Settings › /i,
};

function strings(o: unknown, out: string[] = []): string[] {
  if (typeof o === "string") out.push(o);
  else if (o && typeof o === "object") for (const v of Object.values(o)) strings(v, out);
  return out;
}

for (const lang of ["zh-CN", "en"]) {
  test(`every "Settings › page" in ${lang} names one of Aide's settings pages`, () => {
    const pages = new Set(Object.values(JSON.parse(fs.readFileSync(path.join(locales, lang, "settings.json"), "utf8")).pages as Record<string, string>));
    const word = lang === "zh-CN" ? "设置 › " : "Settings › ";
    const wrong: string[] = [];
    for (const f of fs.readdirSync(path.join(locales, lang))) {
      if (f === "releases.json") continue; // history, as it was said then
      for (const s of strings(JSON.parse(fs.readFileSync(path.join(locales, lang, f), "utf8")))) {
        for (let i = s.indexOf(word); i >= 0; i = s.indexOf(word, i + 1)) {
          if (others[lang].test(s.slice(Math.max(0, i - 24), i + word.length + 16))) continue;
          const rest = s.slice(i + word.length);
          if (![...pages].some((p) => rest.startsWith(p))) wrong.push(`${f}: …${s.slice(i, i + 24)}…`);
        }
      }
    }
    assert.deepEqual(wrong, []);
  });
}
