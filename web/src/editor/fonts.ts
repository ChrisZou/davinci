import { api } from '../api'

/**
 * The font list for the panels. The canvas gets its font files from the
 * server (/api/fonts/face); the browser needs them only for the text editing
 * box, where macOS system fonts are visible by name and the user's own fonts
 * (`davinci font add`) get an @font-face rule.
 */

export interface FontEntry {
  family: string
  weights: number[]
  italic: boolean
  source: 'system' | 'user'
  url?: string
}

const INJECTED = new Set<string>()
let list: FontEntry[] = []
let loaded = false
let loading: Promise<FontEntry[]> | null = null

/** Families offered in the UI, with the user's own fonts first. */
export function fontEntries(): FontEntry[] {
  return list
}

export function fontFamilies(): string[] {
  return list.map((f) => f.family)
}

/** Loads the font list once and injects @font-face rules for uploads. */
export function loadFonts(): Promise<FontEntry[]> {
  if (loaded) return Promise.resolve(list)
  if (loading) return loading
  loading = api
    .fonts()
    .then((fonts) => {
      list = fonts ?? []
      for (const f of list) {
        if (f.url) injectFontFace(f)
      }
      loaded = true
      return list
    })
    .catch(() => {
      list = []
      loaded = true
      return list
    })
  return loading
}

/** Drops the cache so the next load re-fetches (after `davinci font add`). */
export function reloadFonts(): Promise<FontEntry[]> {
  loaded = false
  loading = null
  return loadFonts()
}

function injectFontFace(f: FontEntry) {
  if (!f.url || INJECTED.has(f.family)) return
  INJECTED.add(f.family)
  const style = document.createElement('style')
  style.textContent = `@font-face{font-family:"${f.family}";src:url("${f.url}");font-display:swap}`
  document.head.appendChild(style)
}

/** The family as the font list knows it, tolerating a name we have never seen. */
export function resolveFamily(family: string): string {
  const want = (family ?? '').trim()
  if (!want) return DEFAULT_FAMILY
  const hit = list.find((f) => f.family === want)
  return hit ? hit.family : want
}

export const DEFAULT_FAMILY = 'PingFang SC'

const FALLBACKS = ['PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'sans-serif']

/** A CSS font-family list (the text editing box) that keeps Chinese glyphs intact on any machine. */
export function fontStack(family: string): string {
  const f = resolveFamily(family)
  return [f, ...FALLBACKS.filter((x) => x !== f)].join(', ')
}
