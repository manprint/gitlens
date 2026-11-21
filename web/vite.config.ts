import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { extname, join, resolve } from 'node:path'
import { constants, gzipSync } from 'node:zlib'
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

const COMPRESSIBLE = new Set(['.js', '.css', '.html', '.md', '.svg', '.json', '.txt'])

/**
 * Writes `name.gz` next to every text file over 1 KiB of the build, which the
 * server sends to browsers that accept gzip: the bundle shrinks to about a
 * quarter and the server compresses nothing at run time.
 */
function gzipAssets(): Plugin {
  let outDir = ''
  return {
    name: 'pglens-gzip',
    apply: 'build',
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir)
    },
    closeBundle() {
      const walk = (directory: string) => {
        for (const entry of readdirSync(directory, { withFileTypes: true })) {
          const path = join(directory, entry.name)
          if (entry.isDirectory()) walk(path)
          else if (COMPRESSIBLE.has(extname(entry.name)) && statSync(path).size > 1024) {
            const source = readFileSync(path)
            const packed = gzipSync(source, { level: constants.Z_BEST_COMPRESSION })
            if (packed.length < source.length * 0.9) writeFileSync(`${path}.gz`, packed)
          }
        }
      }
      walk(outDir)
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), documentAssets(), gzipAssets()],
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
          // One chunk per language; English stays in the entry (plan 006 D5).
          const locale = /\/src\/i18n\/messages\/([^/]+)\//.exec(id)?.[1]
          if (locale && locale !== 'en') return `i18n-${locale}`
          // Captions, rule sentences, glossary and units: a second chunk per language (F4).
          const domain = /\/src\/i18n\/domain\/([^/]+)\//.exec(id)?.[1]
          if (domain) return `i18n-${domain}-domain`
          // The Inline Help of a language other than English loads with its first reader (F6).
          const help = /\/src\/lib\/help\/topics\/([^/]+)\//.exec(id)?.[1]
          if (help && help !== 'en') return `help-${help}`
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
