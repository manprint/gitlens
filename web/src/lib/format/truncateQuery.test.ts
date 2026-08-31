import { describe, expect, it } from 'vitest'

import { isTruncatedQuery, TRUNCATION_MARKER, truncateQuery } from './truncateQuery'

describe('truncateQuery', () => {
  it('leaves an empty query unchanged', () => {
    expect(truncateQuery('')).toBe('')
    expect(isTruncatedQuery('')).toBe(false)
  })

  it('does not truncate exactly at the 2048-byte budget', () => {
    const query = 'x'.repeat(2048)
    expect(truncateQuery(query)).toBe(query)
    expect(isTruncatedQuery(query)).toBe(false)
  })

  it('marks a query that exceeds the 2048-byte budget', () => {
    const result = truncateQuery('x'.repeat(2049))
    expect(result).toContain(TRUNCATION_MARKER)
    expect(isTruncatedQuery(result)).toBe(true)
    expect(new TextEncoder().encode(result)).toHaveLength(2048)
  })

  it('does not split a multibyte character', () => {
    const result = truncateQuery('é'.repeat(2_000), 20)
    expect(result).toContain(TRUNCATION_MARKER)
    expect(result).not.toContain('\ufffd')
  })

  it('returns the marker when the budget cannot contain a prefix', () => {
    expect(truncateQuery('query', 0)).toBe(TRUNCATION_MARKER)
  })
})
