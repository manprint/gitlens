import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'

const openApiDocument = readFileSync(new URL('../api/openapi.yaml', import.meta.url), 'utf8')
const apiContractVersion = /^ {2}version:\s*(\S+)\s*$/m.exec(openApiDocument)?.[1] ?? 'unknown'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  define: {
    __PGLENS_API_CONTRACT_VERSION__: JSON.stringify(apiContractVersion),
    __PGLENS_BUILD__: JSON.stringify(process.env.PGLENS_VERSION ?? 'dev'),
  },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 900,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
    },
  },
})
