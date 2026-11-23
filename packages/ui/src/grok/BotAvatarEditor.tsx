import { useState, useSyncExternalStore } from 'react'
import {
  BOT_COLORS,
  BOT_SHAPES,
  GrokBotMark,
  getBotColorHex,
  resolveBotColor,
  resolveBotShape,
  type BotColorId,
  type BotShape
} from './GrokBotMark'

// Ported from BotArk (MIT, see ./LICENSE): src/renderer/src/components/ui/BotAvatarEditorDialog.tsx, its Bot tab.
// Aide's changes (工单 #83): a panel that a sheet holds (Aide's own contact and look sheets are the dialog), the look
// handed back through onChange in place of BotArk's bot store, the words passed in (Aide's i18n), plain class names in
// place of Tailwind; no Photo tab (a contact keeps no picture, and a picture shows no state: the character is the
// avatar), no Muse identity import.

export type BotLookPick = { shape: BotShape; color: BotColorId }

export interface BotAvatarEditorProps {
  seed: string
  shape?: string | null
  color?: string | null
  onChange: (look: BotLookPick) => void
  /** The words: the groups' names, the stage's tooltip, each shape's and colour's name. */
  words: { shapes: string; colors: string; poke: string; shape: (s: BotShape) => string; color: (c: BotColorId) => string }
}

const darkQuery = '(prefers-color-scheme: dark)'
function subscribeDark(f: () => void) {
  const m = window.matchMedia(darkQuery)
  m.addEventListener('change', f)
  return () => m.removeEventListener('change', f)
}

/**
 * Mirrors Grok Bot's `AgentAvatarEditor` (`Bot` tab):
 * live interactive GrokBotMark preview + 26 shapes grid (5 per row) + 11 colors row
 */
export function BotAvatarEditor({ seed, shape, color, onChange, words }: BotAvatarEditorProps) {
  const [poke, setPoke] = useState(0)
  const currentShape = resolveBotShape(seed, shape)
  const currentColor = resolveBotColor(seed, color)
  const isDark = useSyncExternalStore(subscribeDark, () => window.matchMedia(darkQuery).matches, () => false)

  const selectShape = (s: BotShape) => {
    onChange({ shape: s, color: currentColor })
    setPoke((n) => n + 1)
  }

  const selectColor = (c: BotColorId) => {
    onChange({ shape: currentShape, color: c })
    setPoke((n) => n + 1)
  }

  return (
    <div className="gbe">
      {/* Live Interactive Character Stage */}
      <button type="button" onClick={() => setPoke((n) => n + 1)} title={words.poke} aria-label={words.poke} className="gbe-stage">
        <GrokBotMark seed={seed} shape={currentShape} color={currentColor} sizePx={72} followPointer pokeSignal={poke} />
      </button>

      {/* 26 Shapes Grid (5 columns like Grok Bot) */}
      <div role="group" aria-label={words.shapes} className="gbe-grid">
        {BOT_SHAPES.map((s) => {
          const selected = currentShape === s
          return (
            <button
              key={s}
              type="button"
              title={words.shape(s)}
              aria-label={words.shape(s)}
              aria-pressed={selected}
              data-shape={s}
              onClick={() => selectShape(s)}
              className={`gbe-cell${selected ? ' on' : ''}`}
            >
              <GrokBotMark seed={seed} shape={s} color={currentColor} sizePx={36} isPaused />
            </button>
          )
        })}
      </div>

      {/* 11 Colors Palette */}
      <div role="group" aria-label={words.colors} className="gbe-colors">
        {BOT_COLORS.map((c) => {
          const selected = currentColor === c.id
          return (
            <button
              key={c.id}
              type="button"
              title={words.color(c.id)}
              aria-label={words.color(c.id)}
              aria-pressed={selected}
              data-color={c.id}
              onClick={() => selectColor(c.id)}
              className={`gbe-swatch${selected ? ' on' : ''}`}
            >
              <span style={{ backgroundColor: getBotColorHex(c.id, isDark) }} />
            </button>
          )
        })}
      </div>
    </div>
  )
}
