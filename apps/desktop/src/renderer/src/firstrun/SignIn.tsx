import { useEffect, useState } from "react";
import type { TFunction } from "i18next";
import { Trans, useTranslation } from "react-i18next";
import { rootPlatform } from "@hermesh/core";
import { Mark } from "@hermesh/ui";
import { local } from "../api";
import { Icon } from "../icons";
import { errorText, save, useStore } from "../store";
import { i18n } from "../../../shared/i18n";
import { Frame, StepIcon } from "./Frame";
import { setProgress } from "./progress";

// Before this computer belongs to an owner: the welcome, logging in with the
// server, account and password, or setting a new server up with its setup
// code, then the page saying what this computer will do (docs/accounts.md).
// No other device approves, and there is no code to compare. Every way
// starts with the owner's own server, typed: no build names one.

/** What this computer is about to log in with, shown on the authorization page; nothing happens before Continue. */
type How =
  | { kind: "login"; server: string; account: string; password: string }
  | { kind: "setUp"; server: string; code: string; account: string; password: string };

type Page = { kind: "welcome" } | { kind: "login" } | { kind: "setUp" } | { kind: "authorize"; how: How };

const bridge = () => window.hermesh!;

export function SignIn() {
  const [page, setPage] = useState<Page>({ kind: "welcome" });
  const welcome = () => setPage({ kind: "welcome" });
  switch (page.kind) {
    case "welcome":
      return <Welcome setUp={() => setPage({ kind: "setUp" })} login={() => setPage({ kind: "login" })} />;
    case "login":
      return <Login back={welcome} next={(how) => setPage({ kind: "authorize", how })} />;
    case "setUp":
      return <SetUp back={welcome} next={(how) => setPage({ kind: "authorize", how })} />;
    case "authorize":
      return <Authorize how={page.how} cancel={() => setPage({ kind: page.how.kind })} />;
  }
}

function Welcome({ setUp, login }: { setUp: () => void; login: () => void }) {
  const { t } = useTranslation("firstrun");
  return (
    <Frame>
      <ErasedNote />
      <div className="hero">
        <div className="mk">
          <div className="glow" />
          <Mark size={96} variant="reveal" />
        </div>
        <h1>{t("welcome.title")}</h1>
        <p className="lede">
          <Trans t={t} i18nKey="welcome.lede" components={{ br: <br /> }} />
        </p>
        <button className="btn primary lg cta" onClick={login}>
          {t("password.start")}
        </button>
      </div>
      <div className="center-line">
        <button className="link" onClick={setUp}>
          {t("welcome.selfHosted")}
        </button>
      </div>
      <p className="promise">
        <Icon name="shield" />
        {t("welcome.promise")}
      </p>
    </Frame>
  );
}
/**
 * Once, after this Mac erased everything (or was erased from another device):
 * what was done, who erases next time it connects, and the worktrees kept.
 */
function ErasedNote() {
  const { t } = useTranslation("firstrun");
  const st = useStore();
  const e = st.status?.erased;
  const id = e ? e.eraseId || String(e.at?.seconds ?? "") : "";
  const [seen, setSeen] = useState(() => {
    try {
      return localStorage.getItem("erasedSeen") ?? "";
    } catch {
      return "";
    }
  });
  if (!e || seen === id) return null;
  const dismiss = () => {
    try {
      localStorage.setItem("erasedSeen", id);
    } catch {
      // Shown again next time.
    }
    setSeen(id);
  };
  return (
    <div className="erased-note" role="status">
      <Icon name={e.by ? "info" : "check"} />
      <div className="t">
        <b>{e.by ? t("erased.by", { device: e.by }) : t("erased.done")}</b>
        {e.by && <span>{t("erased.byBody")}</span>}
        {e.pending.length > 0 && <span>{t("erased.pending", { names: e.pending.join(i18n.t("settings:listSeparator")) })}</span>}
        {e.keptWorktrees.length > 0 && (
          <>
            <span>{t("erased.kept")}</span>
            {e.keptWorktrees.map((p) => (
              <span key={p} className="kept">
                <span className="mono sel-text">{p}</span>
                <button className="link" onClick={() => bridge().revealKeptWorktree(p)}>{t("erased.reveal")}</button>
              </span>
            ))}
          </>
        )}
      </div>
      <button className="x" aria-label={t("erased.dismiss")} onClick={dismiss}>
        <Icon name="x" />
      </button>
    </div>
  );
}


/** The last server typed, kept for next time: there is no default. */
function lastServer(): string {
  try {
    return localStorage.getItem("loginServer") ?? "";
  } catch {
    return "";
  }
}

