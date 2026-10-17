import { useEffect, useState } from 'react'
import { api, type ProjectSummary } from '../api'
import { navigate } from '../App'
import { Lockup } from '../components/Brand'
import { PaletteBrowser } from '../components/PaletteParts'

export function Palettes({ id }: { id?: string }) {
  const [projects, setProjects] = useState<ProjectSummary[]>([])
  const [project, setProject] = useState(() => new URLSearchParams(location.search).get('project') ?? '')
  const [error, setError] = useState('')
  const [board, setBoard] = useState(() => new URLSearchParams(location.search).get('board') ?? undefined)
  useEffect(() => {
    document.title = '配色 · DaVinci'
    void api.projects({ kind: 'all' }).then((ps) => { setProjects(ps); setProject((p) => p || ps.find((x) => x.kind === 'design')?.id || '') })
      .catch((e: Error) => setError(e.message))
  }, [])
  return (
    <div className="flex h-full flex-col bg-paper">
      <header className="flex min-h-[84px] shrink-0 flex-wrap items-center gap-3 px-5 py-4 sm:px-8">
        <Lockup size={30} /><span className="h-4 w-px bg-line-strong" />
        {id ? <a href={`/palettes${location.search}`} className="text-sm text-ink-2" onClick={(e) => { e.preventDefault(); navigate(`/palettes${location.search}`) }}>← 配色</a> : <h1 className="m-0 font-serif text-2xl font-black text-ink">配色</h1>}
        <div className="flex-1" />
        {projects.length > 0 && <select aria-label="应用配色的项目" value={project} className="h-9 min-w-0 max-w-[min(15rem,50vw)] rounded-full bg-card px-4 text-sm" onChange={(e) => {
          setProject(e.target.value); setBoard(undefined)
          const url = new URL(location.href); url.searchParams.set('project', e.target.value); url.searchParams.delete('board'); history.replaceState(null, '', url)
        }}>
          <option value="" disabled>选择项目</option>
          {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>}
      </header>
      {error && <p role="alert" className="mx-5 text-sm text-accent">{error}</p>}
      <div className="mx-auto flex min-h-0 w-full max-w-[1280px] flex-1 px-5 pb-5 sm:px-8">
        <PaletteBrowser selectedID={id} onSelect={(next) => navigate(`/palettes${next ? `/${encodeURIComponent(next)}` : ''}${location.search}`)} onApply={project ? async (palette) => { await api.applyPalette(project, palette, board) } : undefined} />
      </div>
    </div>
  )
}
