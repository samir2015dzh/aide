import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Workspace } from "@aide/proto/identity";
import { folderName, isAbsolutePath, projectIdFromFolder } from "@aide/core";
import { Mark } from "@aide/ui";
import { local } from "../api";
import { Icon } from "../icons";
import { useStore } from "../store";
import { ErrorLine, useAction } from "./common";

const releases = "https://github.com/samir2015dzh/aide/releases/latest/download";

/**
 * The command that logs a computer in as a machine of this account:
 * install.sh's, or install.ps1's in PowerShell. It asks for the password on
 * that computer's terminal: it is never part of the command.
 */
function addMachineCommand(windows: boolean, server: string, account: string) {
  const q = (s: string) => `'${s.replace(/'/g, windows ? "''" : "'\\''")}'`;
  return windows
    ? `$env:AIDE_ACCOUNT=${q(account)}; & ([scriptblock]::Create((irm ${releases}/install.ps1))) ${server}`
    : `curl -fsSL ${releases}/install.sh | AIDE_ACCOUNT=${q(account)} sh -s -- ${server}`;
}

/** Where that PC gets the app: the fork's releases (it has no site, and no Mac app). */
const downloadPage = "github.com/samir2015dzh/aide/releases/latest";

type MachineOs = "mac" | "windows" | "linux";

/** A copy button, and the toast it shows. */
function useCopy() {
  const { t } = useTranslation("sheets");
  const st = useStore();
  return (text: string) => (
    <button className="btn sm" onClick={() => (navigator.clipboard.writeText(text).catch(() => {}), st.show(t("copied")))}>
      <Icon name="copy" />
      {t("copy")}
    </button>
  );
}

/**
 * Add Machine: pick that machine's system, then how: the app (a Mac or a
 * Windows PC, which then logs in with the server, the account and the
 * password, as a machine only or as a device) or one command in its
 * terminal (Linux has only this). No one approves it: it shows here once it
 * logged in.
 */
export function AddMachineSheet() {
  const { t } = useTranslation("sheets");
  const st = useStore();
  const [os, setOs] = useState<MachineOs>("mac");
  const [app, setApp] = useState(true);
  const server = st.status?.relayUrl ?? "";
  const account = st.status?.account ?? "";
  const windows = os === "windows";
  const viaApp = app && os !== "linux";
  const command = addMachineCommand(windows, server, account);
  const [before] = useState(() => new Set(st.machines.map((m) => m.machineId)));
  const arrived = st.machines.find((m) => !before.has(m.machineId));
  const copyButton = useCopy();
  return (
    <>
      <h3 id="sheet-title">{t("addMachine.title")}</h3>
      {arrived ? (
        <div className="big-ok">
          <span className="okc"><Icon name="check" /></span>
          <b style={{ fontSize: 15 }}>{t("addMachine.added", { name: arrived.machineName })}</b>
        </div>
      ) : (
        <>
          <p className="sub">{t("addMachine.sub")}</p>
          <div className="os-pick" role="group" aria-label={t("addMachine.system")}>
            {(["mac", "windows", "linux"] as const).map((o) => (
              <button key={o} type="button" className={o === os ? "os on" : "os"} aria-pressed={o === os} onClick={() => setOs(o)}>
                <OsIcon os={o} />
                {o === "mac" ? "Mac" : o === "windows" ? "Windows" : "Linux"}
              </button>
            ))}
          </div>
          <div className="seg" role="group" aria-label={t("addMachine.how")}>
            <button type="button" className={viaApp ? "on" : ""} aria-pressed={viaApp} disabled={os === "linux"} onClick={() => setApp(true)}>
              {t("addMachine.app")}
              {os === "linux" && <span className="tag">{t("addMachine.noApp")}</span>}
            </button>
            <button type="button" className={viaApp ? "" : "on"} aria-pressed={!viaApp} onClick={() => setApp(false)}>{t("addMachine.command")}</button>
          </div>
          <div className="am-panel">
            {viaApp ? (
              <>
                <div className="am-step">
                  <span className="am-n">1</span>
                  {t(windows ? "addMachine.openOnPc" : "addMachine.openOnMac")}
                  <span className="am-chip sel-text">{downloadPage}</span>
                  {copyButton(`https://${downloadPage}`)}
                </div>
                <div className="am-step">
                  <span className="am-n">2</span>
                  {t("addMachine.logIn")}
                  <span className="am-chip sel-text">{server}</span>
                  {copyButton(server)}
                </div>
                <div className="am-step">
                  <span className="am-n">3</span>
                  {t("addMachine.account")}
                  <span className="am-chip sel-text">{account}</span>
                </div>
              </>
            ) : (
              <>
                <div className="am-cmdh">
                  {t(windows ? "addMachine.powershell" : "addMachine.terminal")}
                  {copyButton(command)}
                </div>
                <div className="am-code sel-text">{command}</div>
                <p className="sheet-note muted">{t("addMachine.password")}</p>
              </>
            )}
          </div>
        </>
      )}
      <div className="sheet-foot">
        {!arrived && (
          <span className="am-wait left">
            <Mark size={20} variant="wait" />
            {t("addMachine.waiting")}
          </span>
        )}
        <button className="btn" onClick={() => st.set({ sheet: null })}>{t("done")}</button>
      </div>
    </>
  );
}