function keepServer(server: string) {
  try {
    localStorage.setItem("loginServer", server);
  } catch {
    // Typed again next time.
  }
}

/** Logging in with the owner's server, account and password. */
function Login({ back, next }: { back: () => void; next: (how: How) => void }) {
  const { t } = useTranslation("firstrun");
  const [server, setServer] = useState(lastServer);
  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const ok = server.trim() !== "" && account.trim() !== "" && password !== "";
  const go = () => (keepServer(server.trim()), next({ kind: "login", server: server.trim(), account: account.trim(), password }));
  return (
    <Frame foot={<Foot back={back} ok={ok} go={go} />}>
      <StepIcon name="key" />
      <h2 className="ob-h">{t("password.title")}</h2>
      <p className="ob-p">{t("password.body")}</p>
      <form className="fgroup ob-form" onSubmit={(e) => (e.preventDefault(), ok && go())}>
        <Field id="server" label={t("password.server")} placeholder="relay.example.com" value={server} set={setServer} autoFocus={server === ""} />
        <Field id="account" label={t("password.account")} autoComplete="username" value={account} set={setAccount} autoFocus={server !== ""} />
        <Field id="password" label={t("password.password")} type="password" autoComplete="current-password" value={password} set={setPassword} />
        <button type="submit" hidden />
      </form>
      {plainHTTP(server) && <PlainWarning />}
    </Frame>
  );
}

/** A new server of one's own (aide-relay): its address, the setup code it printed, and the account it will keep. */
function SetUp({ back, next }: { back: () => void; next: (how: How) => void }) {
  const { t } = useTranslation("firstrun");
  const [server, setServer] = useState("");
  const [code, setCode] = useState("");
  const [account, setAccount] = useState("");
  const [password, setPassword] = useState("");
  const ok = server.trim() !== "" && code.trim() !== "" && account.trim() !== "" && password.length >= 8;
  const go = () => (keepServer(server.trim()), next({ kind: "setUp", server: server.trim(), code: code.trim(), account: account.trim(), password }));
  return (
    <Frame foot={<Foot back={back} ok={ok} go={go} />}>
      <StepIcon name="server" />
      <h2 className="ob-h">{t("setUp.title")}</h2>
      <p className="ob-p">{t("setUp.body")}</p>
      <form className="fgroup ob-form" onSubmit={(e) => (e.preventDefault(), ok && go())}>
        <Field id="relay" label={t("setUp.address")} placeholder="relay.example.com" value={server} set={setServer} autoFocus />
        <Field id="code" label={t("setUp.code")} mono placeholder="XXXX-XXXX-XXXX-XXXX" value={code} set={setCode} />
        <Field id="account" label={t("setUp.account")} autoComplete="username" value={account} set={setAccount} />
        <Field id="password" label={t("setUp.password")} type="password" autoComplete="new-password" value={password} set={setPassword} />
        <button type="submit" hidden />
      </form>
      {plainHTTP(server) && <PlainWarning />}
    </Frame>
  );
}

function Foot({ back, ok, go }: { back: () => void; ok: boolean; go: () => void }) {
  const { t } = useTranslation("firstrun");
  return (
    <>
      <span className="note" />
      <button className="btn" onClick={back}>
        {t("back")}
      </button>
      <button className="btn primary" disabled={!ok} onClick={go}>
        {t("continue")}
      </button>
    </>
  );
}

function Field(p: {
  id: string;
  label: string;
  value: string;
  set: (v: string) => void;
  placeholder?: string;
  type?: string;
  autoComplete?: string;
  autoFocus?: boolean;
  mono?: boolean;
}) {
  return (
    <div className="frow">
      <label htmlFor={p.id}>{p.label}</label>
      <span className="fv">
        <input
          id={p.id}
          className={p.mono ? "field mono" : "field"}
          type={p.type}
          placeholder={p.placeholder}
          autoComplete={p.autoComplete}
          value={p.value}
          onChange={(e) => p.set(e.target.value)}
          autoFocus={p.autoFocus}
        />
      </span>
    </div>
  );
}

function PlainWarning() {
  const { t } = useTranslation("firstrun");
  return (
    <p className="ob-warn" role="note">
      <Icon name="warn" />
      {t("setUp.plain")}
    </p>
  );
}

/**
 * Whether a server address is plain HTTP to another computer: "http://…", or
 * an IP address, which the app reaches by HTTP (the relay gets certificates
 * for domain names only; internal/owner's normalizeRelayURL).
 */
