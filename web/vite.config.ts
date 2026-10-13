import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 24381,
    strictPort: true,
    // The Go server (:24380) proxies everything but /api here in dev and is the
    // preferred origin (cookies + OIDC callbacks share it). This proxy makes
    // http://localhost:24381 work on its own as well, websocket upgrades included.
    proxy: {
      '/api': { target: 'http://localhost:24380', changeOrigin: false, ws: true },
    },
  },
  build: { outDir: 'dist', sourcemap: true },
})
