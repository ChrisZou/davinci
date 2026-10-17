import { useEffect, useSyncExternalStore } from 'react'
import { api, type Palette } from '../api'

// One shared request for all ColorFields. Focus refresh also picks up CLI edits.
let state: { items: Palette[] | null; error: string } = { items: null, error: '' }
let pending: Promise<void> | null = null
let sequence = 0
const listeners = new Set<() => void>()
const emit = () => listeners.forEach((fn) => fn())
export function reloadPalettes(force = false): Promise<void> {
  if (pending && !force) return pending
  const mine = ++sequence
  pending = api.palettes().then((items) => {
    if (mine !== sequence) return
    state = { items, error: '' }; emit()
  }).catch((e: unknown) => {
    if (mine !== sequence) return
    state = { ...state, error: e instanceof Error ? e.message : String(e) }; emit()
  }).finally(() => { if (mine === sequence) pending = null })
  return pending
}
function subscribe(listener: () => void) {
  if (!listeners.size) window.addEventListener('focus', refresh)
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
    if (!listeners.size) window.removeEventListener('focus', refresh)
  }
}
const refresh = () => { void reloadPalettes() }
const snapshot = () => state
export function usePalettes() {
  const value = useSyncExternalStore(subscribe, snapshot)
  useEffect(() => { void reloadPalettes() }, [])
  return { ...value, reload: reloadPalettes }
}
