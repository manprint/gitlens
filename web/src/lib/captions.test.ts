import { describe, expect, it } from 'vitest'

import { CAPTIONS, columnHelp, type CaptionEntry } from './captions'
import { COMMON_ACRONYMS, isTerm } from './glossary'

const entries = Object.entries(CAPTIONS) as [string, CaptionEntry][]

/** Every text a reader sees for an entry. */
function textsOf(entry: CaptionEntry): string[] {
  return [
    entry.what,
    entry.source,
    entry.unit,
    entry.window,
    entry.howToRead,
    entry.thresholds,
  ].filter((text): text is string => text !== undefined)
}

/** Upper-case tokens of two or more letters, plus `I/O`. */
function acronymsIn(text: string): string[] {
  const tokens = text.match(/\b[A-Z]{2,}\b/g) ?? []
  return text.includes('I/O')
    ? [...tokens.filter((token) => token !== 'I' && token !== 'O'), 'I/O']
    : tokens
}

describe('caption catalogue (D30)', () => {
  it('CAPTION-001 every entry says what, from where, and its unit or window', () => {
    expect(entries.length).toBeGreaterThanOrEqual(5)
    for (const [id, entry] of entries) {
      expect(entry.what.trim(), id).not.toBe('')
      expect(entry.what.length, id).toBeLessThanOrEqual(160)
      expect(entry.what, id).toMatch(/\.$/)
      expect(entry.source.trim(), id).not.toBe('')
      expect(entry.unit !== undefined || entry.window !== undefined, id).toBe(true)
    }
  })

  it('CAPTION-002 every acronym is in the glossary or a common one', () => {
    const common = new Set<string>(COMMON_ACRONYMS)
    const texts = [...entries.flatMap(([, entry]) => textsOf(entry)), ...Object.values(columnHelp)]
    for (const text of texts) {
      for (const token of acronymsIn(text)) {
        expect(isTerm(token) || common.has(token), `${token} in "${text}"`).toBe(true)
      }
    }
  })

  it('CAPTION-003 the what sentence has at most 28 words', () => {
    for (const [id, entry] of entries) {
      expect(entry.what.split(/\s+/).length, id).toBeLessThanOrEqual(28)
    }
  })

  it('CAPTION-004 no entry holds a placeholder', () => {
    const texts = [...entries.flatMap(([, entry]) => textsOf(entry)), ...Object.values(columnHelp)]
    for (const text of texts) {
      for (const placeholder of ['TODO', '...', 'N/A']) expect(text).not.toContain(placeholder)
    }
  })

  it('CAPTION-006 every column hint is one short sentence', () => {
    const hints = Object.entries(columnHelp)
    expect(hints.length).toBeGreaterThanOrEqual(5)
    for (const [id, text] of hints) {
      expect(text.trim(), id).not.toBe('')
      expect(text.length, id).toBeLessThanOrEqual(160)
      expect(text, id).toMatch(/\.$/)
      expect(text.split(/\s+/).length, id).toBeLessThanOrEqual(28)
    }
  })

  it('CAPTION-005 ids are unique and dot or kebab case', () => {
    const ids = [...Object.keys(CAPTIONS), ...Object.keys(columnHelp)]
    expect(new Set(ids).size).toBe(ids.length)
    for (const id of ids) expect(id).toMatch(/^[a-z0-9]+(?:[.-][a-z0-9]+)*$/)
  })

  it('CAPTION-002 the acronym check finds the tokens it must', () => {
    expect(acronymsIn('WAL bytes per second, from pg_stat_wal and I/O timing.')).toEqual([
      'WAL',
      'I/O',
    ])
  })
})
