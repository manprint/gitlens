import { describe, expect, it } from 'vitest'

import { formatDuration } from './duration'

describe('formatDuration', () => {
  it.each([
    ['null is unknown', null, '—'],
    ['zero stays zero', 0, '0s'],
    ['negative values keep their sign', -65, '-1m 5s'],
    ['minute boundary', 60, '1m'],
    ['hour boundary', 3_600, '1h 0m'],
    ['day boundary', 86_400, '1d 0h 0m'],
  ])('%s', (_name, value, expected) => {
    expect(formatDuration(value)).toBe(expected)
  })

  it('does not turn an unknown duration into zero', () => {
    expect(formatDuration(null)).not.toBe('0s')
  })
})
