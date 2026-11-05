import { describe, expect, it } from 'vitest'

import { base } from '@/test/fixtures/getIndexSuggestions'

import {
  compareSuggestions,
  coverageText,
  DEFAULT_SUGGESTION_WINDOW,
  estimateLabel,
  keyText,
  matchesSuggestion,
  noteLabel,
  readSuggestionWindow,
  statementLink,
  statisticsMissing,
  verdictLabel,
  verdictVariant,
  writesText,
  type IndexSuggestion,
} from './indexSuggestions'

const suggestion = base.suggestions[0] as IndexSuggestion

describe('indexSuggestions', () => {
  it('SUGG-LIB-001 reads the window, falling back to 24h', () => {
    expect(readSuggestionWindow('6h')).toBe('6h')
    expect(readSuggestionWindow('30d')).toBe(DEFAULT_SUGGESTION_WINDOW)
    expect(readSuggestionWindow(null)).toBe('24h')
  })

  it('SUGG-LIB-002 translates note codes and keeps an unknown code readable', () => {
    for (const code of [
      'small_table',
      'low_selectivity',
      'width_assumed',
      'size_unknown',
      'column_unknown',
    ]) {
      expect(noteLabel(code)).not.toBe(code)
    }
    expect(noteLabel('future_note')).toBe('future_note')
  })

  it('SUGG-LIB-003 labels verdicts in words', () => {
    expect(verdictLabel('write_heavy')).toBe('Write-heavy')
    expect(verdictLabel('strong')).toBe('Strong')
    expect(verdictVariant('write_heavy')).toBe('warning')
    expect(verdictVariant('moderate')).toBe('secondary')
  })

  it('SUGG-LIB-004 prefixes a size estimate with ~ and says Unknown without one', () => {
    expect(estimateLabel(1_380_000)).toBe('~1.3 MiB')
    expect(estimateLabel(null)).toBe('Unknown')
  })

  it('SUGG-LIB-005 writes the key, the write rates and the statement link', () => {
    expect(
      keyText([
        { name: 'a', desc: false },
        { name: 'b', desc: true },
      ]),
    ).toBe('a, b DESC')
    expect(writesText(suggestion.write)).toBe('0.5/s · 1/s · 0.1/s')
    expect(statementLink('abc', -5)).toBe('/instances/abc/queries/-5')
  })

  it('SUGG-LIB-006 words the coverage line with the skipped reasons, most frequent first', () => {
    expect(coverageText({ statements_considered: 5, statements_analysed: 5, skipped: {} })).toBe(
      'Analysed 5 of 5 statements.',
    )
    expect(
      coverageText({
        statements_considered: 10,
        statements_analysed: 6,
        skipped: { or_group: 1, no_text: 3, zero: 0 },
      }),
    ).toBe('Analysed 6 of 10 statements (4 skipped: 3 no stored text, 1 OR condition).')
  })

  it('SUGG-LIB-007 tells missing statistics from no suggestion', () => {
    expect(
      statisticsMissing({ ...base, coverage: { ...base.coverage, statements_considered: 0 } }),
    ).toBe(true)
    expect(statisticsMissing(base)).toBe(false)
  })

  it('SUGG-LIB-008 matches the table, the key and the statement text', () => {
    expect(matchesSuggestion(suggestion, 'RES_PARTNER')).toBe(true)
    expect(matchesSuggestion(suggestion, 'ref')).toBe(true)
    expect(matchesSuggestion(suggestion, 'where ref =')).toBe(true)
    expect(matchesSuggestion(suggestion, 'nothing')).toBe(false)
    expect(matchesSuggestion(suggestion, '  ')).toBe(true)
  })

  it('SUGG-LIB-009 sorts, with an unknown size last in both directions', () => {
    const small = { ...suggestion, est_size_bytes: 10, weight_pct: 5 }
    const big = { ...suggestion, est_size_bytes: 100, weight_pct: 50 }
    const unknown = { ...suggestion, est_size_bytes: null }
    expect(compareSuggestions(small, big, 'size', 'asc')).toBeLessThan(0)
    expect(compareSuggestions(small, big, 'size', 'desc')).toBeGreaterThan(0)
    expect(compareSuggestions(unknown, big, 'size', 'asc')).toBeGreaterThan(0)
    expect(compareSuggestions(unknown, big, 'size', 'desc')).toBeGreaterThan(0)
    expect(compareSuggestions(small, big, 'weight', 'desc')).toBeGreaterThan(0)
    expect(compareSuggestions(small, big, 'verdict', 'asc')).toBe(0)
  })
})
