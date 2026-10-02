import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// In dev the Vite server owns the page, and everything the Go server serves is
// proxied to it. In prod the Go binary serves web/dist itself.
const BACKEND = process.env.DAVINCI_BACKEND ?? 'http://127.0.0.1:7789'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': { target: BACKEND, changeOrigin: true },
      '/assets': { target: BACKEND, changeOrigin: true },
      '/fonts': { target: BACKEND, changeOrigin: true },
      '/ws': { target: BACKEND, ws: true, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // The Go server serves uploaded images from /assets/*, so the bundle's
    // chunks live under /app/* instead.
    assetsDir: 'app',
  },
})
