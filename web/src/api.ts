import type { Document, LayerRow } from './types'

/**
 * REST + WebSocket client for the davinci server. Everything here is plain
 * fetch: the editor page uses it for project loading and for the human-side
 * save, and AI-driven changes arrive over the socket (see bridge.ts).
 */

export interface CanvasPreset {
  key: string
  name: string
  width: number
  height: number
}

export interface ProjectSummary {
  id: string
  name: string
  width: number
  height: number
  revision: number
  updatedAt: string
}

export interface Project extends ProjectSummary {
  document: Document
  thumbnail?: string
  createdAt: string
}

export interface LibraryCategory {
  id: string
  name: string
  sort: number
  count: number
}

export interface LibraryItem {
  id: string
  categoryId: string
  category: string
  name: string
  url: string
  thumb: string
  width: number
  height: number
  tags: string[]
  description: string
  meta?: unknown
  source?: string
  createdAt: string
}

async function asJSON<T>(resp: Response): Promise<T> {
  const text = await resp.text()
  let body: any = null
  try {
    body = text ? JSON.parse(text) : null
  } catch {
    throw new Error(`${resp.status}: ${text.slice(0, 200)}`)
  }
  if (!resp.ok) {
    throw new Error(body?.error ?? `HTTP ${resp.status}`)
  }
  return body as T
}

const base = ''

export const api = {
  async health() {
    return asJSON<{ ok: boolean; projects: number; boot: string }>(await fetch(`${base}/api/health`))
  },

  async presets() {
    const out = await asJSON<{ presets: CanvasPreset[] }>(await fetch(`${base}/api/canvas-presets`))
    return out.presets
  },

  async projects() {
    const out = await asJSON<{ projects: ProjectSummary[] }>(await fetch(`${base}/api/projects`))
    return out.projects
  },

  async project(id: string) {
    const out = await asJSON<{ project: Project }>(await fetch(`${base}/api/projects/${encodeURIComponent(id)}`))
    return out.project
  },

  async createProject(input: { name?: string; preset?: string; width?: number; height?: number }) {
    const out = await asJSON<{ project: Project; editorURL: string }>(
      await fetch(`${base}/api/projects`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(input),
      }),
    )
    return out.project
  },

  async renameProject(id: string, name: string) {
    await asJSON<{ ok: boolean }>(
      await fetch(`${base}/api/projects/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      }),
    )
  },

  /** The saved thumbnail as an image URL; the revision busts the cache per save. */
  thumbnailURL(p: { id: string; revision: number }) {
    return `${base}/api/projects/${encodeURIComponent(p.id)}/thumbnail?rev=${p.revision}`
  },

  async deleteProject(id: string) {
    await asJSON<{ ok: boolean }>(
      await fetch(`${base}/api/projects/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    )
  },

  async layers(id: string) {
    const out = await asJSON<{ layers: LayerRow[] }>(
      await fetch(`${base}/api/projects/${encodeURIComponent(id)}/layers`),
    )
    return out.layers
  },

  /** Uploads a local file (or imports a URL) and returns its /assets/ URL. */
  async uploadAsset(file: File | Blob, filename?: string): Promise<string> {
    const form = new FormData()
    form.append('file', file, filename)
    const out = await asJSON<{ asset: { url: string } }>(
      await fetch(`${base}/api/assets`, { method: 'POST', body: form }),
    )
    return out.asset.url
  },

  // --- material library ---

  async libraryCategories() {
    const out = await asJSON<{ categories: LibraryCategory[] }>(await fetch(`${base}/api/library/categories`))
    return out.categories
  },

  async createCategory(name: string) {
    const out = await asJSON<{ category: LibraryCategory }>(
      await fetch(`${base}/api/library/categories`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      }),
    )
    return out.category
  },

  async renameCategory(id: string, name: string) {
    await asJSON(
      await fetch(`${base}/api/library/categories/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      }),
    )
  },

  async deleteCategory(id: string) {
    await asJSON(await fetch(`${base}/api/library/categories/${encodeURIComponent(id)}`, { method: 'DELETE' }))
  },

  async libraryItems(query: { category?: string; q?: string; limit?: number } = {}) {
    const p = new URLSearchParams()
    if (query.category) p.set('category', query.category)
    if (query.q) p.set('q', query.q)
    p.set('limit', String(query.limit ?? 500))
    return asJSON<{ items: LibraryItem[]; total: number }>(await fetch(`${base}/api/library/items?${p}`))
  },

  async libraryItem(id: string) {
    const out = await asJSON<{ item: LibraryItem }>(await fetch(`${base}/api/library/items/${encodeURIComponent(id)}`))
    return out.item
  },

  async addLibraryItem(file: File, category: string) {
    const form = new FormData()
    form.append('file', file, file.name)
    form.append('category', category)
    // Cut-outs often come on a big transparent frame; keep just what is visible.
    // Only transparent margins of a PNG are removed, so other pictures are untouched.
    form.append('trim', 'true')
    const out = await asJSON<{ item: LibraryItem }>(await fetch(`${base}/api/library/items`, { method: 'POST', body: form }))
    return out.item
  },

  async updateLibraryItem(id: string, patch: { name?: string; category?: string; tags?: string[]; description?: string }) {
    const out = await asJSON<{ item: LibraryItem }>(
      await fetch(`${base}/api/library/items/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch),
      }),
    )
    return out.item
  },

  async deleteLibraryItem(id: string) {
    await asJSON(await fetch(`${base}/api/library/items/${encodeURIComponent(id)}`, { method: 'DELETE' }))
  },

  async fonts() {
    const out = await asJSON<{ fonts: FontInfo[] }>(await fetch(`${base}/api/fonts`))
    return out.fonts
  },

  /** Stars or hides a font; returns the whole list, re-ordered. */
  async setFontPref(family: string, pref: { favorite?: boolean; hidden?: boolean }) {
    const out = await asJSON<{ fonts: FontInfo[] }>(
      await fetch(`${base}/api/fonts/prefs`, { method: 'PUT', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ family, ...pref }) }),
    )
    return out.fonts
  },
}

/** A font family, as the picker shows it: favourites first, some hidden. */
export interface FontInfo {
  family: string
  weights: number[]
  italic: boolean
  source: 'system' | 'user'
  url?: string
  /** Other names it answers to, localised (中文) ones first. */
  aliases?: string[]
  favorite?: boolean
  hidden?: boolean
}
