import { useEffect, useState } from 'react'
import { Modal } from './Dialog'

/**
 * The desktop app's first run: what an agent needs before it can work on the
 * designs here — the davinci command and the skill — each one button away.
 * Shown on the home page until both are in place, or until put off.
 * Browsers have no window.davinciApp, so they never see it.
 */

type Setup = { cli: boolean; skill: boolean }
/** An install's outcome; a cancelled one is { ok: false } with no message. */
type Result = { ok: boolean; message?: string; detail?: string }

declare global {
  interface Window {
    davinciApp?: {
      platform: string
      setupStatus(): Promise<Setup>
      installCLI(): Promise<Result>
      installSkill(): Promise<Result>
    }
  }
}

const KEY = 'davinci.onboarding'

function putOff(state: 'later' | 'done') {
  try {
    localStorage.setItem(KEY, state)
  } catch {
    // Without storage it simply asks again next time.
  }
}

export function Onboarding() {
  const app = window.davinciApp
  const [open, setOpen] = useState(false)
  const [status, setStatus] = useState<Setup>({ cli: false, skill: false })
  const [busy, setBusy] = useState<keyof Setup | null>(null)
  const [notes, setNotes] = useState<Partial<Record<keyof Setup, Result>>>({})

  useEffect(() => {
    if (!app) return
    let seen = false
    try {
      seen = localStorage.getItem(KEY) !== null
    } catch {}
    if (seen) return
    void app.setupStatus().then((s) => {
      setStatus(s)
      if (!(s.cli && s.skill)) setOpen(true)
    })
  }, [app])

  if (!app || !open) return null

  const install = async (step: keyof Setup) => {
    setBusy(step)
    try {
      const r = await (step === 'cli' ? app.installCLI() : app.installSkill())
      setNotes((n) => ({ ...n, [step]: r }))
      if (r.ok) setStatus((s) => ({ ...s, [step]: true }))
    } finally {
      setBusy(null)
    }
  }

  const ready = status.cli && status.skill
  const close = (state: 'later' | 'done') => {
    putOff(state)
    setOpen(false)
  }
  const menu = app.platform === 'win32' ? '按Alt调出菜单，在「帮助」里' : '在菜单「davinci」里'

  const step = (key: keyof Setup, n: number, title: string, desc: string) => {
    const done = status[key]
    const note = notes[key]
    return (
      <li className="flex gap-3.5 rounded-2xl bg-paper px-4 py-3.5">
        <span
          className={`mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-bold ${done ? 'bg-ok text-card' : 'bg-card text-ink-2'}`}
          aria-hidden
        >
          {done ? '✓' : n}
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-[14px] font-bold text-ink">{title}</div>
          <p className="m-0 mt-0.5 text-[12.5px] leading-relaxed text-muted">{desc}</p>
          {note?.message && (
            <p className={`m-0 mt-1.5 text-[12px] leading-relaxed ${note.ok ? 'text-muted' : 'text-accent'}`}>
              {note.ok ? note.detail : `${note.message}${note.detail ? `：${note.detail}` : ''}`}
            </p>
          )}
        </div>
        {done ? (
          <span className="self-center text-[12.5px] font-bold text-ok">已安装</span>
        ) : (
          <button className="btn-primary h-8 self-center px-4 text-[13px]" disabled={busy !== null} onClick={() => void install(key)} data-testid={`onboarding-${key}`}>
            {busy === key ? '安装中…' : '安装'}
          </button>
        )}
      </li>
    )
  }

  return (
    <Modal onClose={() => close('later')} width="w-[480px]" label="开始使用davinci">
      <div className="flex flex-col gap-5 p-7" data-testid="onboarding">
        <div>
          <h2 className="m-0 font-serif text-[22px] font-black text-ink">开始使用davinci</h2>
          <p className="m-0 mt-1.5 text-[13px] leading-relaxed text-muted">装好这两样，你的AI Agent就能直接操作这里的每一个图层。</p>
        </div>
        <ol className="m-0 flex list-none flex-col gap-2.5 p-0">
          {step('cli', 1, '安装命令行工具', 'Agent通过davinci命令创建和修改设计稿。')}
          {step('skill', 2, '安装Agent skill', '装给Claude Code、Codex、Hermes，Agent就知道什么时候、怎么用davinci。')}
        </ol>
        {ready && (
          <p className="m-0 rounded-2xl border border-line px-4 py-3 text-[13px] leading-relaxed text-ink-2">
            都装好了。新开一个Agent会话，跟它说「做一张小红书封面，标题是……」，它改的每一个图层都会实时出现在这里。
          </p>
        )}
        <div className="flex items-center justify-between">
          <span className="text-[12px] text-faint">{ready ? '' : `以后也可以${menu}找到这两项。`}</span>
          {ready ? (
            <button className="btn-primary" onClick={() => close('done')} data-testid="onboarding-done">
              开始使用
            </button>
          ) : (
            <button className="btn-ghost" onClick={() => close('later')}>
              以后再说
            </button>
          )}
        </div>
      </div>
    </Modal>
  )
}
