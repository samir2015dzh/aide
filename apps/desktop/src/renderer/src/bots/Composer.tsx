import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ClipboardEvent, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { admitAttachments, attachmentKind, attachmentNote, freshFiles, pasteAttaches, sizeText, type AttachmentKind } from "@aide/core";
import { Icon } from "../icons";
import "./composer.css";

// The box to write in (工单 #92, after Claude's input box; one for a session's page, a bot's page, a personal agent's
// conversation and a room): a rounded box with the line to write in, + at its foot on the left for what goes with the
// message (images, files, audio — 工单 #91 — and the page's own items), the microphone and the send button on the
// right; under it, on the right, the choices the message goes with (project, model and effort, mode) and, on the left,
// what became of the last message (工单 #61): sending, sent, and why not, never somewhere else. Typing / opens the
// page's commands when it has some (the placeholder says so only then).

export type ComposerState = { kind: "idle" } | { kind: "sending" } | { kind: "done"; text: string } | { kind: "error"; text: string; retry?: () => void };

export type PlusItem = { id: string; label: string; icon?: string; run: () => void; disabled?: boolean };

/** A command typed after /: its name (what is typed) and what it does. */
export type SlashCommand = { id: string; name: string; label: string; run: () => void };

/** A file about to go with the message. */
export type Draft = { id: string; file: File; kind: AttachmentKind; url?: string };

/** The files about to go with a message (useAttachments): the page sends them and clears them once sent. */
export type Attachments = {
  files: Draft[];
  add: (files: File[]) => void;
  remove: (id: string) => void;
  clear: () => void;
  /** Why some were not added, in words naming their kind (shown in the box until the next change). */
  refused: string[];
};

export function useAttachments(): Attachments {
  const [files, setFiles] = useState<Draft[]>([]);
  const [refused, setRefused] = useState<string[]>([]);
  const live = useRef(files);
  live.current = files;
  useEffect(() => () => live.current.forEach((d) => d.url && URL.revokeObjectURL(d.url)), []);
  const add = useCallback((more: File[]) => {
    const pending = (fs: File[]) => fs.map((file) => ({ file, name: file.name || "file", type: file.type, size: file.size }));
    // The same file pasted or dropped twice goes once (工单 #143).
    const have = pending(live.current.map((d) => d.file));
    const { take, refused } = admitAttachments(have, freshFiles(have, pending(more)));
    const drafts = take.map(({ file }) => {
      const kind = attachmentKind({ name: file.name, type: file.type });
      return { id: crypto.randomUUID(), file, kind, url: kind === "image" ? URL.createObjectURL(file) : undefined };
    });
    setFiles((x) => [...x, ...drafts]);
    setRefused(refused);
  }, []);
  const remove = useCallback((id: string) => {
    setFiles((x) => {
      const gone = x.find((d) => d.id === id);
      if (gone?.url) URL.revokeObjectURL(gone.url);
      return x.filter((d) => d.id !== id);
    });
    setRefused([]);
  }, []);
  const clear = useCallback(() => {
    live.current.forEach((d) => d.url && URL.revokeObjectURL(d.url));
    setFiles([]);
    setRefused([]);
  }, []);
  return { files, add, remove, clear, refused };
}

/** The files as a message carries them (aide.session.v1.Attachment). */
export async function attachmentsOf(files: Draft[]): Promise<{ name: string; mediaType: string; data: Uint8Array }[]> {
  return Promise.all(files.map(async (d) => ({ name: d.file.name || "file", mediaType: d.file.type, data: new Uint8Array(await d.file.arrayBuffer()) })));
}

/** How this computer turns speech into text in a text field: macOS's dictation, Windows' voice typing (Win+H), or none. */
const dictation: "mac" | "win" | "" = window.aide?.platform === "darwin" ? "mac" : window.aide?.platform === "win32" ? "win" : "";

