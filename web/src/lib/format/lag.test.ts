import { describe, expect, it } from 'vitest'

import { formatLag } from './lag'

describe('formatLag', () => {
  it.each([
    ['null is unknown', null, '—'],
    ['zero keeps sub-second precision', 0, '0.00 s'],
    ['negative values keep their sign', -0.12, '-0.12 s'],
    ['sub-second boundary', 0.12, '0.12 s'],
    ['whole-second boundary', 1, '1.0 s'],
  ])('%s', (_name, value, expected) => {
    expect(formatLag(value)).toBe(expected)
  })
})
