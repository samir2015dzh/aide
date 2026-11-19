import { useState } from "react";
import { useTranslation } from "react-i18next";
import { PageTop } from "../components";
import { useHer } from "../her";
import { Icon } from "../icons";
import { ChatGptConnection } from "./ChatGpt";
import { CustomSection } from "./CustomProvider";
import { AddProvider, KeyRow, TokenPlanSection } from "./providers";

// Models: where this environment's models come
// from, as grouped lists. Each row says only what it is, its state, and the one thing to do
// with it. The address `#models/custom` opens the sheet that adds a provider of one's own.

export function ModelsPage({ item, go }: { item: string | null; go: (item?: string) => void }) {
  const { t } = useTranslation("settings");
  const { t: tc } = useTranslation();
  const { models } = useHer();
  const [adding, setAdding] = useState(false);
  // Providers with a key from somewhere; keyless ones (Ollama) need nothing. One's own have their section.
  const keyed = (models?.providers ?? []).filter((p) => !p.plan && !p.custom && p.auth === "api_key" && p.credential !== "missing");
  return (
    <>
      <PageTop title={tc("nav.models")} />
      <div className="set-body">
        <div className="form models">
          <section>
            <h3 className="fhead">{t("models.subscription")}</h3>
            <div className="fgroup">
              <ChatGptConnection />
            </div>
            <p className="set-note">{t("models.subscriptionNote")}</p>
          </section>
          <TokenPlanSection />
          <section>
            <h3 className="fhead">{t("models.api")}</h3>
            <div className="fgroup">
              {keyed.map((p) => (
                <KeyRow key={p.id} p={p} />
              ))}
              <button className="frow mrow-add" disabled={!models} onClick={() => setAdding(true)}>
                <Icon name="plus" />
                {t("models.add")}
              </button>
            </div>
          </section>
          <CustomSection adding={item === "custom"} setAdding={(on) => go(on ? "custom" : undefined)} />
        </div>
      </div>
      {adding && <AddProvider skip={keyed.map((p) => p.id)} onClose={() => setAdding(false)} />}
    </>
  );
}
