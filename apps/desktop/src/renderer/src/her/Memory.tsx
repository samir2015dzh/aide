import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { BotMemoryPage, BotMemoryResult, BotSummary, ConfirmRule } from "@aide/her-protocol";
import { Icon } from "../icons";
import { Markdown } from "../session/Markdown";
import { errorText, useStore } from "../store";
import { herAnswers, herHas, needsUpdate, type HerHas } from "../skew";
import { Tile } from "./Composer";
import { useHer } from "./context";
import { useSaveFile, type FileScope } from "./Messages";
import { FilePreview, previews } from "./preview/FilePreview";
import { HerUnavailable } from "./Unavailable";

// What an assistant keeps in its memory (docs/her-memory.md), in the place its settings take. MEMORY.md
// is shown as written, as Markdown, whatever form the memory takes: the assistant writes it, and how is
// its prompt's business, not this page's. Changing it goes through the chat.

/**
 * The memory as bots/memory gives it, read again whenever the assistant's last message changes, when
 * `reload` is called (after it was cleared), and when the machine's methods or her connection change
 * (her restarted, updated); null until then.
 * `older`: the machine's Aide is older than this app and has no memory to show (docs/version-skew.md
 * §6.2), as it says or as its answer showed; once it is updated, the memory is read by itself. `has`
 * tells that apart from her not running there (herHas).
 */
export function useMemory(bot: BotSummary): { memory: BotMemoryResult | null; error: string | null; older: boolean; has: HerHas; reload: () => void } {
  const her = useHer();
  const answers = herAnswers(her.machine, "bots/memory");
  const [memory, setMemory] = useState<BotMemoryResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [lacks, setLacks] = useState(false);
  const [round, setRound] = useState(0);
  const last = JSON.stringify(bot.last ?? null);
  const connection = her.env.rpc;
  useEffect(() => {
    if (answers === false || !connection) return;
    let live = true;
    her.call("bots/memory", { bot_id: bot.id }).then(
      (m) => live && (setMemory(m), setError(null), setLacks(false)),
      (e) => live && (setError(errorText(e)), setLacks(needsUpdate(e))),
    );
    return () => {
      live = false;
    };
  }, [bot.id, last, round, answers, connection]);
  const has = herHas(her.machine, "bots/memory", lacks);
  return { memory, error, older: has === "older", has, reload: () => setRound((n) => n + 1) };
}

export const isEmpty = (m: BotMemoryResult) => m.text.trim() === "" && (m.pages ?? []).length === 0;

export function MemoryPage({ bot, files, onBack, onClose }: { bot: BotSummary; files: FileScope; onBack: () => void; onClose: () => void }) {
  const { t } = useTranslation("her");
  const { memory, error, has, reload } = useMemory(bot);
  const her = useHer();
  const st = useStore();
  // The memory goes to the trash, and the assistant stops remembering at once; the chat stays. At the page's foot,
  // with what it clears.
  const clear = () =>
    st.set({
      sheet: {
        kind: "confirm",
        title: t("memoryClear.title", { name: bot.name }),
        message: t("memoryClear.message"),
        button: t("memoryClear.button"),
        run: () => her.call("bots/memory_clear", { bot_id: bot.id }).then(reload),
        done: t("memoryClear.done"),
      },
    });
  return (
    <aside className="her-panel rtn mem" aria-label={t("memory.title")}>
      <div className="her-panel-top drag rtn-top rtn-sub">
        <button className="tb" title={t("memory.back")} aria-label={t("memory.back")} onClick={onBack}>
          <Icon name="chevL" />
        </button>
        <span className="rtn-tt">{t("memory.title")}</span>
        <button className="tb" title={t("panel.close")} aria-label={t("panel.close")} onClick={onClose}>
          <Icon name="x" />
        </button>
      </div>
      <div className="her-panel-body">
        {!memory && <HerUnavailable machine={her.machine} has={has} />}
        {error && !memory && (has === "yes" || has === "unknown") && <p className="set-note">{t("memory.failed", { error })}</p>}
        {memory && isEmpty(memory) && (
          <div className="rtn-empty">
            <b>{t("memory.emptyTitle")}</b>
            {t("memory.emptyBody")}
          </div>
        )}
        {memory && !isEmpty(memory) && (
          <div className="form">
            {memory.text.trim() !== "" && (
              <div className="fgroup mem-doc sel-text">
                <Markdown text={memory.text} />
              </div>
            )}
            {(memory.pages ?? []).length > 0 && <Pages bot={bot} files={files} pages={memory.pages ?? []} />}
            <p className="set-note">{t("memory.note")}</p>
            <section className="fgroup">
              <button className="frow bpn-end" data-testid="her-clear-memory" onClick={clear}>
                {t("panel.clearMemory")}
              </button>
            </section>
          </div>
        )}
        {memory && <ConfirmRules bot={bot} />}
      </div>
    </aside>
  );
}

