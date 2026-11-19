import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Device } from "@hermesh/proto/local";
import { platformTitle } from "@hermesh/core";
import { local } from "../api";
import { herDataKept } from "../her/environments";
import { Icon } from "../icons";
import { errorText, useStore } from "../store";
import type { SignerUnlock } from "../../../shared/bridge";
import { copy, More, SRow, type Ask } from "./common";

/**
 * Account & Devices: who this Mac belongs to and where it connects, the
 * trusted devices (each can approve), and signing out. Addresses and
 * commands are folded under Advanced.
 */
export function Account({ ask }: { ask: (a: Ask) => void }) {
  const { t } = useTranslation("settings");
  const st = useStore();
  const s = st.status;
  if (s?.machineOnly) return <MachineAccount />;
  const user = window.hermesh?.userName || "—";
  const relay = s?.relayUrl ?? "";
  const host = URL.canParse(relay) ? new URL(relay).host : relay;
  const addr = s?.connectAddress ?? "";
  const silent = useElevatesSilently();
  const removeDevice = (d: Device) => {
    // Its own machine goes with it, and Her's data there can go too (docs/her-server.md §5.6).
    const m = st.machines.find((x) => x.machineId === d.machineId);
    st.set({
      sheet: {
        kind: "confirm",
        title: t("removeDeviceTitle", { name: d.label }),
        message: t("account.removeDeviceMessage"),
        button: t("removeButton"),
        run: (eraseHer) => local.removeDevice({ keyId: d.keyId, eraseHer }),
        done: t("removed", { name: d.label }),
        herData: m && (m.her?.enabled || herDataKept(m)) ? { name: m.machineName, offline: !m.online } : undefined,
      },
    });
  };
  const leave = () =>
    s?.removed
      ? st.set({
          sheet: {
            kind: "confirm",
            title: t("account.signOutTitle"),
            message: t("account.signOutRemovedMessage"),
            button: t("account.signOutButton"),
            run: async () => {
              await local.leave({});
              window.close();
            },
          },
        })
      : ask({
          title: t("account.signOutTitle"),
          message: t("account.signOutFirstMessage"),
        });
  return (
    <div className="form">
      <div className="dhero">
        <span className="av xl me" aria-hidden="true">{user.slice(0, 1).toUpperCase()}</span>
        <div>
          <b>{user}</b>
          <span>
            {t("account.ownServer", { host: host || "—" })}
            {s && !s.relayConnected && <span className="warn"> · {t("account.notConnected")}</span>}
          </span>
        </div>
      </div>
      <div>
        <div className="fhead">{t("account.trustedDevices")}</div>
        <div className="fgroup" role="list" aria-label={t("account.trustedDevices")}>
          {st.devices.filter((d) => d.platform !== "password").map((d) => (
            <div className="frow" role="listitem" key={d.keyId}>
              <Icon name="laptop" />
              <div className="ft">
                <div className="fn">
                  {d.label}
                  {d.thisDevice && <span className="faint"> {t("thisMac")}</span>}
                </div>
                <div className="fdesc">
                  {[platformTitle(d.platform), d.machineId ? t("account.alsoMachine", { status: t(d.machineOnline ? "online" : "offline") }) : ""].filter(Boolean).join(" · ")}
                </div>
              </div>
              {!d.thisDevice && (
                <button className="btn sm danger" onClick={() => removeDevice(d)}>{t("remove")}</button>
              )}
            </div>
          ))}
          <div className="frow">
            <button className="link" onClick={() => st.set({ sheet: { kind: "addDevice" } })}>{t("account.addDevice")}</button>
          </div>
        </div>
        <p className="set-note">{t("account.devicesNote")}</p>
        {s && <PasswordLogin />}
        <Unlock keyId={st.devices.find((d) => d.thisDevice)?.keyId} />
        {silent && (
          <p className="set-note">
            <span className="warn">{t("account.elevatesSilently")}</span>{" "}
            <button className="link" onClick={() => window.hermesh?.openElevationSettings()}>{t("account.elevationSettings")}</button>
          </p>
        )}
      </div>
      <More label={t("account.advanced")}>
        <div className="fgroup">
          <div className="frow">
            <span className="fl">{t("account.relay")}</span>
            <span className="fv">
              {host || "—"} <i className={`dotc ${s?.relayConnected ? "on" : "warn"}`} />
              {t(s?.relayConnected ? "account.connected" : "account.notConnected")}
            </span>
          </div>
          <CopyRow k={t("account.ownerId")} text={s?.ownerId ?? ""} />
          {addr && <CopyRow k={t("account.connectAddress")} text={addr} />}
          {addr && <CopyRow k={t("account.addMachine")} text={`aided pair ${addr}`} />}
          {addr && <CopyRow k={t("account.agentLogin")} text={`aide auth login ${addr}`} />}
          {/* The server's MCP address carries its key. */}
          {s?.mcpUrl && <CopyRow k={t("account.mcpUrl")} text={s.mcpUrl} />}
        </div>
      </More>
      <div className="set-acts">
        {s && !s.removed && (
          <button className="btn danger" onClick={() => st.set({ sheet: { kind: "erase" } })}>{t("account.eraseAll")}</button>
        )}
        <button className="btn danger push" onClick={leave}>{t("account.signOut")}</button>
      </div>
    </div>
  );
}

