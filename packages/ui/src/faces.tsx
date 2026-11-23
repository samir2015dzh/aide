import faces from "./faces.json" with { type: "json" };

/** What a face is of (工单 #100): the person whose account it is, or a device by what it reports (core deviceKind). */
export type FaceKind = keyof typeof faces.faces;

/**
 * The account's or a device's face, drawn from faces.json (the same paths Android draws): a white line drawing,
 * 1.5px whatever the size, on its colour, round for the person, an app icon's tile for a device.
 */
export function DeviceFace({ kind, size, label }: { kind: FaceKind; size: number; label?: string }) {
  const f = faces.faces[kind] ?? faces.faces.device;
  const round = "round" in f && f.round;
  const inset = (24 * (1 - faces.glyph)) / 2;
  return (
    <svg
      className="dface"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      data-face={kind}
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      style={{ display: "block", flex: "none" }}
    >
      <rect width="24" height="24" rx={round ? 12 : 24 * faces.corner} fill={f.color} />
      <g transform={`translate(${inset} ${inset}) scale(${faces.glyph})`} fill="none" stroke="#fff" strokeWidth={faces.stroke} strokeLinecap="round" strokeLinejoin="round">
        {f.paths.map((d) => (
          <path key={d} d={d} vectorEffect="non-scaling-stroke" />
        ))}
      </g>
    </svg>
  );
}