function plainHTTP(address: string): boolean {
  const s = address.trim();
  const http = /^http:\/\//i.test(s);
  if (!http && s.includes("://")) return false;
  const host = (http ? s.slice(7) : s).split("/")[0];
  const v6 = /^\[([^\]]+)\]/.exec(host);
  const h = v6 ? v6[1] : host.replace(/:\d*$/, "");
  if (!http && !v6 && !/^\d{1,3}(\.\d{1,3}){3}$/.test(h)) return false;
  return !(h === "localhost" || h.startsWith("127.") || h === "::1");
}

/**
 * Before logging in: what this computer will do, row by row. Being one of
 * the owner's devices is on unless the person turns it off (logging in, not
 * setting up): then it is a machine only. Working here and reminders are on
 * unless turned off.
 */
function Authorize({ how, cancel }: { how: How; cancel: () => void }) {
  const { t } = useTranslation("firstrun");
  const st = useStore();
  const [device, setDevice] = useState(true);
  const [share, setShare] = useState(true);
  const machine = share || !device;
  const [remind, setRemind] = useState(true);
  const [busy, setBusy] = useState(false);
  const [trouble, setTrouble] = useState<Trouble | null>(null);

  const go = async () => {
    // Reminders: start with the login session (in the menu bar) and notify.
    for (const k of ["openAtLogin", "notifyQuestion", "notifyFailed"]) save(k, remind);
    (window.hermesh as { setLoginItem?: (on: boolean) => void } | undefined)?.setLoginItem?.(remind);
    setBusy(true);
    // The pages after this one, before hermeshd says it is set up (so the main window never flashes).
    setProgress(!device ? null : machine ? { step: "prepare" } : { step: "agent" });
    try {
      const name = await bridge().computerName();
      const platform = rootPlatform(bridge().platform);
      const localMachineName = machine ? name : "";
      if (how.kind === "setUp") {
        await local.setup({ relayUrl: how.server, setupCode: how.code, account: how.account, password: how.password, label: name, platform, localMachineName });
      } else if (!device) {
        await local.logIn({ relayUrl: how.server, account: how.account, password: how.password, label: name, platform, machineOnly: true });
      } else {
        await local.logIn({ relayUrl: how.server, account: how.account, password: how.password, label: name, platform, localMachineName });
      }
      await st.refresh();
    } catch (e) {
      setProgress(null);
      setTrouble(troubleOf(t, e, t("authorize.failed")));
      setBusy(false);
    }
  };

  return (
    <Frame
      sheet={trouble && <TroubleSheet trouble={trouble} close={() => setTrouble(null)} />}
      foot={
        <>
          <span className="note" />
          <button className="btn" onClick={cancel} disabled={busy}>
            {t("cancel")}
          </button>
          <button className="btn primary" onClick={go} disabled={busy}>
            {busy ? t("authorize.settingUp") : t("continue")}
          </button>
        </>
      }
    >
      <StepIcon name="key" />
      <h2 className="ob-h">{t("authorize.title")}</h2>
      <p className="ob-p">{t("authorize.body")}</p>
      <div className="perm">
        <div className="prow">
          <span className="pi">
            <Icon name="devices" />
          </span>
          <div className="pt">
            <b>{t("authorize.device")}</b>
            <span>{t(device ? "authorize.deviceBody" : "authorize.deviceOff")}</span>
          </div>
          {how.kind === "login" ? <Switch on={device} set={setDevice} label={t("authorize.device")} disabled={busy} /> : <span className="req">{t("authorize.required")}</span>}
        </div>
        <div className="prow">
          <span className="pi">
            <Icon name="terminal" />
          </span>
          <div className="pt">
            <b>{t("authorize.share")}</b>
            <span>{t(device ? "authorize.shareBody" : "authorize.shareBodyMachine")}</span>
          </div>
          {device ? <Switch on={share} set={setShare} label={t("authorize.share")} disabled={busy} /> : <span className="req">{t("authorize.required")}</span>}
        </div>
        <div className="prow">
          <span className="pi">
            <Icon name="bell" />
          </span>
          <div className="pt">
            <b>{t("authorize.remind")}</b>
            <span>{t(device ? "authorize.remindBody" : "authorize.remindBodyMachine")}</span>
          </div>
          <Switch on={remind} set={setRemind} label={t("authorize.remind")} disabled={busy} />
        </div>
      </div>
    </Frame>
  );
}

function Switch({ on, set, label, disabled }: { on: boolean; set: (on: boolean) => void; label: string; disabled?: boolean }) {
  return <button className={`sw ${on ? "on" : ""}`} role="switch" aria-checked={on} aria-label={label} disabled={disabled} onClick={() => set(!on)} />;
}

/**
 * A machine only becoming one of the owner's devices too: the account and
 * password again, on the server it already uses; its machine stays.
 */
