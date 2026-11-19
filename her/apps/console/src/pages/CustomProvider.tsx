import { useState } from "react";
import { useTranslation } from "react-i18next";
import { RpcError } from "@hermesh/her-client";
import type { CustomModel, ProviderSummary } from "@hermesh/her-protocol";
import { Dialog } from "../components";
import { errorText } from "../format";
import { useHer } from "../her";
import { Icon } from "../icons";
import { Brand } from "./providers";
import { customProviders, formOf, problem, saveParams, toggle, type Api, type Form } from "./customProviderForm";

// Providers of one's own (providers/*): an OpenAI- or Anthropic-compatible service at an
// address, such as a company gateway. Added and changed in one sheet: its name, address,
// protocol and key, and its models, picked from the service's own list when it gives one, or
// named by hand. her keeps them in models.toml and the key in its credential store; the page
// keeps nothing.

/** The sheet that adds a provider of one's own, or with `provider` changes it. */
export function CustomProviderSheet({ provider, onClose }: { provider?: ProviderSummary; onClose: () => void }) {
  const { t } = useTranslation("settings");
  const her = useHer();
  const [form, setForm] = useState<Form>(() => formOf(provider));
  const [listed, setListed] = useState<CustomModel[] | null>(null);
  const [listNote, setListNote] = useState("");
  const [busy, setBusy] = useState<"list" | "save" | null>(null);
  const [error, setError] = useState("");
  const set = (patch: Partial<Form>) => setForm((f) => ({ ...f, ...patch }));
  const setRow = (i: number, patch: Partial<Form["models"][number]>) => set({ models: form.models.map((m, j) => (j === i ? { ...m, ...patch } : m)) });
  const keyed = !provider || provider.auth === "api_key";
  const title = provider ? t("models.custom.editTitle", { name: provider.name }) : t("models.custom.add");

  const fetchList = async () => {
    setBusy("list");
    setError("");
    setListNote("");
    try {
      const key = form.key.trim();
      const r = await her.call("providers/models", { base_url: form.baseUrl.trim(), api: form.api, ...(key ? { api_key: key } : provider ? { provider: provider.id } : {}) });
      setListed(r.models);
      if (!r.listed) setListNote(r.error ? t("models.custom.listFailed", { message: r.error }) : t("models.custom.noList"));
    } catch (e) {
      setListed(null);
      setError(e instanceof RpcError && e.kind === "key_rejected" ? t("key.rejected", { message: e.message }) : errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const save = async () => {
    const wrong = problem(form);
    if (wrong) return setError(t(`models.custom.problem.${wrong}`));
    setBusy("save");
    setError("");
    try {
      await her.call("providers/save", saveParams(form, provider?.id));
      await her.refreshModels();
      her.show(t("models.custom.saved", { name: form.name.trim() }));
      onClose();
    } catch (e) {
      setError(errorText(e));
      setBusy(null);
    }
  };

  return (
    <Dialog label={title} width={600} className="models-add" onClose={onClose}>
      <div className="provider-dialog-top">
        <span>{title}</span>
        <button className="provider-icon-button" aria-label={t("models.close")} onClick={onClose}>
          <Icon name="x" />
        </button>
      </div>
      <form
        className="custom-provider"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <p className="sub">{t("models.custom.note")}</p>
        <div className="provider-form">
          <label>
            {t("models.custom.name")}
            <input className="field" autoFocus spellCheck={false} value={form.name} placeholder={t("models.custom.namePlaceholder")} onChange={(e) => set({ name: e.target.value })} />
          </label>
          <label>
            {t("models.custom.baseUrl")}
            <input className="field mono" spellCheck={false} value={form.baseUrl} placeholder="https://example.com/v1" onChange={(e) => set({ baseUrl: e.target.value })} />
          </label>
          <label>
            {t("models.custom.protocol")}
            <select className="field" value={form.api} onChange={(e) => set({ api: e.target.value as Api })}>
              <option value="openai-chat">{t("models.custom.openai")}</option>
              <option value="anthropic-messages">{t("models.custom.anthropic")}</option>
            </select>
          </label>
          {keyed && (
            <label>
              API Key
              <input
                className="field"
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={form.key}
                placeholder={provider?.credential === "stored" ? t("models.custom.keyKept") : t("models.custom.keyPlaceholder")}
                onChange={(e) => set({ key: e.target.value })}
              />
            </label>
          )}
          <div className="custom-models">
            <div className="custom-models-h">
              <span>{t("models.custom.models")}</span>
              <button type="button" className="btn" disabled={busy !== null || !form.baseUrl.trim()} onClick={() => void fetchList()}>
                {busy === "list" ? t("models.custom.fetching") : t("models.custom.fetch")}
              </button>
            </div>
            {listNote && <p className="provider-form-note">{listNote}</p>}
            {listed && listed.length > 0 && (
              <div className="custom-listed" role="group" aria-label={t("models.custom.listed")}>
                {listed.map((m) => (
                  <label key={m.id} className="custom-listed-row">
                    <input type="checkbox" checked={form.models.some((r) => r.id === m.id)} onChange={() => set({ models: toggle(form.models, m) })} />
                    <span className="mono">{m.id}</span>
                  </label>
                ))}
              </div>
            )}
            {form.models.map((m, i) => (
              <div key={i} className="custom-model-row">
                <input className="field mono" spellCheck={false} aria-label={t("models.custom.modelId")} placeholder={t("models.custom.modelId")} value={m.id} onChange={(e) => setRow(i, { id: e.target.value })} />
                <input className="field" aria-label={t("models.custom.context")} placeholder={t("models.custom.context")} value={m.context} onChange={(e) => setRow(i, { context: e.target.value })} />
                <input className="field" aria-label={t("models.custom.output")} placeholder={t("models.custom.output")} value={m.output} onChange={(e) => setRow(i, { output: e.target.value })} />
                <button type="button" className="provider-icon-button" aria-label={t("models.custom.removeModel", { id: m.id })} onClick={() => set({ models: form.models.filter((_, j) => j !== i) })}>
                  <Icon name="x" />
                </button>
              </div>
            ))}
            <button type="button" className="link custom-add-model" onClick={() => set({ models: [...form.models, { id: "", context: "", output: "" }] })}>
              <Icon name="plus" />
              {t("models.custom.addModel")}
            </button>
            <p className="provider-form-note">{t("models.custom.limitsNote")}</p>
          </div>
          {error && (
            <p className="err" role="alert">
              {error}
            </p>
          )}
        </div>
        <div className="connection-card-footer">
          <span>
            <Icon name="lock" />
            {t("key.where")}
          </span>
          <div className="key-actions">
            <button type="button" className="btn" disabled={busy === "save"} onClick={onClose}>
              {t("models.cancel")}
            </button>
            <button type="submit" className="btn primary" disabled={busy !== null}>
              {busy === "save" ? t("key.saving") : t("key.save")}
            </button>
          </div>
        </div>
      </form>
    </Dialog>
  );
}

/** One provider of one's own: marked Custom, where it is and how many models; changed or removed here. */
function CustomRow({ p }: { p: ProviderSummary }) {
  const { t } = useTranslation("settings");
  const her = useHer();
  const [editing, setEditing] = useState(false);
  const count = p.custom?.models.length ?? 0;
  const key = p.credential === "missing" ? t("models.custom.noKey") : t(`models.source.${p.credential}` as "models.source.stored", { env: p.credential_source ?? "", defaultValue: "" });
  const remove = () =>
    her.confirm({
      title: t("models.custom.removeTitle", { name: p.name }),
      message: t("models.custom.removeNote"),
      button: t("models.confirmRemove"),
      run: async () => {
        await her.call("providers/remove", { id: p.id });
        await her.refreshModels();
      },
    });
  return (
    <>
      <div className="frow mrow" data-provider={p.id}>
        <Brand id={p.id} name={p.name} />
        <div className="ft">
          <div className="fn">
            {p.name}
            <span className="badge acc">{t("models.custom.badge")}</span>
          </div>
          <div className="fdesc">{[p.custom?.base_url, key, t("key.models", { count })].filter(Boolean).join(" · ")}</div>
        </div>
        <button className="btn" onClick={() => setEditing(true)}>
          {t("models.custom.edit")}
        </button>
        <button className="btn" onClick={remove}>
          {t("models.remove")}
        </button>
      </div>
      {editing && <CustomProviderSheet provider={p} onClose={() => setEditing(false)} />}
    </>
  );
}

/** The providers of one's own, and the entry that adds another; `adding` opens it (the page's address says so). */
export function CustomSection({ adding, setAdding }: { adding: boolean; setAdding: (on: boolean) => void }) {
  const { t } = useTranslation("settings");
  const { models } = useHer();
  const mine = customProviders(models?.providers ?? []);
  return (
    <section aria-label={t("models.custom.title")}>
      <h3 className="fhead">{t("models.custom.title")}</h3>
      <div className="fgroup">
        {mine.map((p) => (
          <CustomRow key={p.id} p={p} />
        ))}
        <button className="frow mrow-add" disabled={!models} onClick={() => setAdding(true)}>
          <Icon name="plus" />
          {t("models.custom.add")}
        </button>
      </div>
      <p className="set-note">{t("models.custom.sectionNote")}</p>
      {adding && <CustomProviderSheet onClose={() => setAdding(false)} />}
    </section>
  );
}
