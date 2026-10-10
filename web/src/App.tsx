import { useEffect, useState } from 'react'
import { Home } from './pages/Home'
import { Editor } from './pages/Editor'
import { Library } from './pages/Library'
import { Templates } from './pages/Templates'

/**
 * Routes, driven by the pathname so the Go server's `/editor/<id>` links
 * work directly: `/` is the project list, `/editor/<id>` is a canvas,
 * `/library` the material library and `/templates` the template library.
 */
function routeOf(): { page: 'home' | 'editor' | 'library' | 'templates'; id: string } {
  const m = location.pathname.match(/^\/editor\/([^/?#]+)/)
  if (m) return { page: 'editor', id: decodeURIComponent(m[1]) }
  if (/^\/library\/?$/.test(location.pathname)) return { page: 'library', id: '' }
  if (/^\/templates\/?$/.test(location.pathname)) return { page: 'templates', id: '' }
  return { page: 'home', id: '' }
}

export function navigate(path: string) {
  history.pushState(null, '', path)
  window.dispatchEvent(new PopStateEvent('popstate'))
}

export function App() {
  const [r, setR] = useState(routeOf)
  useEffect(() => {
    const on = () => setR(routeOf())
    window.addEventListener('popstate', on)
    return () => window.removeEventListener('popstate', on)
  }, [])

  // The drag strip comes first: the clickable things after it cut themselves out of it.
  return (
    <>
      <div className="titlebar-drag" aria-hidden />
      {r.page === 'editor' ? <Editor projectID={r.id} /> : r.page === 'library' ? <Library /> : r.page === 'templates' ? <Templates /> : <Home />}
    </>
  )
}
