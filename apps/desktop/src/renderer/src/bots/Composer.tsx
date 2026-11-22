import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Icon } from "../icons";

// The box to write to a bot (工单 #66, after BotArk's composer): one rounded capsule at the foot of the page — + on the
// left for what goes with the message (a new conversation, the project…), the line to write in, saying whom it goes
// to, and a round send button on the right, lit once there is something to send. What it goes with (the project, the
// model…) sits in it as small chips. What becomes of a message is said in the box itself (工单 #61): sending, sent,
// and why not, never somewhere else.

export type ComposerState = { kind: "idle" } | { kind: "sending" } | { kind: "done"; text: string } | { kind: "error"; text: string; retry?: () => void };

export type PlusItem = { id: string; label: string; icon?: string; run: () => void; disabled?: boolean };

export function Composer({
  value, onChange, onSend, placeholder, state, disabled = false, why, chips, plus, mentions, sendLabel,
}: {
  value: string;
  onChange: (v: string) => void;
  onSend: () => void;
  /** "Message <bot>". */
  placeholder: string;
  state: ComposerState;
  disabled?: boolean;
  /** A line over the box: why it can't send, or what the next message does (a new conversation…). */
  why?: { text: string; tone?: "warn" | "quiet"; action?: { label: string; run: () => void } };
  chips?: ReactNode;
  plus?: PlusItem[];
  /** Rooms: whom it wakes, above the line. */
  mentions?: ReactNode;
  sendLabel?: string;
}) {
  const { t } = useTranslation("common");
  const area = useRef<HTMLTextAreaElement>(null);
  const [menu, setMenu] = useState(false);
  const plusRef = useRef<HTMLDivElement>(null);
  const busy = state.kind === "sending";
  const can = !disabled && !busy && value.trim() !== "";
  // The line grows with what is written, up to a few lines, then scrolls.
  useLayoutEffect(() => {
    const el = area.current;
    if (!el) return;
    el.style.height = "0px";
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, [value]);
  useEffect(() => {
    if (!menu) return;
    const out = (e: MouseEvent) => !plusRef.current?.contains(e.target as Node) && setMenu(false);
    document.addEventListener("mousedown", out);
    return () => document.removeEventListener("mousedown", out);
  }, [menu]);
  const submit = (e?: FormEvent) => {
    e?.preventDefault();
    if (can) onSend();
  };
  return (
    <form className="composer" onSubmit={submit} aria-busy={busy}>
      {why && (
        <div className={`composer-why ${why.tone ?? ""}`} role="note">
          <span>{why.text}</span>
          {why.action && <button type="button" className="link" onClick={why.action.run}>{why.action.label}</button>}
        </div>
      )}
      <div className={`composer-box ${disabled ? "off" : ""}`}>
        {(chips || mentions) && (
          <div className="composer-chips">
            {mentions}
            {chips}
          </div>
        )}
        <div className="composer-line">
          {plus && plus.length > 0 && (
            <div className="composer-plus" ref={plusRef}>
              <button type="button" className="composer-round" aria-label={t("composer.more")} title={t("composer.more")} aria-haspopup="menu" aria-expanded={menu} onClick={() => setMenu(!menu)} disabled={disabled}>
                <Icon name="plus" />
              </button>
              {menu && (
                <div className="vmenu composer-menu" role="menu" aria-label={t("composer.more")}>
                  {plus.map((p) => (
                    <button key={p.id} type="button" className="mi" role="menuitem" disabled={p.disabled} onClick={() => (setMenu(false), p.run())}>
                      {p.icon && <Icon name={p.icon} />}
                      <span>{p.label}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          <textarea
            ref={area}
            className="composer-in"
            rows={1}
            value={value}
            placeholder={placeholder}
            aria-label={placeholder}
            disabled={disabled}
            onChange={(e) => onChange(e.target.value)}
            onKeyDown={(e) => {
              // Return sends, Shift-Return is a new line; never while an input method is composing.
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault();
                submit();
              }
            }}
          />
          <button type="submit" className="composer-round composer-send" disabled={!can} aria-label={sendLabel ?? t("composer.send")} title={sendLabel ?? t("composer.send")}>
            {busy ? <span className="busy" aria-hidden="true"><i /></span> : <Icon name="arrowUp" />}
          </button>
        </div>
      </div>
      {/* What became of the last message, here (工单 #61). */}
      <div className={`composer-state ${state.kind}`} role="status" aria-live="polite">
        {state.kind === "sending" && t("composer.sending")}
        {state.kind === "done" && state.text}
        {state.kind === "error" && (
          <>
            <Icon name="alert" className="sm" />
            <span>{state.text}</span>
            {state.retry && <button type="button" className="link" onClick={state.retry}>{t("composer.retry")}</button>}
          </>
        )}
      </div>
    </form>
  );
}
