import type { CSSProperties } from "react";

// The Aide mark, "副官星" (the aide's star): the master is the dot in the
// middle, the ring its orbit, and the aide the small star in the ring's
// gap, going round the master, ready to carry the word. One colour, the
// ink of the page (Grok-like black on light, white on dark). The geometry
// is the icon's (the handover's design/aide/icons/final-g3, 256 units, its
// ink centred), so the app's mark and its icon are one shape; animations
// always settle on it. Motion is in brand.css (.hm-<variant>).
const CENTRE = { x: 126, y: 130 };
const RING = "M203.372 106.031A81 81 0 1 1 149.969 52.628A40 40 0 0 0 146.264 74.589A59 59 0 1 0 181.411 109.736A40 40 0 0 0 203.372 106.031Z";
const CORE = { ...CENTRE, r: 22 };
const SAT = { x: 186, y: 70, r: 22 };
// The ink's box (x 45..208, y 48..211), square.
const BOX = { x: 45, y: 48, size: 163 };
const viewBox = (pad: number) => `${BOX.x - pad} ${BOX.y - pad} ${BOX.size + 2 * pad} ${BOX.size + 2 * pad}`;

/**
 * The mark in a size×size box. variant: "reveal" draws it in (the ring
 * sweeps round, the master, then the aide arrives); "seq" turns the ring
 * with the aide in its gap (busy, small sizes); "pulse" breathes the master
 * while the aide orbits (loading); "wait" lifts the aide out of its gap and
 * breathes (waiting for the other side), "lock" settles it back (done),
 * "fail" lets it drift off, grey. mono is the current colour (the mark is
 * one colour either way; mono keeps it from the page's ink). pad (viewBox
 * units) leaves room around the mark, for the waiting motion.
 */
export function Mark({ size = 20, variant, mono, pad = 0, className }: { size?: number; variant?: "reveal" | "seq" | "pulse" | "wait" | "lock" | "fail"; mono?: boolean; pad?: number; className?: string }) {
  const cls = ["hm-mark", variant && `hm-${variant}`, mono && "hm-mono", className].filter(Boolean).join(" ");
  const origin = { transformOrigin: `${CENTRE.x}px ${CENTRE.y}px`, transformBox: "view-box" } as CSSProperties;
  return (
    <svg viewBox={viewBox(pad)} width={size} height={size} aria-hidden="true" className={cls}>
      <g className="hm-orbit" style={origin}>
        <path d={RING} className="hm-ring" fill="currentColor" style={origin} />
        <circle cx={SAT.x} cy={SAT.y} r={SAT.r} className="hm-sat" fill="currentColor" />
      </g>
      <circle cx={CORE.x} cy={CORE.y} r={CORE.r} className="hm-core" fill="currentColor" style={origin} />
    </svg>
  );
}
