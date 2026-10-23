import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

/** A UI test ID opens a test title: a quote, UI-AREA-NNN, then a space. */
const TEST_ID = /(['"`])(UI-[A-Z]+-\d+) /g

const webDirectory = resolve(dirname(fileURLToPath(import.meta.url)), '../..')

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) return sourceFiles(path)
    return /\.(test\.)?tsx?$/.test(entry.name) ? [path] : []
  })
}

describe('UI test IDs', () => {
  it('names one test title per UI-* ID, so a failure can be found by its ID', () => {
    const owners = new Map<string, string[]>()
    for (const directory of ['src', 'e2e']) {
      for (const file of sourceFiles(join(webDirectory, directory))) {
        for (const match of readFileSync(file, 'utf8').matchAll(TEST_ID)) {
          const id = match[2] ?? ''
          const where = relative(webDirectory, file)
          owners.set(id, [...(owners.get(id) ?? []), where])
        }
      }
    }
    const duplicates = [...owners].filter(([, files]) => files.length > 1)
    expect(duplicates).toEqual([])
  })
})
