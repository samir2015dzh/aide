import { useEffect, useState } from "react";
import { Picker } from "../Picker";
import { useTranslation } from "react-i18next";
import { pickLanguage } from "@aide/core";
import { Mark } from "@aide/ui";
import type { Appearance, LoginItem, Preferences } from "../../../shared/bridge";
import { languages, resolveLanguage, type LanguageChoice } from "../../../shared/i18n";
import { useUpdate } from "../update";
import { SRow, Switch } from "./common";

/** The preferences that are switches. */
type Flag = { [K in keyof Preferences]: Preferences[K] extends boolean ? K : never }[keyof Preferences];

/** General: which version this is, the appearance and language, start at login, notifications, the menu bar, updates. */
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
      <About />
      <div className="fgroup">
        <SRow title={t("appearance.title")}>
          <Picker
            label={t("appearance.title")}
            value={prefs?.appearance ?? "system"}
            disabled={!prefs}
            onChange={(v) => window.aide?.setPreferences({ appearance: v as Appearance }).then(setPrefs)}
            options={(["system", "light", "dark"] as Appearance[]).map((a) => ({ value: a, label: t(`appearance.${a}`) }))}
          />
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

/**
 * 版本与更新（#103 / #107 §0.5c #8 & §9）：顶部展示版本号、检查更新及「自动下载更新」开关。
 */
function About() {
  const { t } = useTranslation("settings");
  const { t: tu } = useTranslation("updates");
  const info = useUpdate();
  const version = info?.version ?? window.aide?.version ?? "";
  const u = info?.update;
  const newer = u && "version" in u ? u.version : "";
  const said =
    !u || u.state === "idle" ? "" :
    u.state === "error" ? tu(`error.${u.reason}`) :
    u.state === "unsupported" ? tu("unsupported") :
    t(`about.${u.state}`, { version: u.state === "current" ? version : newer });
  const details = u && ["available", "downloading", "ready", "error"].includes(u.state);
  const busy = !u || ["unsupported", "checking", "downloading", "ready"].includes(u.state);
  return (
    <div className="fgroup" aria-label={t("about.title")} role="group">
      <div className="frow about">
        <Mark size={40} />
        <div className="ft">
          <div className="fn">Aide</div>
          <div className="fdesc" data-version={version}>{t("about.version", { version })}</div>
        </div>
        <button className="btn" disabled={busy} onClick={() => window.aide?.checkForUpdates()}>{t("about.check")}</button>
      </div>
      {said && (
        <div className="frow about-said" role="status" aria-label={t("about.status")} data-state={u?.state}>
          <span className="ft">{said}</span>
          {details && <button className="link" onClick={() => window.aide?.openUpdates()}>{t(u?.state === "error" ? "about.errorDetails" : "about.details")}</button>}
        </div>
      )}
      <SRow title={t("update.autoDownload")} desc={t("update.autoDownloadDesc")}>
        <Switch label={t("update.autoDownload")} on={info?.autoDownload ?? true} disabled={!info || info.update.state === "unsupported"} set={(on) => { void window.aide?.setAutoDownload(on); }} />
      </SRow>
    </div>
  );
}