export function Upgrade({ relay, cancel }: { relay: string; cancel: () => void }) {
  const { t } = useTranslation("firstrun");
  const st = useStore();
  const [account, setAccount] = useState(st.status?.account ?? "");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [trouble, setTrouble] = useState<Trouble | null>(null);
  const ok = account.trim() !== "" && password !== "" && !busy;
  const go = async () => {
    setBusy(true);
    setProgress({ step: "prepare", joined: true });
    try {
      const name = await bridge().computerName();
      await local.logIn({ relayUrl: relay, account: account.trim(), password, label: name, platform: rootPlatform(bridge().platform), localMachineName: name });
      await st.refresh();
      st.set({ upgrading: false });
    } catch (e) {
      setProgress(null);
      setTrouble(troubleOf(t, e, t("authorize.failed")));
      setBusy(false);
    }
  };
  return (
    <Frame
      sheet={trouble && <TroubleSheet trouble={trouble} close={() => setTrouble(null)} />}
      foot={
        <>
          <span className="note" />
          <button className="btn" onClick={cancel} disabled={busy}>
            {t("cancel")}
          </button>
          <button className="btn primary" disabled={!ok} onClick={go}>
            {busy ? t("authorize.settingUp") : t("continue")}
          </button>
        </>
      }
    >
      <StepIcon name="devices" />
      <h2 className="ob-h">{t("password.title")}</h2>
      <p className="ob-p">{t("password.body")}</p>
      <form className="fgroup ob-form" onSubmit={(e) => (e.preventDefault(), ok && go())}>
        <Field id="account" label={t("password.account")} autoComplete="username" value={account} set={setAccount} autoFocus={account === ""} />
        <Field id="password" label={t("password.password")} type="password" autoComplete="current-password" value={password} set={setPassword} autoFocus={account !== ""} />
        <button type="submit" hidden />
      </form>
    </Frame>
  );
}

/**
 * This computer is no longer one of the owner's devices: another signed it
 * out, or the account started over. Or, a machine only, its owner
 * removed it.
 */
export function Removed() {
  const { t } = useTranslation("firstrun");
  const st = useStore();
  return (
    <Frame>
      <div className="status">
        <Icon name="devices" className="ob-big-ic" />
        <h2 className="ob-h">{t("removed.title")}</h2>
        <p className="ob-p">{t(st.status?.machineOnly ? "removed.bodyMachine" : "removed.body")}</p>
        <button
          className="btn primary lg"
          onClick={() =>
            st.run(async () => {
              await local.leave({});
              setProgress(null);
            })
          }
        >
          {t("removed.setUp")}
        </button>
      </div>
    </Frame>
  );
}

/** A failure that stops this step: what happened, what to do, and the raw cause behind Details. */
type Trouble = { title: string; message: string; detail?: string };

/**
 * e said for the person. hermeshd checks the server before logging in and
 * says "relay unreachable (host): reason" when this network can't reach it.
 */
function troubleOf(t: TFunction<"firstrun">, e: unknown, title: string): Trouble {
  const text = errorText(e);
  const m = /^relay unreachable \(([^)]+)\): (.*)$/s.exec(text);
  if (m) return { title: t("unreachable.title"), message: t("unreachable.body", { host: m[1] }), detail: m[2] };
  return { title, message: text };
}

function TroubleSheet({ trouble, close }: { trouble: Trouble; close: () => void }) {
  const { t } = useTranslation("firstrun");
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const esc = (e: KeyboardEvent) => e.key === "Escape" && close();
    window.addEventListener("keydown", esc);
    return () => window.removeEventListener("keydown", esc);
  }, [close]);
  return (
    <>
      <div className="sheet-scrim" />
      <div className="sheet trouble" role="alertdialog" aria-modal="true" aria-labelledby="trouble-title" aria-describedby="trouble-body">
        <h3 id="trouble-title">{trouble.title}</h3>
        <p className="sub" id="trouble-body">{trouble.message}</p>
        {trouble.detail && (
          <>
            <button className="link" aria-expanded={open} onClick={() => setOpen(!open)}>
              {t("details")} <Icon name="chevR" className={`sm chev ${open ? "open" : ""}`} />
            </button>
            {open && <div className="trouble-raw sel-text">{trouble.detail}</div>}
          </>
        )}
        <div className="sheet-foot">
          <button className="btn primary" onClick={close} autoFocus>
            {t("ok")}
          </button>
        </div>
      </div>
    </>
  );
}

function Problem({ text }: { text: string }) {
  return (
    <span className="ob-err" role="alert">
      <Icon name="warn" />
      {text}
    </span>
  );
}
