import { describe, expect, it } from 'vitest'

import { formatPercent } from './percent'

describe('formatPercent', () => {
  it.each([
    ['zero numerator', 0, 10, '0.0%'],
    ['negative numerator', -1, 4, '-25.0%'],
    ['fraction boundary', 1, 3, '33.3%'],
    ['whole boundary', 10, 10, '100.0%'],
  ])('%s', (_name, numerator, denominator, expected) => {
    expect(formatPercent(numerator, denominator)).toBe(expected)
  })

  it('returns null when the denominator is zero', () => {
    expect(formatPercent(5, 0)).toBeNull()
  })

  it('returns null for non-finite ratios', () => {
    expect(formatPercent(Number.NaN, 1)).toBeNull()
    expect(formatPercent(1, Number.POSITIVE_INFINITY)).toBeNull()
  })
})
