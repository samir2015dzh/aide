import "./cat.css";

// Her's cat (the avatar her calls "mark"), on a 120 grid of which the box 8 16 104 104 shows: a round body with
// two ears behind it, in one ink, and the eyes and mouth cut out of it, after Grok's look (the owner's ruling,
// 2026-11-19): black on light, white on dark, no shading, no colour. Small, her body, ears and larger eyes alone.
// She breathes and blinks; busy, she looks around and bobs (cat.css).
const catBody = "M60 30C84 30 100 48 103 72C106 96 92 110 60 110C28 110 14 96 17 72C20 48 36 30 60 30Z";
const catEar = "M30 53L26 25L50 37Z";

export function Cat({ small, busy = false }: { small: boolean; busy?: boolean }) {
  const ear = <path d={catEar} fill="var(--cat-ink)" stroke="var(--cat-ink)" strokeWidth={8} strokeLinejoin="round" />;
  const eye = (x: number) => (
    <g className="eye">
      <ellipse cx={x} cy={66} rx={small ? 6 : 5} ry={small ? 7.4 : 6.4} fill="var(--cat-cut)" />
    </g>
  );
  const apart = small ? 13 : 12.5;
  return (
    <svg className={`cat ${busy ? "busy" : ""}`} viewBox="8 16 104 104" aria-hidden="true">
      <g className="cat-body">
        {ear}
        <g transform="translate(120 0) scale(-1 1)">{ear}</g>
        <path d={catBody} fill="var(--cat-ink)" />
        <g className="cat-eyes">
          {eye(60 - apart)}
          {eye(60 + apart)}
        </g>
        {!small && <path d="M55 80q2.5 3.4 5 0q2.5 3.4 5 0" fill="none" stroke="var(--cat-cut)" strokeWidth={2.6} strokeLinecap="round" strokeLinejoin="round" />}
      </g>
    </svg>
  );
}
