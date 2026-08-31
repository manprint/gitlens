import { describe, expect, it } from 'vitest'

import { formatCount } from './count'

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
