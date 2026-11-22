import { useEffect, useState } from "react";
import { Picker } from "../Picker";
import { useTranslation } from "react-i18next";
import { pickLanguage } from "@aide/core";
import type { Appearance, LoginItem, Preferences } from "../../../shared/bridge";
import { languages, resolveLanguage, type LanguageChoice } from "../../../shared/i18n";
import { useUpdate } from "../update";
import { SRow, Switch } from "./common";

/** The preferences that are switches. */
type Flag = { [K in keyof Preferences]: Preferences[K] extends boolean ? K : never }[keyof Preferences];

/** General: the appearance and language, start at login, notifications, the menu bar, updates. */
export function General() {
  const { t } = useTranslation("settings");
  // Kept by the main process, which acts on them while no window is open.
  const [prefs, setPrefs] = useState<Preferences>();
  const [login, setLogin] = useState<LoginItem>();

  useEffect(() => {
    window.aide?.getPreferences().then(setPrefs);
    window.aide?.loginItem().then(setLogin);
  }, []);
  const pref = (k: Flag, title: string, desc?: string) => (
    <SRow title={title} desc={desc}>
      <Switch on={prefs?.[k] ?? false} label={title} disabled={!prefs} set={(on) => window.aide?.setPreferences({ [k]: on }).then(setPrefs)} />
    </SRow>
  );

  return (
    <div className="form">
      <div className="fgroup">
        <SRow title={t("appearance.title")}>
          <div className="seg" role="group" aria-label={t("appearance.title")}>
            {(["system", "light", "dark"] as Appearance[]).map((a) => (
              <button
                key={a}
                type="button"
                className={prefs?.appearance === a ? "on" : ""}
                aria-pressed={prefs?.appearance === a}
                disabled={!prefs}
                onClick={() => window.aide?.setPreferences({ appearance: a }).then(setPrefs)}
              >
                {t(`appearance.${a}`)}
              </button>
            ))}
          </div>
        </SRow>
      </div>
      <Language prefs={prefs} setPrefs={setPrefs} />
      <div>
        <div className="fhead">{t("general.startup")}</div>
        <div className="fgroup">
          <SRow
            title={t("general.openAtLogin")}
            desc={
              <>
                {t("general.openAtLoginDesc")}
                {login?.needsApproval && (
                  <>
                    {" "}{t("general.loginItemsApproval")}{" "}
                    <button className="link" onClick={() => window.aide?.openLoginItems()}>{t("general.openSystemSettings")}</button>
                  </>
                )}
              </>
            }
          >
            <Switch on={login?.on ?? false} label={t("general.openAtLogin")} disabled={!login} set={(on) => window.aide?.setLoginItem(on).then(setLogin)} />
          </SRow>
        </div>
      </div>
      <div>
        <div className="fhead">{t("general.notifications")}</div>
        <div className="fgroup">
          {pref("notifyStarted", t("general.sessionStarted"))}
          {pref("notifyQuestion", t("general.agentQuestions"), t("general.agentQuestionsDesc"))}
          {pref("notifyFailed", t("general.sessionFailed"))}
          {pref("notifyTurnDone", t("general.turnDone"))}
        </div>
      </div>
      <div>
        <div className="fhead">{t("general.menuBar")}</div>
        <div className="fgroup">
          {pref("menuBadge", t("general.menuBadge"))}
        </div>
      </div>
      <Updates />
    </div>
  );
}

/**
 * The app's language: the system's (named after the language it gives now)
 * or one of the app's, each named in its own language. A new choice takes
 * effect when the app next starts; until then, a way to restart now.
 */
function Language({ prefs, setPrefs }: { prefs?: Preferences; setPrefs: (p: Preferences) => void }) {
  const { t } = useTranslation("settings");
  const running = window.aide?.language;
  const system = pickLanguage(navigator.languages);
  const restart = prefs && running && resolveLanguage(prefs.language, navigator.languages) !== running;
  return (
    <div>
      <div className="fgroup">
        <SRow
          title={t("language.title")}
          desc={
            restart && (
              <>
                {t("language.restartHint")}{" "}
                <button className="link" onClick={() => window.aide?.relaunch()}>{t("language.restartNow")}</button>
              </>
            )
          }
        >
          <Picker
            label={t("language.title")}
            value={prefs?.language ?? "system"}
            disabled={!prefs}
            onChange={(v) => window.aide?.setPreferences({ language: v as LanguageChoice }).then(setPrefs)}
            options={[{ value: "system", label: t("language.system", { name: t(`language.names.${system}`) }) }, ...languages.map((l) => ({ value: l as string, label: t(`language.names.${l}`) }))]}
          />
        </SRow>
      </div>
    </div>
  );
}

/** Update status lives in its own window; General keeps only the download preference. */
function Updates() {
  const { t } = useTranslation("settings");
  const info = useUpdate();
  return <div><div className="fhead">{t("update.title")}</div><div className="fgroup">
    <SRow title={t("update.autoDownload")} desc={t("update.autoDownloadDesc")}>
      <Switch label={t("update.autoDownload")} on={info?.autoDownload ?? true} disabled={!info || info.update.state === "unsupported"} set={(on) => { void window.aide?.setAutoDownload(on); }} />
    </SRow>
  </div></div>;
}
