import { describe, expect, it } from 'vitest'

import { formatCount, formatIntegerString } from './count'

describe('formatCount', () => {
  it.each([
    ['zero', 0, '0'],
    ['negative', -1_234, '-1,234'],
    ['thousand boundary', 1_000, '1,000'],
    ['large count', 1_234_567, '1,234,567'],
  ])('%s', (_name, value, expected) => {
    expect(formatCount(value)).toBe(expected)
  })

  it('respects the explicit locale', () => {
    expect(formatCount(1_234_567, 'it-IT')).toBe('1.234.567')
  })
})

describe('formatIntegerString', () => {
  it('keeps every digit of a bigint', () => {
    expect(formatIntegerString('9223372036854775807')).toBe('9,223,372,036,854,775,807')
    expect(formatIntegerString('-9223372036854775808')).toBe('-9,223,372,036,854,775,808')
  })

  it('returns a value that is not an integer as it is', () => {
    expect(formatIntegerString('1.5')).toBe('1.5')
    expect(formatIntegerString('')).toBe('')
  })
})
