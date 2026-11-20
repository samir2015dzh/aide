import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { dateOf } from "@aide/core";
import { type Tab } from "./ConnectGuide";
import { lines } from "./connectGuideLogic";
import { Icon } from "./icons";
import { ToastView } from "./MainWindow";
import { SheetHost } from "./Sheets";
import { useNow, useStore } from "./store";
import { Account } from "./settings/Account";
import { AskSheet, type Ask } from "./settings/common";
import { Connect } from "./settings/Connect";
import { General } from "./settings/General";
import { Grants } from "./settings/Grants";
import { Machines } from "./settings/Machines";
import { OpsLog } from "./settings/OpsLog";
import "./settings/settings.css";

const pages = [
  { id: "general", icon: "gear" },
  { id: "connect", icon: "link" },
  { id: "grants", icon: "key" },
  { id: "machines", icon: "laptop" },
  { id: "ops", icon: "terminal" },
  { id: "account", icon: "user" },
] as const;

type Page = (typeof pages)[number]["id"];

/** "machines/<id>": the page, and the item opened in it ("devices" was a page of its own). */
function route(s: string): { page: Page; item: string } {
  const [p, ...rest] = s.split("/");
  if (p === "devices") return { page: "account", item: "" };
  return { page: pages.find((x) => x.id === p)?.id ?? "general", item: rest.join("/") };
}

/** "connect/<line>": the line of the Connect guide to open on. */
const connectLine = (item: string): Tab | undefined => ([...lines, "assistants"] as string[]).includes(item) ? (item as Tab) : undefined;

/**
 * The settings window (#settings or #settings/<page>[/<item>]), laid out as
 * System Settings: the pages in a sidebar, the page on the right. Access and
 * Machines list their items; one opens in place, with ‹ back to the list (a
 * machine's agent, back to the machine).
 */
export function SettingsWindow() {
  const { t } = useTranslation("settings");
  const st = useStore();
  const now = useNow(60_000);
  const [start] = useState(() => route(location.hash.replace(/^#settings\/?/, "")));
  const [asked, setPage] = useState(start.page);
  const [item, setItem] = useState(start.item);
  const [ask, setAsk] = useState<Ask | null>(null);
  // A machine only sets what is this Mac's: no grants, and no other machine (nor connecting agents, which tests them from an owner's device).
  const only = st.status?.machineOnly === true;
  const shown = only ? pages.filter((p) => p.id !== "grants" && p.id !== "ops" && p.id !== "connect") : pages;
  const page = shown.some((p) => p.id === asked) ? asked : "general";
  const title = (p: Page) => (only && p === "machines" ? t("pages.machinesOnly") : only && p === "account" ? t("pages.accountOnly") : t(`pages.${p}`));
  const label = title(page);
  const open = (p: Page, it = "") => (setPage(p), setItem(it));
  // An item within an item ("<machine>/<agent>") goes back to the one it is in;
  // a machine only's page is its machine already.
  const up = item.includes("/") && !only ? item.slice(0, item.lastIndexOf("/")) : "";
  // Grants running out within a week, said once beside Access.
  const soon = st.clients.filter((c) => {
    const d = dateOf(c.expiresAt);
    return !c.revoked && d && d.getTime() - now.getTime() <= 7 * 86_400_000;
  }).length;

  useEffect(
    () =>
      window.aide?.onSettingsTab((s) => {
        const r = route(s);
        open(r.page, r.item);
      }),
    [],
  );
  useEffect(() => {
    document.title = label;
  }, [label]);
  // The window's material shows through the sidebar, as in the main window.
  useEffect(() => {
    if (!window.aide?.material) return;
    document.documentElement.classList.add("vibrant");
    return () => document.documentElement.classList.remove("vibrant");
  }, []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      if (ask) setAsk(null);
      else if (!st.sheet) window.close();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [ask, st.sheet]);

  return (
    <div className="settings">
      <aside className="side set-side">
        <div className="set-side-top drag" />
        <nav className="side-nav" aria-label={t("title")}>
          {shown.map((p) => (
            <button key={p.id} className={`nav-i ${p.id === page ? "sel" : ""}`} aria-current={p.id === page ? "page" : undefined} onClick={() => open(p.id)}>
              <Icon name={p.icon} />
              {title(p.id)}
              {p.id === "grants" && soon > 0 && <span className="set-soon">{t("expiringSoon", { count: soon })}</span>}
            </button>
          ))}
        </nav>
      </aside>
      <main className="set-main" aria-label={label}>
        <div className="set-top drag">
          {item && (
            <button className="set-back" aria-label={t("back", { page: up ? (st.machines.find((m) => m.machineId === up)?.machineName ?? label) : label })} onClick={() => setItem(up)}>
              <Icon name="chevR" />
            </button>
          )}
          <h1>{label}</h1>
        </div>
        <div className="set-body">
          {page === "general" && <General />}
          {page === "connect" && <Connect key={item} line={connectLine(item)} />}
          {page === "grants" && <Grants open={item} select={setItem} />}
          {page === "machines" && <Machines open={item} select={setItem} ask={setAsk} />}
          {page === "ops" && <OpsLog />}
          {page === "account" && <Account ask={setAsk} />}
        </div>
      </main>
      {ask && <AskSheet ask={ask} close={() => setAsk(null)} />}
      <SheetHost />
      <ToastView />
    </div>
  );
}
