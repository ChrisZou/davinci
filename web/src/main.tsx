import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles.css'
import { App } from './App'
import { IS_MAC } from './editor/keys'

// In the macOS desktop app the page is the whole window, title bar included.
if (IS_MAC && /Electron\//.test(navigator.userAgent)) document.documentElement.classList.add('desktop-mac')

const el = document.getElementById('root')
if (!el) throw new Error('#root is missing from index.html')

createRoot(el).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
