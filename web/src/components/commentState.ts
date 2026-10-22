import type { EditComment } from '../api'

/**
 * What this editor session shows: its own comments, plus one still running from
 * before (it holds the project's queue, so it must stay visible and cancellable).
 */
export function sessionComments(current: EditComment[], incoming: EditComment[], session: string): EditComment[] {
  return incoming.filter((c) => c.session === session || c.status === 'running' || current.some((x) => x.id === c.id))
}

/** A delayed HTTP response must never roll a pushed completion back to running. */
export function mergeComments(current: EditComment[], incoming: EditComment[]): EditComment[] {
  const byID = new Map(current.map((c) => [c.id, c]))
  for (const comment of incoming) {
    const previous = byID.get(comment.id)
    if (previous && previous.status !== 'running') continue
    byID.set(comment.id, comment)
  }
  return [...byID.values()].sort((a, b) => a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id))
}
