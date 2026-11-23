import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Icon } from "./icons";

// The app's own pop-up button (工单 #68): the label of what is chosen with ⌃⌄ beside it, and on a click a menu of the
// choices, the chosen one ticked, as macOS draws one — never the browser's own <select>. The keyboard works as in a
// native menu: arrows move, Return or Space picks, Esc closes back to the button.

export type PickerOption<T extends string | number> = { value: T; label: string; sub?: string; icon?: ReactNode; disabled?: boolean } | null;

export function Picker<T extends string | number>({
  value, options, onChange, label, disabled = false, size = "", className = "", chip = false, placeholder = "",
}: {
  value: T;
  /** null: a separator. */
  options: PickerOption<T>[];
  onChange: (v: T) => void;
  /** What it picks, for screen readers (it shows the choice, not this). */
  label: string;
  disabled?: boolean;
  size?: "" | "sm";
  className?: string;
  /** Drawn as a quiet chip (in a composer), not as a button. */
  chip?: boolean;
  placeholder?: string;
}) {
  const [open, setOpen] = useState<false | "mouse" | "key">(false);
  const btn = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState({ left: -9999, top: 0, minWidth: 0 });
  const chosen = options.find((o) => o && o.value === value);
  useLayoutEffect(() => {
    if (!open || !btn.current || !menu.current) return;
    const r = btn.current.getBoundingClientRect();
    const h = menu.current.offsetHeight;
    const w = Math.max(menu.current.offsetWidth, r.width);
    const below = r.bottom + 4 + h <= innerHeight - 8;
    setAt({ left: Math.max(8, Math.min(r.left, innerWidth - 8 - w)), top: below ? r.bottom + 4 : Math.max(8, r.top - 4 - h), minWidth: r.width });
    const items = [...menu.current.querySelectorAll<HTMLElement>("[role=menuitemradio]:not(:disabled)")];
    (open === "key" ? (items.find((x) => x.getAttribute("aria-checked") === "true") ?? items[0]) : menu.current)?.focus();
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const outside = (e: MouseEvent) => !menu.current?.contains(e.target as Node) && !btn.current?.contains(e.target as Node) && setOpen(false);
    const blur = () => setOpen(false);
    document.addEventListener("mousedown", outside);
    window.addEventListener("blur", blur);
    return () => {
      document.removeEventListener("mousedown", outside);
      window.removeEventListener("blur", blur);
    };
  }, [open]);
  const pick = (v: T) => {
    setOpen(false);
    btn.current?.focus();
    if (v !== value) onChange(v);
  };
  const onKeyDown = (e: ReactKeyboardEvent) => {
    e.stopPropagation();
    const all = [...(menu.current?.querySelectorAll<HTMLElement>("[role=menuitemradio]:not(:disabled)") ?? [])];
    const i = all.indexOf(document.activeElement as HTMLElement);
    const go = { ArrowDown: (i + 1) % all.length, ArrowUp: i <= 0 ? all.length - 1 : i - 1, Home: 0, End: all.length - 1 }[e.key];
    if (go !== undefined) {
      e.preventDefault();
      all[go]?.focus();
    } else if (e.key === "Escape" || e.key === "Tab") {
      e.preventDefault();
      setOpen(false);
      btn.current?.focus();
    }
  };
  return (
    <>
      <button
        type="button"
        ref={btn}
        className={`picker ${chip ? "chip" : ""} ${size} ${open ? "on" : ""} ${className}`}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={!!open}
        disabled={disabled}
        title={chosen?.label}
        onClick={(e) => setOpen(open ? false : e.detail ? "mouse" : "key")}
        onKeyDown={(e) => (e.key === "ArrowDown" || e.key === "ArrowUp") && !open && (e.preventDefault(), setOpen("key"))}
      >
        {chosen?.icon}
        <span className="picker-v">{chosen?.label ?? placeholder}</span>
        <Icon name={chip ? "chevD" : "chevUD"} className="sm picker-c" />
      </button>
      {open &&
        createPortal(
          <div className="vmenu picker-menu" role="menu" aria-label={label} tabIndex={-1} ref={menu} style={at} onKeyDown={onKeyDown}>
            {options.map((o, i) =>
              o ? (
                <button
                  key={String(o.value)}
                  type="button"
                  className="mi"
                  role="menuitemradio"
                  aria-checked={o.value === value}
                  tabIndex={-1}
                  disabled={o.disabled}
                  onMouseMove={(e) => e.currentTarget.focus()}
                  onClick={() => pick(o.value)}
                >
                  <span className="ck">{o.value === value && <Icon name="check" className="sm" />}</span>
                  {o.icon}
                  <span className="picker-l">
                    {o.label}
                    {o.sub && <small>{o.sub}</small>}
                  </span>
                </button>
              ) : (
                <div key={`sep${i}`} className="msep" role="separator" />
              ),
            )}
          </div>,
          document.body,
        )}
    </>
  );
}

/** A tick box the app's own way (工单 #68): a round check, as in Apple's lists, never the browser's checkbox. */
export function Check({ on, label, set, disabled }: { on: boolean; label: string; set: (on: boolean) => void; disabled?: boolean }) {
  return (
    <button type="button" className={`chk ${on ? "on" : ""}`} role="checkbox" aria-checked={on} aria-label={label} disabled={disabled} onClick={() => set(!on)}>
      {on && <Icon name="check" className="sm" />}
    </button>
  );
}
