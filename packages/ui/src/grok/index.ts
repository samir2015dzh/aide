// The bots' animated character (工单 #83), ported from BotArk (MIT, ./LICENSE): its own entry, so only the windows
// that draw bots load the engine.
export { BotAvatarEditor, type BotAvatarEditorProps, type BotLookPick } from "./BotAvatarEditor";
export {
  BOT_COLORS,
  BOT_SHAPES,
  GrokBotMark,
  getBotColorHex,
  resolveBotColor,
  resolveBotShape,
  type BotColorId,
  type BotShape,
  type GrokBotMarkProps,
  type GrokStatus,
} from "./GrokBotMark";
export { GROK_STATES } from "./grokMotionEngine";
