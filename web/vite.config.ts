import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig, type Plugin } from 'vite'

const openApiDocument = readFileSync(new URL('../api/openapi.yaml', import.meta.url), 'utf8')
const apiContractVersion = /^ {2}version:\s*(\S+)\s*$/m.exec(openApiDocument)?.[1] ?? 'unknown'
/** Documents the interface links to, keyed by the path they are served at. */
const documents = {
  'README.md': new URL('../README.md', import.meta.url),
  'docs/LIMITS.md': new URL('../docs/LIMITS.md', import.meta.url),
  'docs/api.md': new URL('../docs/api.md', import.meta.url),
}

/**
 * The interface links its guidance to these documents, which nothing served:
 * the server answers unknown paths with the application, so every documentation
 * link opened the Not found page. They now ship next to the bundle, and the dev
 * server serves them too, as plain text.
 */
function documentAssets(): Plugin {
  return {
    name: 'pglens-documents',
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        const path = request.url?.split('?')[0] ?? ''
        const document = Object.entries(documents).find(([name]) => path === `/${name}`)
        if (document === undefined) {
          next()
          return
        }
        response.setHeader('Content-Type', 'text/plain; charset=utf-8')
        response.end(readFileSync(document[1]))
      })
    },
    generateBundle() {
      for (const [fileName, file] of Object.entries(documents)) {
        this.emitFile({ type: 'asset', fileName, source: readFileSync(file, 'utf8') })
      }
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), documentAssets()],
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
