import { createContext, useContext, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { AttachmentInfo } from "@aide/proto/session";
import { Icon } from "../icons";
import { sizeText } from "@aide/core";
import { DraftContext, useContentSession } from "./scope";
import { usePicture, type Picture as Pic } from "./sentFiles";
import { asWritten, isLocal, type ShownPictures } from "./pictures";

export { firstPictures, isLocal, shownPictures } from "./pictures";

// The pictures an agent's reply shows (工单 #158, after hermesh's session images): ![alt](a file on the machine) is
// drawn where it is written, once a reply's block is settled; the same picture twice in a reply is drawn once, the
// second a chip that opens it; a click opens it large over a blurred page. A web address or inline data is never
// loaded (the alt text stays, as before).

/** By the seq of each reply event of a turn: the pictures the turn kept (shownPictures). */
export const PicturesContext = createContext<ShownPictures>(new Map());

/** The pictures the reply being drawn has kept, by path; and of each picture (by SHA-256, else path), where in the reply it first is. */
type Reply = { kept: Map<string, AttachmentInfo> | undefined; first: Map<string, number> };
export const ReplyContext = createContext<Reply>({ kept: undefined, first: new Map() });

/** An image of a reply, at its place: at is where it starts in the reply. */
export function ReplyPicture({ src: given, alt, at }: { src: string; alt: string; at: number | undefined }) {
  const { t } = useTranslation("session");
  const sessionId = useContentSession();
  const draft = useContext(DraftContext);
  const { kept, first } = useContext(ReplyContext);
  const src = asWritten(given);
  const info = kept?.get(src);
  const local = sessionId !== "" && isLocal(src);
  const pic = usePicture(sessionId, info?.sha256 ?? "", info ? "" : src, !local || draft);
  const [open, setOpen] = useState(false);
  if (!local) return <span>{alt}</span>;
  if (draft) return <span className="rp-wait" aria-busy="true">{alt}</span>;
  const key = info?.sha256 || src;
  const name = info?.name || (typeof pic === "object" ? pic.name : "") || src.split(/[\\/]/).pop() || src;
  const again = at !== undefined && first.has(key) && first.get(key) !== at;
  if (pic === "gone") return (
      <span className="rp-gone" title={t("picture.gone")}>
        <Icon name="image" className="sm" />
        {alt || name}
      </span>
    );
  if (again) {
    return (
      <>
        <button type="button" className="rp-chip" onClick={() => setOpen(true)} disabled={pic === "loading"}>
          <Icon name="image" className="sm" />
          {alt || name}
        </button>
        {open && typeof pic === "object" && <Lightbox pic={pic} path={src} onClose={() => setOpen(false)} />}
      </>
    );
  }
  return (
    <span className="rp">
      {pic === "loading" ? (
        <span className="rp-wait" aria-busy="true">{alt}</span>
      ) : (
        <button type="button" className="rp-img" onClick={() => setOpen(true)} aria-label={t("picture.open", { name: alt || name })}>
          <img src={pic.url} alt={alt} />
        </button>
      )}
      {open && typeof pic === "object" && <Lightbox pic={pic} path={src} onClose={() => setOpen(false)} />}
    </span>
  );
}

/** A picture large over the page, the page blurred behind it: its name and size above, saving and copying its path, closing (Esc, a click beside it). A double click zooms. */
export function Lightbox({ pic, path, onClose }: { pic: Pic; path?: string; onClose: () => void }) {
  const { t } = useTranslation("session");
  const [zoom, setZoom] = useState(false);
  const [dims, setDims] = useState("");
  const close = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    close.current?.focus();
    const key = (e: KeyboardEvent) => e.key === "Escape" && (e.stopPropagation(), onClose());
    window.addEventListener("keydown", key, true);
    return () => {
      window.removeEventListener("keydown", key, true);
      before?.focus();
    };
  }, [onClose]);
  return createPortal(
    <div className="lightbox" role="dialog" aria-modal="true" aria-label={pic.name} onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div className="lb-bar">
        <div className="lb-title">
          <b>{pic.name}</b>
          <span>{[dims, sizeText(pic.size)].filter(Boolean).join(" · ")}</span>
        </div>
        <div className="lb-acts">
          <a className="btn sm" href={pic.url} download={pic.name}>{t("picture.save")}</a>
          {path && <button type="button" className="btn sm" onClick={() => navigator.clipboard.writeText(path).catch(() => {})}>{t("picture.copyPath")}</button>}
          <button type="button" className="tb" ref={close} onClick={onClose} aria-label={t("markdown.close")}><Icon name="x" /></button>
        </div>
      </div>
      <img
        className={zoom ? "lb-img zoom" : "lb-img"}
        src={pic.url}
        alt={pic.name}
        onLoad={(e) => setDims(`${e.currentTarget.naturalWidth}×${e.currentTarget.naturalHeight}`)}
        onDoubleClick={() => setZoom((z) => !z)}
      />
    </div>,
    document.body,
  );
}
