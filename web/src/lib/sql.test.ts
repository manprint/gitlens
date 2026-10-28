import { describe, expect, it } from 'vitest'

import { tokenizeSql } from './sql'

describe('tokenizeSql', () => {
  it('keeps every character, so the joined tokens equal the input', () => {
    const sql = "SELECT a, 'it''s' FROM t WHERE id = $1 AND n > 2.5 -- note\n/* c */ \"Col\""
    expect(
      tokenizeSql(sql)
        .map((token) => token.text)
        .join(''),
    ).toBe(sql)
  })

  it('classifies keywords, strings, parameters, numbers and comments', () => {
    const kinds = tokenizeSql("select 'x', $2, 42 -- c").filter((token) => token.kind !== 'text')
    expect(kinds).toEqual([
      { kind: 'keyword', text: 'select' },
      { kind: 'string', text: "'x'" },
      { kind: 'parameter', text: '$2' },
      { kind: 'number', text: '42' },
      { kind: 'comment', text: '-- c' },
    ])
  })

  it('treats identifiers that only contain a keyword as plain text', () => {
    expect(tokenizeSql('selected_rows')).toEqual([{ kind: 'text', text: 'selected_rows' }])
  })

  it('returns no tokens for empty input', () => {
    expect(tokenizeSql('')).toEqual([])
  })
})
