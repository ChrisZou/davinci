import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles.css'
import { App } from './App'

const el = document.getElementById('root')
if (!el) throw new Error('#root is missing from index.html')

createRoot(el).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