export function Composer({
  value, onChange, onSend, placeholder, state, disabled = false, why, chips, plus, mentions, sendLabel, attach, commands,
}: {
  value: string;
  onChange: (v: string) => void;
  onSend: () => void;
  /** "Message <bot>": the placeholder, unless the page has commands (then it is the box's name). */
  placeholder: string;
  state: ComposerState;
  disabled?: boolean;
  /** A line over the box: why it can't send, or what the next message does (a new conversation…). */
  why?: { text: string; tone?: "warn" | "quiet"; action?: { label: string; run: () => void } };
  /** The choices the message goes with, under the box on the right (project, model and effort, mode…). */
  chips?: ReactNode;
  plus?: PlusItem[];
  /** Rooms: whom it wakes, above the line. */
  mentions?: ReactNode;
  sendLabel?: string;
  /** Files can go with the message: + offers images, files and audio; they can be pasted and dropped too. */
  attach?: Attachments;
  /** What typing / offers. */
  commands?: SlashCommand[];
}) {
  const { t } = useTranslation("common");
  const area = useRef<HTMLTextAreaElement>(null);
  const [menu, setMenu] = useState(false);
  const [over, setOver] = useState(false);
  const [hint, setHint] = useState("");
  const plusRef = useRef<HTMLDivElement>(null);
  const pick = useRef<Record<AttachmentKind, HTMLInputElement | null>>({ image: null, audio: null, file: null });
  const busy = state.kind === "sending";
  const files = attach?.files ?? [];
  const can = !disabled && !busy && (value.trim() !== "" || files.length > 0);
  // The commands what is typed so far names (/ and the start of one, nothing after it).
  const slash = /^\/(\S*)$/.exec(value);
  const shown = useMemo(
    () => (slash && commands ? commands.filter((c) => c.name.startsWith(slash[1].toLowerCase()) || c.label.toLowerCase().includes(slash[1].toLowerCase())) : []),
    [slash?.[1], commands], // eslint-disable-line react-hooks/exhaustive-deps
  );
  const [chosen, setChosen] = useState(0);
  useEffect(() => setChosen(0), [shown.length]);
  // The line grows with what is written, up to a few lines, then scrolls.
  useLayoutEffect(() => {
    const el = area.current;
    if (!el) return;
    el.style.height = "0px";
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
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
  const runCommand = (c: SlashCommand) => {
    onChange("");
    c.run();
    area.current?.focus();
  };
  const listen = () => {
    area.current?.focus();
    if (dictation === "mac") window.aide?.startDictation();
    setHint(t(dictation === "mac" ? "composer.dictationMac" : "composer.dictationWin"));
  };
  // A paste attaches a screenshot, a picture or files copied; words copied with a picture of them (a spreadsheet's
  // cells) paste as the words (工单 #143).
  const paste = (e: ClipboardEvent) => {
    const fs = [...e.clipboardData.files];
    if (!attach || !pasteAttaches(e.clipboardData.getData("text/plain"), fs.length, fs.map((f) => window.aide?.pathForFile(f) ?? ""))) return;
    e.preventDefault();
    attach.add(fs);
  };
  // Files dragged anywhere over the window come to this box (工单 #143): it says it takes them while they are over,
  // and takes them where they are let go.
  const take = useRef<((fs: File[]) => void) | null>(null);
  take.current = attach && !disabled ? (fs) => (attach.add(fs), area.current?.focus()) : null;
  useEffect(() => {
    const files = (e: DragEvent) => !!take.current && !!e.dataTransfer?.types.includes("Files");
    const over = (e: DragEvent) => {
      if (!files(e)) return;
      e.preventDefault();
      setOver(true);
    };
    const leave = (e: DragEvent) => e.relatedTarget === null && setOver(false);
    const drop = (e: DragEvent) => {
      setOver(false);
      if (!files(e) || !e.dataTransfer?.files.length) return;
      e.preventDefault();
      take.current?.([...e.dataTransfer.files]);
    };
    window.addEventListener("dragover", over);
    window.addEventListener("dragleave", leave);
    window.addEventListener("drop", drop);
    return () => {
      window.removeEventListener("dragover", over);
      window.removeEventListener("dragleave", leave);
      window.removeEventListener("drop", drop);
    };
  }, []);
  const items: PlusItem[] = [
    ...(attach
      ? (["image", "file", "audio"] as const).map((k) => ({ id: `attach-${k}`, label: t(`composer.attach.${k}`), icon: k === "image" ? "image" : k === "audio" ? "audio" : "doc", run: () => pick.current[k]?.click() }))
      : []),
    ...(plus ?? []),
  ];
  const placeholderText = commands?.length ? t("composer.slashPlaceholder") : placeholder;
  return (
    <form className="composer" onSubmit={submit} aria-busy={busy}>
      {why && (
        <div className={`composer-why ${why.tone ?? ""}`} role="note">
          <span>{why.text}</span>
          {why.action && <button type="button" className="link" onClick={why.action.run}>{why.action.label}</button>}
        </div>
      )}
      <div className={`composer-box ${disabled ? "off" : ""} ${over ? "over" : ""}`}>
        {over && (
          <div className="composer-drop" aria-hidden="true">
            <b>{t("composer.dropTitle")}</b>
            <span>{t("composer.dropHint")}</span>
          </div>
        )}
        {shown.length > 0 && (
          <div className="vmenu composer-cmds" role="listbox" aria-label={t("composer.commands")}>
            {shown.map((c, i) => (
              <button key={c.id} type="button" role="option" aria-selected={i === chosen} className={`mi ${i === chosen ? "on" : ""}`} onMouseDown={(e) => e.preventDefault()} onClick={() => runCommand(c)}>
                <b>/{c.name}</b>
                <span>{c.label}</span>
              </button>
            ))}
          </div>
        )}
        {mentions && <div className="composer-chips">{mentions}</div>}
        {files.length > 0 && (
          <ul className="composer-files" aria-label={t("composer.attached")}>
            {files.map((d) => {
              const note = attachmentNote({ name: d.file.name, type: d.file.type, size: d.file.size });
              return (
                <li key={d.id} className={`composer-file ${d.url ? "img" : ""}`} title={[d.file.name, note].filter(Boolean).join("\n")}>
                  {d.url ? <img src={d.url} alt={d.file.name} /> : <Icon name={d.kind === "audio" ? "audio" : "doc"} />}
                  {!d.url && (
                    <span className="composer-file-t">
                      <b>{d.file.name}</b>
                      <span>{t(`composer.attach.${d.kind}`)} · {sizeText(d.file.size)}</span>
                    </span>
                  )}
                  <button type="button" className="composer-file-x" aria-label={t("composer.removeFile", { name: d.file.name })} title={t("composer.removeFile", { name: d.file.name })} onClick={() => attach?.remove(d.id)}>
                    <Icon name="x" />
                  </button>
                </li>
              );
            })}
          </ul>
        )}
        <textarea
          ref={area}
          className="composer-in"
          rows={1}
          value={value}
          placeholder={placeholderText}
          aria-label={placeholder}
          disabled={disabled}
          onChange={(e) => (onChange(e.target.value), hint && setHint(""))}
          onPaste={paste}
          onKeyDown={(e) => {
            if (shown.length > 0) {
              if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                e.preventDefault();
                setChosen((chosen + (e.key === "ArrowDown" ? 1 : shown.length - 1)) % shown.length);
                return;
              }
              if ((e.key === "Enter" || e.key === "Tab") && !e.nativeEvent.isComposing) {
                e.preventDefault();
                runCommand(shown[chosen]);
                return;
              }
              if (e.key === "Escape") {
                e.preventDefault();
                onChange("");
                return;
              }
            }
            // Return sends, Shift-Return is a new line; never while an input method is composing.
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              submit();
            }
          }}
        />
        <div className="composer-bar">
          {items.length > 0 && (
            <div className="composer-plus" ref={plusRef}>
              <button type="button" className="composer-round" aria-label={t("composer.more")} title={t("composer.more")} aria-haspopup="menu" aria-expanded={menu} onClick={() => setMenu(!menu)} disabled={disabled}>
                <Icon name="plus" />
              </button>
              {menu && (
                <div className="vmenu composer-menu" role="menu" aria-label={t("composer.more")}>
                  {items.map((p) => (
                    <button key={p.id} type="button" className="mi" role="menuitem" disabled={p.disabled} onClick={() => (setMenu(false), p.run())}>
                      {p.icon && <Icon name={p.icon} />}
                      <span>{p.label}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          {attach &&
            (["image", "file", "audio"] as const).map((k) => (
              <input
                key={k}
                ref={(el) => void (pick.current[k] = el)}
                type="file"
                multiple
                hidden
                accept={k === "image" ? "image/*" : k === "audio" ? "audio/*" : undefined}
                data-attach={k}
                onChange={(e) => (attach.add([...(e.target.files ?? [])]), (e.target.value = ""))}
              />
            ))}
          <span className="composer-gap" />
          {dictation && (
            <button type="button" className="composer-round composer-mic" aria-label={t("composer.dictate")} title={t("composer.dictate")} onClick={listen} disabled={disabled}>
              <Icon name="mic" />
            </button>
          )}
          <button type="submit" className="composer-round composer-send" disabled={!can} aria-label={sendLabel ?? t("composer.send")} title={sendLabel ?? t("composer.send")}>
            {busy ? <span className="busy" aria-hidden="true"><i /></span> : <Icon name="arrowUp" />}
          </button>
        </div>
      </div>
      <div className="composer-foot">
        {/* What became of the last message, here (工单 #61); what was not added and why (工单 #91); how to dictate. */}
        <div className={`composer-state ${attach?.refused.length ? "error" : hint ? "idle" : state.kind}`} role="status" aria-live="polite">
          {attach?.refused.length ? (
            <>
              <Icon name="alert" className="sm" />
              {attach.refused.map((r) => <span key={r}>{r}</span>)}
            </>
          ) : hint ? (
            <span>{hint}</span>
          ) : (
            <>
              {state.kind === "sending" && t("composer.sending")}
              {state.kind === "done" && state.text}
              {state.kind === "error" && (
                <>
                  <Icon name="alert" className="sm" />
                  <span>{state.text}</span>
                  {state.retry && <button type="button" className="link" onClick={state.retry}>{t("composer.retry")}</button>}
                </>
              )}
            </>
          )}
        </div>
        {chips && <div className="composer-params">{chips}</div>}
      </div>
    </form>
  );
}