/**
 * A machine only's account: whose it is and where it connects, folded its
 * owner id; it approves nothing and has no devices to list. It can become a
 * trusted device (in the main window), or leave.
 */
function MachineAccount() {
  const { t } = useTranslation("settings");
  const { t: f } = useTranslation("firstrun");
  const st = useStore();
  const s = st.status!;
  const user = window.hermesh?.userName || "—";
  const host = URL.canParse(s.relayUrl) ? new URL(s.relayUrl).host : s.relayUrl;
  const leave = () =>
    st.set({
      sheet: {
        kind: "confirm",
        title: f("machineOnly.leaveTitle"),
        message: f("machineOnly.leaveMessage"),
        button: f("machineOnly.leaveButton"),
        run: async () => {
          await local.leave({});
          window.close();
        },
      },
    });
  return (
    <div className="form">
      <div className="dhero">
        <span className="av xl me" aria-hidden="true">{user.slice(0, 1).toUpperCase()}</span>
        <div>
          <b>{user}</b>
          <span>
            {t("account.ownServer", { host: host || "—" })}
            {!s.relayConnected && <span className="warn"> · {t("account.notConnected")}</span>}
          </span>
        </div>
      </div>
      <p className="set-note">{t("account.machineOnlyNote")}</p>
      <More label={t("account.advanced")}>
        <div className="fgroup">
          <div className="frow">
            <span className="fl">{t("account.relay")}</span>
            <span className="fv">
              {host || "—"} <i className={`dotc ${s.relayConnected ? "on" : "warn"}`} />
              {t(s.relayConnected ? "account.connected" : "account.notConnected")}
            </span>
          </div>
          <CopyRow k={t("account.ownerId")} text={s.ownerId} />
        </div>
      </More>
      <div className="set-acts">
        <button className="btn" onClick={() => (window.hermesh?.showUpgrade(), window.close())}>{f("machineOnly.upgrade")}</button>
        <button className="btn danger push" onClick={leave}>{f("machineOnly.leave")}</button>
      </div>
    </div>
  );
}

/**
 * Logging in with an account and password (docs/password-login.md): on, a
 * new phone or computer joins with the account and password alone. Setting
 * or changing it is one Touch ID / Windows Hello; changing it signs out
 * every device and agent that logged in with the old one.
 */
