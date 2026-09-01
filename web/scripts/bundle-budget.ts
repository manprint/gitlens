import { readdirSync, readFileSync, statSync } from 'node:fs'
import { gzipSync } from 'node:zlib'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

export const INITIAL_GZIP_LIMIT = 350 * 1024
export const LAZY_GZIP_LIMIT = 500 * 1024

export interface ChunkMeasurement {
  fileName: string
  bytes: number
  gzipBytes: number
}

/** Parse the tab-separated chunk list emitted by the measurement step. */
export function parseChunkList(output: string): ChunkMeasurement[] {
  return output
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [fileName, rawBytes, rawGzipBytes] = line.split('\t')
      const bytes = Number(rawBytes)
      const gzipBytes = Number(rawGzipBytes)
      if (
        !fileName ||
        !Number.isSafeInteger(bytes) ||
        bytes < 0 ||
        !Number.isSafeInteger(gzipBytes) ||
        gzipBytes < 0
      ) {
        throw new Error(`invalid bundle chunk line: ${line}`)
      }
      return { fileName, bytes, gzipBytes }
    })
}

/** Return only relative JavaScript chunks imported synchronously by a bundle. */
export function parseStaticImports(source: string): string[] {
  const imports = /\bimport\s+(?:[^'"]*?\sfrom\s+)?['"](\.\/[^'"]+\.js)['"]/g
  return [...source.matchAll(imports)]
    .map((match) => match[1])
    .filter((path): path is string => path !== undefined)
}

function measureChunks(distDirectory: string): ChunkMeasurement[] {
  const lines = readdirSync(resolve(distDirectory, 'assets'))
    .filter((fileName) => fileName.endsWith('.js'))
    .sort()
    .map((fileName) => {
      const filePath = resolve(distDirectory, 'assets', fileName)
      const source = readFileSync(filePath)
      return `${fileName}\t${statSync(filePath).size}\t${gzipSync(source).byteLength}`
    })
  return parseChunkList(lines.join('\n'))
}

function initialChunkNames(
  distDirectory: string,
  chunks: readonly ChunkMeasurement[],
): Set<string> {
  const index = readFileSync(resolve(distDirectory, 'index.html'), 'utf8')
  const entry = /src="\/assets\/([^"']+\.js)"/.exec(index)?.[1]
  if (!entry || !chunks.some((chunk) => chunk.fileName === entry)) {
    throw new Error('could not find the Vite entry chunk in internal/webui/dist/index.html')
  }

  const knownChunks = new Set(chunks.map((chunk) => chunk.fileName))
  const initial = new Set<string>()
  const pending = [entry]
  while (pending.length > 0) {
    const fileName = pending.pop()
    if (!fileName || initial.has(fileName)) continue
    initial.add(fileName)
    const source = readFileSync(resolve(distDirectory, 'assets', fileName), 'utf8')
    for (const imported of parseStaticImports(source)) {
      const importedName = imported.slice(2)
      if (knownChunks.has(importedName)) pending.push(importedName)
    }
  }
  return initial
}

function formatKiB(bytes: number): string {
  return `${(bytes / 1024).toFixed(2)} KiB`
}

function main(): void {
  const distDirectory = resolve(
    dirname(fileURLToPath(import.meta.url)),
    '../../internal/webui/dist',
  )
  const chunks = measureChunks(distDirectory)
  const initial = initialChunkNames(distDirectory, chunks)
  const initialGzipBytes = chunks
    .filter((chunk) => initial.has(chunk.fileName))
    .reduce((total, chunk) => total + chunk.gzipBytes, 0)
  const lazy = chunks.filter((chunk) => !initial.has(chunk.fileName))
  const initialPass = initialGzipBytes < INITIAL_GZIP_LIMIT
  const lazyFailures = lazy.filter((chunk) => chunk.gzipBytes >= LAZY_GZIP_LIMIT)

  console.log(
    `initial entry + synchronous imports: ${formatKiB(initialGzipBytes)} (< 350 KiB) ${initialPass ? 'PASS' : 'FAIL'}`,
  )
  for (const chunk of lazy) {
    const result = chunk.gzipBytes < LAZY_GZIP_LIMIT ? 'PASS' : 'FAIL'
    console.log(`lazy ${chunk.fileName}: ${formatKiB(chunk.gzipBytes)} (< 500 KiB) ${result}`)
  }

  if (!initialPass || lazyFailures.length > 0) process.exitCode = 1
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()
