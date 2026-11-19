import type { CustomModel, ProviderSummary, ProvidersSaveParams } from "@hermesh/her-protocol";

// A provider of one's own as the form holds it (CustomProvider.tsx): an OpenAI- or
// Anthropic-compatible service at an address, its key, and the models chosen from its list or
// named by hand, each with optional limits. her keeps it in models.toml and the key in its
// credential store (providers/save).

export type Api = "openai-chat" | "anthropic-messages";

/** A model as typed: its limits as text, empty for her's defaults. */
export type ModelRow = { id: string; context: string; output: string };

export type Form = { name: string; baseUrl: string; api: Api; key: string; models: ModelRow[] };

const text = (n: number | null | undefined) => (n ? String(n) : "");

/** The form for a new provider, or for changing `p` (its key is kept unless one is typed). */
export function formOf(p?: ProviderSummary | null): Form {
  const c = p?.custom;
  return {
    name: p?.name ?? "",
    baseUrl: c?.base_url ?? "",
    api: c?.api === "anthropic-messages" ? "anthropic-messages" : "openai-chat",
    key: "",
    models: (c?.models ?? []).map((m) => ({ id: m.id, context: text(m.context_window), output: text(m.max_output_tokens) })),
  };
}

/** A model of the service's list, as a row: with the limits the list told. */
export const rowOf = (m: CustomModel): ModelRow => ({ id: m.id, context: text(m.context_window), output: text(m.max_output_tokens) });

/** Adds `m` when it is not chosen, takes it out when it is. */
export function toggle(rows: ModelRow[], m: CustomModel): ModelRow[] {
  return rows.some((r) => r.id === m.id) ? rows.filter((r) => r.id !== m.id) : [...rows, rowOf(m)];
}

/** A limit as typed: empty for the default, a whole number (`128k` is 128000), else NaN. */
export function limit(typed: string): number | undefined {
  const t = typed.trim().toLowerCase().replace(/[,_\s]/g, "");
  if (!t) return undefined;
  const m = /^(\d+)(k?)$/.exec(t);
  const n = m ? Number(m[1]) * (m[2] ? 1000 : 1) : NaN;
  return n > 0 && n <= 0xffffffff ? n : NaN;
}

/** What is wrong with the form, as a key of the settings' `custom.problem`, or null. */
export function problem(f: Form): "name" | "address" | "models" | "limit" | null {
  if (!f.name.trim()) return "name";
  if (!/^https?:\/\/[^/\s]+/i.test(f.baseUrl.trim())) return "address";
  const rows = f.models.filter((m) => m.id.trim());
  if (!rows.length) return "models";
  if (rows.some((m) => Number.isNaN(limit(m.context)) || Number.isNaN(limit(m.output)))) return "limit";
  return null;
}

/** providers/save's params for the form: a new provider without `id`; a key only when one is typed. */
export function saveParams(f: Form, id?: string): ProvidersSaveParams {
  const seen = new Set<string>();
  const models: CustomModel[] = [];
  for (const m of f.models) {
    const mid = m.id.trim();
    if (!mid || seen.has(mid)) continue;
    seen.add(mid);
    const context_window = limit(m.context);
    const max_output_tokens = limit(m.output);
    models.push({ id: mid, ...(context_window ? { context_window } : {}), ...(max_output_tokens ? { max_output_tokens } : {}) });
  }
  const key = f.key.trim();
  return { ...(id ? { id } : {}), name: f.name.trim(), base_url: f.baseUrl.trim(), api: f.api, ...(key ? { api_key: key } : {}), models };
}

/** Providers the person added, by name. */
export function customProviders(providers: ProviderSummary[]): ProviderSummary[] {
  return providers.filter((p) => p.custom).sort((a, b) => a.name.localeCompare(b.name));
}
