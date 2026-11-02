import { useEffect, useSyncExternalStore } from 'react'
import { api } from '../api'

let state: { colors: string[] | null; error: string; saving: boolean } = { colors: null, error: '', saving: false }
let pending: Promise<void> | null = null
let writing: Promise<void> | null = null
let sequence = 0
const listeners = new Set<() => void>()
const emit = () => listeners.forEach((fn) => fn())
const snapshot = () => state

export function reloadFavoriteColors(): Promise<void> {
  if (writing) return writing.catch(() => {})
  if (pending) return pending
  const mine = ++sequence
  pending = api.favoriteColors().then((colors) => {
    if (mine !== sequence) return
    state = { colors, error: '', saving: false }; emit()
  }).catch(() => {
    if (mine !== sequence) return
    state = { ...state, error: '收藏加载失败，点击重试' }; emit()
  }).finally(() => { if (mine === sequence) pending = null })
  return pending
}

export function setFavoriteColor(color: string, favorite: boolean): Promise<void> {
  // Serialize writes from separate controls; the server also deduplicates.
  if (writing) return writing.catch(() => {}).then(() => setFavoriteColor(color, favorite))
  ++sequence
  pending = null // A list requested before this write must not overwrite it.
  state = { ...state, saving: true, error: '' }; emit()
  writing = api.setFavoriteColor(color, favorite).then((colors) => {
    state = { colors, error: '', saving: true }
  }).finally(() => {
    writing = null
    state = { ...state, saving: false }; emit()
  })
  return writing
}

const refresh = () => { void reloadFavoriteColors() }
function subscribe(listener: () => void) {
  if (!listeners.size) window.addEventListener('focus', refresh)
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
    if (!listeners.size) window.removeEventListener('focus', refresh)
  }
}

export function useFavoriteColors() {
  const value = useSyncExternalStore(subscribe, snapshot, snapshot)
  useEffect(() => { void reloadFavoriteColors() }, [])
  return { ...value, reload: reloadFavoriteColors, setFavorite: setFavoriteColor }
}
