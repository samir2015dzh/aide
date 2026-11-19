import assert from "node:assert/strict";
import { test } from "node:test";
import type { ProviderSummary } from "@hermesh/her-protocol";
import { customProviders, formOf, limit, problem, saveParams, toggle } from "./customProviderForm.ts";

// A provider of one's own as the form holds it, and what providers/save is sent.

const magpie: ProviderSummary = {
  id: "magpie",
  name: "Magpie",
  auth: "api_key",
  credential: "stored",
  custom: { base_url: "https://gw.example:3443/v1", api: "openai-chat", models: [{ id: "gpt-5", context_window: 400000 }, { id: "glm-5" }] },
};

test("a provider's form holds what models.toml has, and no key", () => {
  assert.deepEqual(formOf(magpie), {
    name: "Magpie",
    baseUrl: "https://gw.example:3443/v1",
    api: "openai-chat",
    key: "",
    models: [
      { id: "gpt-5", context: "400000", output: "" },
      { id: "glm-5", context: "", output: "" },
    ],
  });
  assert.deepEqual(formOf(), { name: "", baseUrl: "", api: "openai-chat", key: "", models: [] });
});

test("limits are whole numbers, k for thousands, empty for the defaults", () => {
  assert.equal(limit(""), undefined);
  assert.equal(limit(" 128k "), 128000);
  assert.equal(limit("200,000"), 200000);
  assert.ok(Number.isNaN(limit("0")));
  assert.ok(Number.isNaN(limit("1.5k")));
  assert.ok(Number.isNaN(limit("lots")));
});

test("models from the list are chosen and unchosen with their limits", () => {
  const rows = toggle([], { id: "a", context_window: 64000 });
  assert.deepEqual(rows, [{ id: "a", context: "64000", output: "" }]);
  assert.deepEqual(toggle(rows, { id: "a" }), []);
});

test("what is missing is said before saving", () => {
  const ok = { ...formOf(magpie), key: "sk-1" };
  assert.equal(problem(ok), null);
  assert.equal(problem({ ...ok, name: " " }), "name");
  assert.equal(problem({ ...ok, baseUrl: "gw.example/v1" }), "address");
  assert.equal(problem({ ...ok, models: [{ id: " ", context: "", output: "" }] }), "models");
  assert.equal(problem({ ...ok, models: [{ id: "m", context: "big", output: "" }] }), "limit");
});

test("a new provider is sent without an id, a change with it; a key only when typed", () => {
  const f = { ...formOf(), name: " Magpie ", baseUrl: "https://gw.example/v1 ", key: " sk-1 ", models: [{ id: "m", context: "32k", output: "" }, { id: "m", context: "", output: "" }, { id: "", context: "", output: "" }] };
  assert.deepEqual(saveParams(f), { name: "Magpie", base_url: "https://gw.example/v1", api: "openai-chat", api_key: "sk-1", models: [{ id: "m", context_window: 32000 }] });
  assert.deepEqual(saveParams({ ...formOf(magpie), api: "anthropic-messages" }, "magpie"), {
    id: "magpie",
    name: "Magpie",
    base_url: "https://gw.example:3443/v1",
    api: "anthropic-messages",
    models: [{ id: "gpt-5", context_window: 400000 }, { id: "glm-5" }],
  });
});

test("only the providers the person added are theirs, by name", () => {
  const builtin: ProviderSummary = { id: "deepseek", name: "DeepSeek", auth: "api_key", credential: "missing" };
  const other = { ...magpie, id: "a", name: "Aardvark" };
  assert.deepEqual(customProviders([builtin, magpie, other]).map((p) => p.id), ["a", "magpie"]);
});
