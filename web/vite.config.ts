import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig, type Plugin } from 'vite'

const openApiDocument = readFileSync(new URL('../api/openapi.yaml', import.meta.url), 'utf8')
const apiContractVersion = /^ {2}version:\s*(\S+)\s*$/m.exec(openApiDocument)?.[1] ?? 'unknown'
const readme = new URL('../README.md', import.meta.url)

/**
 * The interface links its guidance to /README.md, which nothing served: the
 * server answers unknown paths with the application, so every documentation
 * link opened the Not found page. The README now ships next to the bundle
 * (and is served by the dev server), as plain text.
 */
function readmeAsset(): Plugin {
  return {
    name: 'pglens-readme',
    configureServer(server) {
      server.middlewares.use('/README.md', (_request, response) => {
        response.setHeader('Content-Type', 'text/plain; charset=utf-8')
        response.end(readFileSync(readme))
      })
    },
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'README.md', source: readFileSync(readme, 'utf8') })
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), readmeAsset()],
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
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (
            id.includes('/node_modules/echarts') ||
            id.includes('/node_modules/echarts-for-react')
          ) {
            return 'vendor-echarts'
          }
          if (id.includes('/node_modules/@xyflow/')) return 'vendor-react-flow'
          return undefined
        },
      },
    },
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