function PasswordLogin() {
  const { t } = useTranslation("settings");
  const st = useStore();
  const [info, setInfo] = useState<{ account: string; server: string; devices: string[] }>();
  const [form, setForm] = useState<{ account: string; password: string; again: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const load = () =>
    local.getLoginPassword({}).then(
      (r) => setInfo({ account: r.account, server: r.server, devices: r.devices }),
      () => {},
    );
  useEffect(() => {
    load();
  }, []);
  if (!info) return null;
  const on = info.account !== "";
  const save = async () => {
    if (!form) return;
    if (form.password !== form.again) return setError(t("account.password.mismatch"));
    setBusy(true);
    setError("");
    try {
      const r = await local.setLoginPassword({ account: form.account.trim(), password: form.password });
      setForm(null);
      st.show(r.removedDevices > 0 ? t("account.password.changed", { count: r.removedDevices }) : t("account.password.saved"));
      await load();
    } catch (e) {
      setError(errorText(e));
    }
    setBusy(false);
  };
  const off = () =>
    st.set({
      sheet: {
        kind: "confirm",
        title: t("account.password.offTitle"),
        message: t("account.password.offMessage"),
        button: t("account.password.offButton"),
        run: async () => {
          await local.setLoginPassword({ account: "", password: "" });
          await load();
        },
        done: t("account.password.offDone"),
      },
    });
  return (
    <div>
      <div className="fhead">{t("account.password.title")}</div>
      <div className="fgroup">
        {!form && (
          <SRow
            title={on ? t("account.password.on", { account: info.account }) : t("account.password.off")}
            desc={on ? t("account.password.onDesc", { server: info.server }) : t("account.password.offDesc")}
          >
            <button className="btn sm" onClick={() => (setError(""), setForm({ account: info.account, password: "", again: "" }))}>
              {on ? t("account.password.change") : t("account.password.turnOn")}
            </button>
            {on && (
              <button className="btn sm danger" onClick={off}>
                {t("account.password.turnOff")}
              </button>
            )}
          </SRow>
        )}
        {form && (
          <form className="pw-form" onSubmit={(e) => (e.preventDefault(), !busy && save())}>
            {(["account", "password", "again"] as const).map((k) => (
              <div className="frow" key={k}>
                <label className="fl" htmlFor={`pw-${k}`}>
                  {t(`account.password.${k}`)}
                </label>
                <span className="fv">
                  <input
                    id={`pw-${k}`}
                    className="field"
                    type={k === "account" ? "text" : "password"}
                    autoComplete={k === "account" ? "username" : "new-password"}
                    value={form[k]}
                    autoFocus={k === (on ? "password" : "account")}
                    disabled={busy}
                    onChange={(e) => (setError(""), setForm({ ...form, [k]: e.target.value }))}
                  />
                </span>
              </div>
            ))}
            <div className="frow">
              <span className="fdesc">{error ? <span className="warn">{error}</span> : t(on ? "account.password.changeNote" : "account.password.turnOnNote")}</span>
              <span className="fv">
                <button type="button" className="btn sm" disabled={busy} onClick={() => setForm(null)}>
                  {t("cancel")}
                </button>
                <button type="submit" className="btn sm primary" disabled={busy || !form.account.trim() || !form.password}>
                  {t("account.password.save")}
                </button>
              </span>
            </div>
          </form>
        )}
        {on && info.devices.length > 0 && !form && (
          <div className="frow">
            <span className="fdesc">{t("account.password.devices", { names: info.devices.join(t("listSeparator")) })}</span>
          </div>
        )}
      </div>
    </div>
  );
}

function CopyRow({ k, text }: { k: string; text: string }) {
  const { t } = useTranslation("settings");
  const st = useStore();
  return (
    <div className="frow">
      <span className="fl">{k}</span>
      <span className="fv">
        <span className="mono sel-text cut">{text || "—"}</span>
        <button className="btn sm" aria-label={t("copyLabel", { label: k })} disabled={!text} onClick={() => copy(st, text)}>
          <Icon name="copy" />
          {t("copy")}
        </button>
      </span>
    </div>
  );
}

/**
 * How this PC unlocks its key for each approval (Windows): Windows Hello,
 * or a passphrase typed in the signer's window. Switching runs the signer
 * elevated (one UAC prompt), which asks the way it is now first. The cost
 * of a passphrase is said as it is, whenever it is on (docs/windows.md §7.5).
 */
function Unlock({ keyId }: { keyId?: string }) {
  const { t } = useTranslation("settings");
  const st = useStore();
  const [mode, setMode] = useState<SignerUnlock>();
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (keyId) window.hermesh?.signerUnlock(keyId).then(setMode, () => {});
  }, [keyId]);
  if (!keyId || !mode) return null;
  const change = async (to: SignerUnlock) => {
    setBusy(true);
    await st.run(async () => {
      await window.hermesh?.setSignerUnlock(keyId, to);
      setMode(await window.hermesh?.signerUnlock(keyId));
    });
    setBusy(false);
  };
  const choose = (to: SignerUnlock) => {
    if (to === mode || busy) return;
    if (to === "hello") return void change(to);
    st.set({
      sheet: {
        kind: "confirm",
        title: t("account.usePassphraseTitle"),
        message: `${t("account.usePassphraseMessage")} ${t("account.passphraseRisk")}`,
        button: t("account.usePassphraseButton"),
        run: () => change(to),
      },
    });
  };
  return (
    <>
      <div className="fgroup">
        <SRow title={t("account.unlock")} desc={t(mode === "passphrase" ? "account.unlockPassphraseDesc" : "account.unlockHelloDesc")}>
          <div className="seg" role="group" aria-label={t("account.unlock")}>
            {(["hello", "passphrase"] as const).map((m) => (
              <button key={m} type="button" className={mode === m ? "on" : ""} aria-pressed={mode === m} disabled={busy} onClick={() => choose(m)}>
                {t(`account.unlockWith.${m}`)}
              </button>
            ))}
          </div>
        </SRow>
      </div>
      {mode === "passphrase" && (
        <p className="set-note">
          <span className="warn">{t("account.passphraseRisk")}</span>
        </p>
      )}
    </>
  );
}

/** Whether programs become administrator here without asking, with the signer set up (Windows). */
function useElevatesSilently(): boolean {
  const [silent, setSilent] = useState(false);
  useEffect(() => {
    window.hermesh?.elevatesSilently().then(setSilent, () => {});
  }, []);
  return silent;
}