/** The other files of the memory folder: Markdown and PDF open over the window, as in the conversation; the rest go to Downloads. */
function Pages({ bot, files, pages }: { bot: BotSummary; files: FileScope; pages: BotMemoryPage[] }) {
  const { t, i18n } = useTranslation("her");
  const save = useSaveFile(bot.id);
  const [shown, setShown] = useState<BotMemoryPage | null>(null);
  const day = (at?: string | null) => (at ? new Date(at).toLocaleDateString(i18n.language, { month: "short", day: "numeric" }) : undefined);
  return (
    <section>
      <h3 className="fhead">{t("memory.pages")}</h3>
      <div className="bfiles mem-pages">
        {pages.map((p) => (
          <Tile key={p.path} name={p.name} meta={day(p.modified_at)} views={previews(p.name)} onOpen={() => (previews(p.name) ? setShown(p) : void save(p.path, p.name))} />
        ))}
      </div>
      {shown && <FilePreview scope={files} path={shown.path} name={shown.name} onSave={() => void save(shown.path, shown.name)} onClose={() => setShown(null)} />}
    </section>
  );
}

/**
 * The owner's confirmation rules (docs/her-confirm-rules.md): when the assistant does not need to ask
 * first, or always does. They come from the chat; here they are read and deleted. A machine whose her
 * has none of them shows nothing.
 */
function ConfirmRules({ bot }: { bot: BotSummary }) {
  const { t } = useTranslation("her");
  const her = useHer();
  const st = useStore();
  const answers = herAnswers(her.machine, "bots/confirm_rules");
  const [rules, setRules] = useState<ConfirmRule[] | null>(null);
  const [round, setRound] = useState(0);
  const last = JSON.stringify(bot.last ?? null);
  useEffect(() => {
    if (answers === false || !her.env.rpc) return;
    let live = true;
    her.call("bots/confirm_rules", { bot_id: bot.id }).then(
      (r) => live && setRules(r.rules),
      () => live && setRules(null),
    );
    return () => {
      live = false;
    };
  }, [bot.id, last, round, answers, her.env.rpc]);
  if (!rules) return null;
  const remove = (rule: ConfirmRule) =>
    st.set({
      sheet: {
        kind: "confirm",
        title: t("confirmRules.delete.title"),
        message: t("confirmRules.delete.message"),
        button: t("confirmRules.delete.button"),
        run: () => her.call("bots/confirm_rule_delete", { bot_id: bot.id, rule_id: rule.id }).then(() => setRound((n) => n + 1)),
        done: t("confirmRules.delete.done"),
      },
    });
  return (
    <section data-testid="her-confirm-rules">
      <h3 className="fhead">{t("confirmRules.title")}</h3>
      {rules.length > 0 && (
        <div className="fgroup">
          {rules.map((r) => (
            <div className="frow" key={r.id} data-testid={`her-confirm-rule-${r.id}`}>
              <span className="fl">
                {t(`confirmRules.action.${r.action}`)} · {r.target === "*" ? t("confirmRules.anyTarget") : r.target}
              </span>
              <span className="fv">
                <span className="hp-val">{t(`confirmRules.want.${r.want}`)}</span>
                <button className="link" onClick={() => remove(r)}>
                  {t("confirmRules.delete.button")}
                </button>
              </span>
            </div>
          ))}
        </div>
      )}
      <p className="set-note">{rules.length ? t("confirmRules.note") : t("confirmRules.empty")}</p>
    </section>
  );
}