function OsIcon({ os }: { os: MachineOs }) {
  if (os === "windows")
    return (
      <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <rect x="3" y="3" width="8.4" height="8.4" rx="1" />
        <rect x="12.6" y="3" width="8.4" height="8.4" rx="1" />
        <rect x="3" y="12.6" width="8.4" height="8.4" rx="1" />
        <rect x="12.6" y="12.6" width="8.4" height="8.4" rx="1" />
      </svg>
    );
  if (os === "linux")
    return (
      <svg viewBox="0 0 24 24" fill="currentColor" fillRule="evenodd" aria-hidden="true">
        <path d="M12 2.5c-2.6 0-4 2-4 4.8 0 1.4-.6 2.6-1.6 4C5 13.1 4 15 4 17c0 1.2.5 2 1.4 2.4L7 20.5c.6.4 1.6 1 2.7 1h4.6c1.1 0 2.1-.6 2.7-1l1.6-1.1c.9-.4 1.4-1.2 1.4-2.4 0-2-1-3.9-2.4-5.7-1-1.4-1.6-2.6-1.6-4 0-2.8-1.4-4.8-4-4.8zM8.6 15.6a3.4 4 0 1 0 6.8 0a3.4 4 0 1 0-6.8 0zM9.9 6.7a.9 1.1 0 1 0 1.8 0a.9 1.1 0 1 0-1.8 0zM12.3 6.7a.9 1.1 0 1 0 1.8 0a.9 1.1 0 1 0-1.8 0z" />
      </svg>
    );
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M16.4 12.6c0-2.2 1.8-3.3 1.9-3.3-1-1.5-2.6-1.7-3.2-1.7-1.4-.1-2.6.8-3.3.8-.7 0-1.7-.8-2.9-.8-1.5 0-2.8.9-3.6 2.2-1.5 2.7-.4 6.6 1.1 8.8.7 1.1 1.6 2.2 2.7 2.2 1.1 0 1.5-.7 2.8-.7 1.3 0 1.7.7 2.9.7 1.2 0 1.9-1.1 2.6-2.1.8-1.2 1.2-2.4 1.2-2.5 0 0-2.3-.9-2.2-3.6zM14.2 6.1c.6-.7 1-1.8.9-2.8-.9 0-1.9.6-2.5 1.3-.6.6-1 1.7-.9 2.7.9.1 1.9-.5 2.5-1.2z" />
    </svg>
  );
}

/**
 * Add Device: another computer or a phone logs in with the server, the
 * account and the password, in its Aide app; no one approves it. It shows
 * here once it did.
 */
export function AddDeviceSheet() {
  const { t } = useTranslation("sheets");
  const st = useStore();
  const server = st.status?.relayUrl ?? "";
  const account = st.status?.account ?? "";
  const [before] = useState(() => new Set(st.devices.map((d) => d.keyId)));
  const arrived = st.devices.find((d) => !before.has(d.keyId));
  const copyButton = useCopy();
  return (
    <>
      <h3 id="sheet-title">{t("addDevice.title")}</h3>
      {arrived ? (
        <div className="big-ok">
          <span className="okc"><Icon name="check" /></span>
          <b style={{ fontSize: 15 }}>{t("addDevice.added", { name: arrived.label })}</b>
        </div>
      ) : (
        <>
          <p className="sub">{t("addDevice.sub")}</p>
          <div className="am-panel">
            <div className="am-step">
              <span className="am-n">1</span>
              {t("addDevice.open")}
            </div>
            <div className="am-step">
              <span className="am-n">2</span>
              {t("addDevice.server")}
              <span className="am-chip sel-text">{server}</span>
              {copyButton(server)}
            </div>
            <div className="am-step">
              <span className="am-n">3</span>
              {t("addDevice.account")}
              <span className="am-chip sel-text">{account}</span>
            </div>
          </div>
          <div className="waitline">
            <Mark size={24} variant="wait" />
            <div>
              <b>{t("addDevice.waiting")}</b>
            </div>
          </div>
        </>
      )}
      <div className="sheet-foot">
        <button className="btn" onClick={() => st.set({ sheet: null })}>{t("done")}</button>
      </div>
    </>
  );
}

/**
 * A project: a directory on an online machine where agents may work (for
 * agent only, when added under it in settings). On this Mac it can be picked.
 */
