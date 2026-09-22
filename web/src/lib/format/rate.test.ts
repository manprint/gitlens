import { describe, expect, it } from 'vitest'

import { formatRate } from './rate'

describe('formatRate', () => {
  it.each([
    ['zero', 0, '0/s'],
    ['rare scan', 0.02, '0.02/s'],
    ['below ten', 4.567, '4.57/s'],
    ['large rate', 12_345.6, '12,346/s'],
  ])('%s', (_name, value, expected) => {
    expect(formatRate(value)).toBe(expected)
  })

  it('renders an unknown rate as the placeholder', () => {
    expect(formatRate(null)).toBe('—')
    expect(formatRate(Number.NaN)).toBe('—')
  })

  it('respects the explicit locale', () => {
    expect(formatRate(1.5, 'it-IT')).toBe('1,5/s')
  })
})
