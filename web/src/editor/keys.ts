/**
 * Shortcut labels as the person's keyboard reads them. The labels are written
 * the macOS way (⌘Z, ⇧⌘]); off macOS they read Ctrl+Z, Ctrl+Shift+]. The
 * shortcuts themselves take ⌘ or Ctrl either way (editor/keyboard.ts).
 */

export const IS_MAC = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)

const NAMES: Record<string, string> = { '⌘': 'Ctrl', '⌥': 'Alt', '⇧': 'Shift' }
/** Windows order: Ctrl+Alt+Shift+key. */
const ORDER = ['⌘', '⌥', '⇧']

/**
 * "⇧⌘Z" → "Ctrl+Shift+Z", "按住 ⇧ 或 ⌘" → "按住 Shift 或 Ctrl", "⌘ + 滚轮" →
 * "Ctrl + 滚轮" — unchanged on macOS.
 */
export function keys(label: string): string {
  if (IS_MAC) return label
  return label.replace(/([⇧⌥⌘]+)([^\s/⇧⌥⌘]*)/g, (_m, mods: string, key: string) =>
    [...ORDER.filter((m) => mods.includes(m)).map((m) => NAMES[m]), ...(key ? [key] : [])].join('+'),
  )
}