export function AddProjectSheet({ machineId, agent }: { machineId?: string; agent?: string }) {
  const { t } = useTranslation("sheets");
  const st = useStore();
  // This Mac adds to itself in process, online or not.
  const online = st.machines.filter((m) => m.online || m.local);
  const [chosen, setMid] = useState(machineId);
  // The one asked for or chosen, else this Mac, else any (also once the list arrives).
  const mid = (online.find((m) => m.machineId === chosen) ?? online.find((m) => m.local) ?? online[0])?.machineId ?? "";
  const [path, setPath] = useState("");
  const [id, setId] = useState("");
  const [worktree, setWorktree] = useState(true);
  const [base, setBase] = useState("");
  const act = useAction();
  const machine = online.find((m) => m.machineId === mid);
  const here = machine?.local ?? false;
  const platform = machine?.status?.platform ?? "";
  const taken = new Set(st.projects.filter((p) => p.machineId === mid).map((p) => p.projectId));

  const choosePath = (p: string) => {
    setPath(p);
    const folder = folderName(p);
    setId(folder ? projectIdFromFolder(folder, taken) : "");
  };
  const pick = async () => {
    const p = await window.aide?.pickDirectory();
    if (p) choosePath(p);
  };
  const add = async (e: FormEvent) => {
    e.preventDefault();
    const projectId = id.trim();
    const ok = await act.go(() =>
      local.addProject({
        machineId: mid,
        projectId,
        path: path.trim(),
        workspace: worktree ? Workspace.WORKTREE : Workspace.IN_PLACE,
        defaultBaseRef: worktree ? base.trim() : "",
        agents: agent ? [agent] : [],
      }),
    );
    if (ok) {
      st.set({ sheet: null });
      st.show(t("addProject.added", { name: projectId }));
      st.refresh();
    }
  };

  return (
    <form onSubmit={add}>
      <h3 id="sheet-title">{t("addProject.title")}</h3>
      <p className="sub">{t("addProject.sub")}</p>
      <div className="fgroup">
        <div className="frow">
          <span className="fl">{t("addProject.machine")}</span>
          <span className="fv">
            {online.length ? (
              <div className="seg" role="group" aria-label={t("addProject.machine")}>
                {online.map((m) => (
                  <button type="button" key={m.machineId} className={m.machineId === mid ? "on" : ""} aria-pressed={m.machineId === mid} onClick={() => setMid(m.machineId)}>
                    {m.machineName}
                  </button>
                ))}
              </div>
            ) : (
              t("addProject.noMachines")
            )}
          </span>
        </div>
        <div className="frow">
          <label className="fl" htmlFor="pj-path">{t("addProject.folder")}</label>
          <span className="fv">
            <input id="pj-path" className="field mono" style={{ width: 260 }} autoFocus placeholder={here ? t("addProject.folderPlaceholder") : platform.startsWith("windows/") ? "C:\\code\\my-app" : "/home/me/code/my-app"} value={path} onChange={(e) => choosePath(e.target.value)} />
          </span>
          {here && <button type="button" className="btn sm" onClick={pick}>{t("addProject.choose")}</button>}
        </div>
        <div className="frow">
          <label className="fl" htmlFor="pj-id">{t("addProject.name")}</label>
          <span className="fv">
            <input id="pj-id" className="field mono" style={{ width: 260 }} value={id} onChange={(e) => setId(e.target.value)} />
          </span>
        </div>
        <div className="frow">
          <span className="fl">{t("addProject.workspace")}</span>
          <span className="fv">
            <div className="seg" role="group" aria-label={t("addProject.workspace")}>
              <button type="button" className={worktree ? "on" : ""} aria-pressed={worktree} onClick={() => setWorktree(true)}>{t("addProject.worktree")}</button>
              <button type="button" className={worktree ? "" : "on"} aria-pressed={!worktree} onClick={() => setWorktree(false)}>{t("addProject.inPlace")}</button>
            </div>
          </span>
        </div>
        {worktree && (
          <div className="frow">
            <label className="fl" htmlFor="pj-base">{t("addProject.baseRef")}</label>
            <span className="fv">
              <input id="pj-base" className="field mono" style={{ width: 260 }} placeholder={t("addProject.baseRefPlaceholder")} value={base} onChange={(e) => setBase(e.target.value)} />
            </span>
          </div>
        )}
      </div>
      <p className="sub" style={{ margin: "10px 0 0" }}>
        {worktree ? t("addProject.worktreeHint") : t("addProject.inPlaceHint")}
      </p>
      <ErrorLine text={act.error} />
      <div className="sheet-foot">
        <button type="button" className="btn" onClick={() => st.set({ sheet: null })}>{t("cancel")}</button>
        <button type="submit" className="btn primary" disabled={act.busy || !mid || !id.trim() || !isAbsolutePath(path.trim(), platform)}>
          {act.busy ? t("addProject.adding") : t("addProject.add")}
        </button>
      </div>
    </form>
  );
}
