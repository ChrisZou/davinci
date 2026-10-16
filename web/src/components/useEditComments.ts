import { useEffect, useRef, useState } from 'react'
import { api, type AIProvider, type EditComment } from '../api'

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
  const mutation = useRef(0)
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
  useEffect(() => {
    alive.current = true
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      const version = mutation.current
      try {
        const list = await api.comments(projectID)
        if (!stopped && !busy.current && version === mutation.current) { setComments(list); setLoaded(true) }
      } catch (e: any) { if (!stopped) setError(e.message) }
      if (!stopped) timer = setTimeout(poll, 1500)
    }
    void detect()
    void poll()
    return () => { stopped = true; alive.current = false; clearTimeout(timer) }
  }, [projectID])

  const submit = async (input: { text: string; boardId: string; layerIds: string[] }) => {
    if (busy.current || running || !loaded || !available) return false
    const message = input.text.trim()
    if (!message) return false
    busy.current = true; mutation.current++; setSending(true); setError('')
    try {
      const comment = await api.createComment(projectID, {
        ...input, text: message, provider, layerIds: [...input.layerIds],
      })
      if (!alive.current) return
      setComments((all) => [...all.filter((c) => c.id !== comment.id), comment])
      try { localStorage.setItem('davinci.comment-provider', provider) } catch {}
      return comment
    } catch (e: any) { if (alive.current) setError(e.message) }
    finally { busy.current = false; if (alive.current) setSending(false) }
    return false
  }
  const cancel = async () => {
    if (!running || cancelling) return
    mutation.current++; setCancelling(true); setError('')
    try {
      const updated = await api.cancelComment(projectID, running.id)
      if (alive.current) setComments((all) => all.map((c) => c.id === updated.id ? updated : c))
    } catch (e: any) { if (alive.current) setError(e.message) }
    finally { mutation.current++; if (alive.current) setCancelling(false) }
  }

  return { comments, providers, provider, setProvider, error, loaded, sending, cancelling, running, available, detect, submit, cancel }
}

export type EditComments = ReturnType<typeof useEditComments>
