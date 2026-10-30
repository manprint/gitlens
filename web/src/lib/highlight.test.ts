import { describe, expect, it } from 'vitest'

import { highlightMatches, matchCountNote, matchRanges, searchTermOf } from './highlight'

describe('highlightMatches', () => {
  it('SRCH-UI-001 marks the term ignoring case and keeps the original case', () => {
    expect(highlightMatches('Select * From res_partner', 'RES')).toEqual([
      { text: 'Select * From ', match: false },
      { text: 'res', match: true },
      { text: '_partner', match: false },
    ])
  })

  it('SRCH-UI-002 marks every non-overlapping occurrence in reading order', () => {
    expect(highlightMatches('orders JOIN Orders_x ON aaaa', 'orders')).toEqual([
      { text: 'orders', match: true },
      { text: ' JOIN ', match: false },
      { text: 'Orders', match: true },
      { text: '_x ON aaaa', match: false },
    ])
    // Matches do not overlap: `aaaa` holds two `aa`, not three.
    expect(highlightMatches('aaaa', 'aa')).toEqual([
      { text: 'aa', match: true },
      { text: 'aa', match: true },
    ])
    // The term is trimmed and its regular-expression characters match themselves.
    expect(highlightMatches('cost 100% (a.b)', ' 100% ')).toEqual([
      { text: 'cost ', match: false },
      { text: '100%', match: true },
      { text: ' (a.b)', match: false },
    ])
    expect(highlightMatches('f(a.b) a-b', '(a.b)')).toEqual([
      { text: 'f', match: false },
      { text: '(a.b)', match: true },
      { text: ' a-b', match: false },
    ])
    expect(highlightMatches('x a-b', 'a-b')).toEqual([
      { text: 'x ', match: false },
      { text: 'a-b', match: true },
    ])
  })

  it('SRCH-UI-003 an empty, short or absent term gives one unmarked run', () => {
    for (const q of ['', ' ', 'a', ' s ', null, undefined]) {
      expect(highlightMatches('Select 1', q)).toEqual([{ text: 'Select 1', match: false }])
    }
    expect(highlightMatches('Select 1', 'zz')).toEqual([{ text: 'Select 1', match: false }])
    expect(highlightMatches('', 'zz')).toEqual([{ text: '', match: false }])
  })

  it('gives the offsets of the marked runs', () => {
    expect(matchRanges('Select * From res_partner', 'res')).toEqual([[14, 17]])
    expect(matchRanges('abab', 'ab')).toEqual([
      [0, 2],
      [2, 4],
    ])
    expect(matchRanges('abab', 'a')).toEqual([])
  })
})

describe('search words', () => {
  it('keeps a term of 2 or more characters, trimmed', () => {
    expect(searchTermOf(' res ')).toBe('res')
    expect(searchTermOf('r')).toBe('')
    expect(searchTermOf(null)).toBe('')
  })

  it('states the match count, or that more match than were loaded (I-9)', () => {
    expect(matchCountNote(1, false)).toBe('1 match.')
    expect(matchCountNote(37, false)).toBe('37 matches.')
    expect(matchCountNote(200, true)).toBe('More than 200 matches — narrow the search.')
  })
})
