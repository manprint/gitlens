import { describe, expect, it } from 'vitest'

import { COMMON_ACRONYMS, glossary, isTerm } from './glossary'

describe('glossary (D30)', () => {
  it('glossary-001 defines at least 25 terms, each with a one-sentence definition', () => {
    const entries = Object.entries(glossary)
    expect(entries.length).toBeGreaterThanOrEqual(25)
    for (const [term, entry] of entries) {
      expect(entry.short, term).toMatch(/^\S.*\.$/)
      expect(entry.short.length, term).toBeLessThanOrEqual(160)
    }
  })

  it('glossary-002 holds the acronyms the console uses and keeps common ones apart', () => {
    for (const term of ['TPS', 'WAL', 'LSN', 'XID', 'MVCC', 'TOAST', 'HOT', 'ASH', 'FK']) {
      expect(isTerm(term), term).toBe(true)
    }
    for (const acronym of COMMON_ACRONYMS) expect(isTerm(acronym), acronym).toBe(false)
    expect(isTerm('toString')).toBe(false)
  })
})
