import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: { outDir: '../internal/server/frontend/dist', emptyOutDir: true },
  server: { port: 5173, proxy: { '/api': 'http://localhost:8080', '/healthz': 'http://localhost:8080', '/readyz': 'http://localhost:8080' } },
  test: { environment: 'jsdom' }
})
