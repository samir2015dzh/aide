import { agentLogos } from "./agent-logo-data";
import { agentLogoImages } from "./agent-logo-images";

/** A coding agent's logo in a size×size box; a monogram tile for agents without one. */
export function AgentLogo({ agent, size = 16 }: { agent: string; size?: number }) {
  const picture = agentLogoImages[agent];
  if (picture) return <img src={picture} width={size} height={size} alt="" aria-hidden="true" style={{ flex: "none", display: "block" }} />;
  const logo = agentLogos[agent];
  if (!logo) {
    return (
      <span
        aria-hidden="true"
        style={{ width: size, height: size, borderRadius: size * 0.25, background: "#8E8E93", display: "inline-flex", alignItems: "center", justifyContent: "center", flex: "none" }}
      >
        <BotGlyph size={Math.round(size * 0.74)} />
      </span>
    );
  }
  return (
    <svg width={size} height={size} viewBox={logo.viewBox} aria-hidden="true" style={{ flex: "none", display: "block" }}>
      {logo.layers.map((l, i) => (
        <path
          key={i}
          d={l.d}
          fill={l.fill}
          fillOpacity={l.opacity}
          fillRule={logo.evenOdd ? "evenodd" : undefined}
          stroke={logo.stroke ? l.fill : undefined}
          strokeWidth={logo.stroke || undefined}
          strokeLinejoin={logo.stroke ? "round" : undefined}
        />
      ))}
    </svg>
  );
}

/**
 * A coding or personal agent's logo on a tile, the one way it is shown as an
 * icon: a white rounded tile with the logo inside, or a logo that is already
 * an app icon at the tile's size.
 */
export function AgentTile({ agent, size = 32 }: { agent: string; size?: number }) {
  if (agentLogoImages[agent]) return <AgentLogo agent={agent} size={size} />;
  return (
    <span
      aria-hidden="true"
      style={{
        width: size, height: size, borderRadius: size * 0.28, background: "#fff", color: "#000", flex: "none",
        boxShadow: "0 0 0 .5px rgba(0,0,0,.1), 0 1px 2px rgba(0,0,0,.08)", display: "inline-flex", alignItems: "center", justifyContent: "center",
      }}
    >
      <AgentLogo agent={agent} size={Math.round(size * 0.58)} />
    </span>
  );
}

/**
 * A bot's own face where its app has no icon (工单 #62): a small robot, drawn in white on whatever its avatar's
 * colour is. Never a letter: initials are no face.
 */
export function BotGlyph({ size, color = "#fff" }: { size: number; color?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" style={{ flex: "none", display: "block" }}>
      <g fill={color}>
        <circle cx="12" cy="2.9" r="1.35" />
        <rect x="11.3" y="3.6" width="1.4" height="3" rx=".6" />
        <rect x="2.4" y="10.3" width="2.3" height="4.6" rx="1.15" />
        <rect x="19.3" y="10.3" width="2.3" height="4.6" rx="1.15" />
        <path
          fillRule="evenodd"
          d="M8.6 6.2h6.8a4.6 4.6 0 0 1 4.6 4.6v2.8a4.6 4.6 0 0 1-4.6 4.6H8.6A4.6 4.6 0 0 1 4 13.6v-2.8a4.6 4.6 0 0 1 4.6-4.6Zm.9 4.2a1.7 1.7 0 1 0 0 3.4 1.7 1.7 0 0 0 0-3.4Zm5 0a1.7 1.7 0 1 0 0 3.4 1.7 1.7 0 0 0 0-3.4Zm-4.6 5.1a.6.6 0 0 0 0 1.2h4.2a.6.6 0 0 0 0-1.2H9.9Z"
        />
      </g>
    </svg>
  );
}

/** A coding or personal agent's app icon as a picture, when its logo is one (an avatar draws it edge to edge). */
export function agentPicture(agent: string): string | undefined {
  return agentLogoImages[agent];
}
