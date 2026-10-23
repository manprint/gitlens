import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { loadConfigFromFile, type Plugin } from 'vite'
import { describe, expect, it } from 'vitest'

/** The documents the interface links to, relative to the repository root. */
const DOCUMENTS = ['README.md', 'docs/LIMITS.md', 'docs/api.md'] as const

interface EmittedAsset {
  type: string
  fileName: string
  source: string
}

interface DevRequest {
  url?: string
}

interface DevResponse {
  setHeader: (name: string, value: string) => void
  end: (body: Buffer) => void
}

type DevMiddleware = (request: DevRequest, response: DevResponse, next: () => void) => void

const testDirectory = dirname(fileURLToPath(import.meta.url))
const webDirectory = resolve(testDirectory, '../..')
const repositoryDirectory = resolve(webDirectory, '..')

function repositoryFile(path: string): string {
  return resolve(repositoryDirectory, path)
}

/** Plugins may nest in arrays, and may hold falsy entries for disabled plugins. */
function pluginsIn(options: readonly unknown[]): Plugin[] {
  return options.flatMap((option): Plugin[] => {
    if (Array.isArray(option)) return pluginsIn(option)
    if (typeof option === 'object' && option !== null && 'name' in option) return [option as Plugin]
    return []
  })
}

async function documentsPlugin(): Promise<Plugin> {
  // The config is loaded the way the build loads it, by path. Importing it
  // directly fails under vitest, where its import.meta.url is not a file URL.
  const loaded = await loadConfigFromFile(
    { command: 'build', mode: 'production' },
    resolve(webDirectory, 'vite.config.ts'),
  )
  if (!loaded) throw new Error('vite.config.ts did not load')
  const plugin = pluginsIn(loaded.config.plugins ?? []).find(
    (candidate) => candidate.name === 'pglens-documents',
  )
  if (plugin === undefined) throw new Error('pglens-documents is not configured')
  return plugin
}

describe('vite documents plugin', () => {
  it('emits each linked document next to the bundle with its source text', async () => {
    const plugin = await documentsPlugin()
    const emitted: EmittedAsset[] = []
    const generateBundle = plugin.generateBundle as unknown as (this: {
      emitFile: (asset: EmittedAsset) => void
    }) => void

    generateBundle.call({ emitFile: (asset) => emitted.push(asset) })

    expect(emitted.map((asset) => asset.fileName).sort()).toEqual([...DOCUMENTS].sort())
    for (const asset of emitted) {
      expect(asset.type).toBe('asset')
      expect(asset.source).toBe(readFileSync(repositoryFile(asset.fileName), 'utf8'))
    }
  })

  it('serves the same documents as plain text from the dev server and passes other paths on', async () => {
    const plugin = await documentsPlugin()
    const middlewares: DevMiddleware[] = []
    const configureServer = plugin.configureServer as unknown as (
      this: unknown,
      server: {
        middlewares: { use: (middleware: DevMiddleware) => void }
      },
    ) => void
    configureServer.call(null, {
      middlewares: { use: (middleware) => middlewares.push(middleware) },
    })
    const [middleware] = middlewares
    if (middleware === undefined) throw new Error('the dev middleware was not registered')

    const served: { headers: Record<string, string>; body?: Buffer } = { headers: {} }
    const response: DevResponse = {
      setHeader: (name, value) => {
        served.headers[name] = value
      },
      end: (body) => {
        served.body = body
      },
    }
    let passedOn = false
    middleware({ url: '/docs/api.md?ref=main' }, response, () => {
      passedOn = true
    })

    expect(passedOn).toBe(false)
    expect(served.headers['Content-Type']).toBe('text/plain; charset=utf-8')
    expect(served.body).toEqual(readFileSync(repositoryFile('docs/api.md')))

    const untouched: { ended: boolean; headers: string[] } = { ended: false, headers: [] }
    let next = false
    middleware(
      { url: '/fleet' },
      {
        setHeader: (name) => untouched.headers.push(name),
        end: () => {
          untouched.ended = true
        },
      },
      () => {
        next = true
      },
    )
    expect(next).toBe(true)
    expect(untouched).toEqual({ ended: false, headers: [] })
  })
})
