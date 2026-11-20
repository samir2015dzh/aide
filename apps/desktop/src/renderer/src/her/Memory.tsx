import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { BotMemoryEntry, BotMemoryPage, BotMemoryProposal, BotMemoryResult, BotSummary } from "@aide/her-protocol";
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
// its prompt's business, not this page's. Changing it goes through the chat. Knowledge entries (§10) are the
// owner's: Her proposes them, and here the owner saves or declines a proposal, and changes or deletes an entry.

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

export const isEmpty = (m: BotMemoryResult) =>
  m.text.trim() === "" && (m.pages ?? []).length === 0 && (m.entries ?? []).length === 0 && (m.proposals ?? []).length === 0;

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
            {(memory.proposals ?? []).length > 0 && <Proposals bot={bot} proposals={memory.proposals ?? []} onChanged={reload} />}
            {memory.text.trim() !== "" && (
              <div className="fgroup mem-doc sel-text">
                <Markdown text={memory.text} />
              </div>
            )}
            {(memory.pages ?? []).length > 0 && <Pages bot={bot} files={files} pages={memory.pages ?? []} />}
            {(memory.text.trim() !== "" || (memory.pages ?? []).length > 0) && <p className="set-note">{t("memory.note")}</p>}
            {(memory.entries ?? []).length > 0 && <Entries bot={bot} entries={memory.entries ?? []} onChanged={reload} />}
            <section className="fgroup">
              <button className="frow bpn-end" data-testid="her-clear-memory" onClick={clear}>
                {t("panel.clearMemory")}
              </button>
            </section>
          </div>
        )}
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

/** What Her proposed to keep: nothing of it is in her memory until the owner saves it. */
function Proposals({ bot, proposals, onChanged }: { bot: BotSummary; proposals: BotMemoryProposal[]; onChanged: () => void }) {
  const { t } = useTranslation("her");
  const her = useHer();
  const answer = (p: BotMemoryProposal, accept: boolean) =>
    void her.run(() => her.call("bots/memory_proposal_answer", { bot_id: bot.id, proposal_id: p.id, accept }).then(onChanged), t(accept ? "memory.saved" : "memory.declined"));
  return (
    <section>
      <h3 className="fhead">{t("memory.proposals")}</h3>
      {proposals.map((p) => (
        <div className="fgroup mem-entry" key={p.id} data-testid="her-memory-proposal" aria-label={p.name}>
          <EntryView entry={p} />
          {p.replaces && <p className="set-note">{t("memory.replaces", { name: p.name })}</p>}
          <div className="rtn-acts">
            <button className="btn" onClick={() => answer(p, true)}>
              {t("memory.save")}
            </button>
            <button className="btn" onClick={() => answer(p, false)}>
              {t("memory.decline")}
            </button>
          </div>
        </div>
      ))}
      <p className="set-note">{t("memory.proposalsNote")}</p>
    </section>
  );
}

/** Knowledge entries: each comes up only when the owner's words hit its trigger. The owner changes or deletes them here. */
function Entries({ bot, entries, onChanged }: { bot: BotSummary; entries: BotMemoryEntry[]; onChanged: () => void }) {
  const { t } = useTranslation("her");
  const her = useHer();
  const st = useStore();
  const [editing, setEditing] = useState<string | null>(null);
  const remove = (e: BotMemoryEntry) =>
    st.set({
      sheet: {
        kind: "confirm",
        title: t("memory.deleteTitle", { name: e.name }),
        message: t("memory.deleteMessage"),
        button: t("memory.delete"),
        run: () => her.call("bots/memory_entry_delete", { bot_id: bot.id, name: e.name }).then(onChanged),
        done: t("memory.deleted"),
      },
    });
  return (
    <section>
      <h3 className="fhead">{t("memory.entries")}</h3>
      {entries.map((e) =>
        editing === e.name ? (
          <EntryForm key={e.name} bot={bot} entry={e} onDone={() => (setEditing(null), onChanged())} onCancel={() => setEditing(null)} />
        ) : (
          <div className="fgroup mem-entry" key={e.name} data-testid="her-memory-entry" aria-label={e.name}>
            <EntryView entry={e} />
            <div className="rtn-acts">
              <button className="btn" onClick={() => setEditing(e.name)}>
                {t("memory.edit")}
              </button>
              <button className="btn danger" onClick={() => remove(e)}>
                {t("memory.delete")}
              </button>
            </div>
          </div>
        ),
      )}
      <p className="set-note">{t("memory.entriesNote")}</p>
    </section>
  );
}

function EntryView({ entry }: { entry: { name: string; trigger: string; text: string } }) {
  const { t } = useTranslation("her");
  return (
    <>
      <div className="mem-name">{entry.name}</div>
      <div className="mem-trigger">{entry.trigger ? t("memory.trigger", { trigger: entry.trigger }) : t("memory.noTrigger")}</div>
      <div className="rtn-prompt sel-text">{entry.text}</div>
    </>
  );
}

function EntryForm({ bot, entry, onDone, onCancel }: { bot: BotSummary; entry: BotMemoryEntry; onDone: () => void; onCancel: () => void }) {
  const { t } = useTranslation("her");
  const her = useHer();
  const [trigger, setTrigger] = useState(entry.trigger);
  const [text, setText] = useState(entry.text);
  const save = () =>
    void her.run(() => her.call("bots/memory_entry_set", { bot_id: bot.id, name: entry.name, trigger, text }).then(onDone), t("memory.saved"));
  return (
    <div className="fgroup mem-entry" data-testid="her-memory-edit" aria-label={entry.name}>
      <div className="mem-name">{entry.name}</div>
      <input className="field" aria-label={t("memory.triggerLabel")} placeholder={t("memory.triggerLabel")} value={trigger} spellCheck={false} onChange={(e) => setTrigger(e.target.value)} />
      <textarea className="field mem-text" aria-label={t("memory.textLabel")} value={text} rows={4} onChange={(e) => setText(e.target.value)} />
      <div className="rtn-acts">
        <button className="btn" disabled={!trigger.trim() || !text.trim()} onClick={save}>
          {t("memory.save")}
        </button>
        <button className="btn" onClick={onCancel}>
          {t("memory.cancel")}
        </button>
      </div>
    </div>
  );
}
