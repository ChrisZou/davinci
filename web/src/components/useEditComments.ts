import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type AIProvider, type EditComment } from '../api'
import { mergeComments } from './commentState'

/** One project-wide queue, shared by the selection composer and comment history. */
export function useEditComments(projectID: string) {
  const [comments, setComments] = useState<EditComment[]>([])
  const [providers, setProviders] = useState<AIProvider[]>([])
  const [provider, setProvider] = useState(() => { try { return localStorage.getItem('davinci.comment-provider') || 'codex' } catch { return 'codex' } })
  const [error, setError] = useState('')
  const [loaded, setLoaded] = useState(false)
  const [sending, setSending] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const alive = useRef(true)
  const busy = useRef(false)
  const running = comments.find((c) => c.status === 'running')
  const available = providers.find((p) => p.id === provider)?.available

  const detect = async () => {
    try {
      const ps = await api.aiProviders()
      if (!alive.current) return
      setProviders(ps)
      setProvider((p) => ps.some((x) => x.id === p && x.available) ? p : ps.find((x) => x.available)?.id || p)
    } catch (e: any) { if (alive.current) setError(e.message) }
  }
  const receive = useCallback((comment: EditComment) => {
    if (alive.current && comment.projectId === projectID) {
      setComments((all) => mergeComments(all, [comment]))
    }
  }, [projectID])
  const refresh = useCallback(async () => {
    try {
      const list = await api.comments(projectID)
      if (alive.current) { setComments((all) => mergeComments(all, list)); setLoaded(true) }
    } catch (e: any) { if (alive.current) setError(e.message) }
  }, [projectID])
  useEffect(() => {
    alive.current = true
    void detect()
    void refresh()
    return () => { alive.current = false }
  }, [projectID, refresh])

  useEffect(() => {
    if (provider !== 'codex' || !available) return
    const controller = new AbortController()
    // Preload silently. A comment can still initialize/recover the session if
    // warming fails; do not block the editor or add another setup step.
    void api.prepareAI(projectID, controller.signal).catch(() => {})
    return () => controller.abort()
  }, [projectID, provider, available])

  const submit = async (input: { text: string; boardId: string; layerIds: string[] }) => {
    if (busy.current || running || !loaded || !available) return false
    const message = input.text.trim()
    if (!message) return false
    busy.current = true; setSending(true); setError('')
    try {
      const comment = await api.createComment(projectID, {
        ...input, text: message, provider, layerIds: [...input.layerIds],
      })
      if (!alive.current) return
      receive(comment)
      try { localStorage.setItem('davinci.comment-provider', provider) } catch {}
      return comment
    } catch (e: any) { if (alive.current) setError(e.message) }
    finally { busy.current = false; if (alive.current) setSending(false) }
    return false
  }
  const cancel = async () => {
    if (!running || cancelling) return
    setCancelling(true); setError('')
    try {
      const updated = await api.cancelComment(projectID, running.id)
      if (alive.current) receive(updated)
    } catch (e: any) { if (alive.current) setError(e.message) }
    finally { if (alive.current) setCancelling(false) }
  }

  return { comments, providers, provider, setProvider, error, loaded, sending, cancelling, running, available, detect, submit, cancel, receive, refresh }
}

export type EditComments = ReturnType<typeof useEditComments>
