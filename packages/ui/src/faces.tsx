import faces from "./faces.json" with { type: "json" };

/** 账号与设备头像类别（工单 #100 / #109）：person 或 core deviceKind 上报的设备类型。 */
export type FaceKind = keyof typeof faces.faces;

/**
 * 账号与设备头像组件，直接读取 faces.json（与安卓 AccountFaces.kt 同源）：
 * 1.5px 恒定宽度白色线性描边 + 非状态色底块；账号为圆形，设备为圆角方块（圆角 = floor(size * 0.3)）。
 */
export function DeviceFace({ kind, size, label }: { kind: FaceKind; size: number; label?: string }) {
  const f = faces.faces[kind] ?? faces.faces.device;
  const round = "round" in f && f.round;
  const inset = (24 * (1 - faces.glyph)) / 2;
  const rx = round ? 12 : size > 0 ? (Math.floor(size * faces.corner) * 24) / size : 24 * faces.corner;
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
      <rect width="24" height="24" rx={rx} fill={f.color} />
      <rect className="dface-rim" x="0.5" y="0.5" width="23" height="23" rx={Math.max(0, rx - 0.5)} fill="none" />
      <g transform={`translate(${inset} ${inset}) scale(${faces.glyph})`} fill="none" stroke="#fff" strokeWidth={faces.stroke} strokeLinecap="round" strokeLinejoin="round">
        {f.paths.map((d) => (
          <path key={d} d={d} vectorEffect="non-scaling-stroke" />
        ))}
      </g>
    </svg>
  );
}
