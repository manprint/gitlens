import { describe, expect, it } from 'vitest'

import { formatBytes } from './bytes'

describe('formatBytes', () => {
  it.each([
    ['null is unknown', null, '—'],
    ['zero has a byte unit', 0, '0 B'],
    ['negative values keep their sign', -1024, '-1.0 KiB'],
    ['byte boundary', 1023, '1023 B'],
    ['binary unit boundary', 1024, '1.0 KiB'],
    ['mebibyte boundary', 1024 ** 2, '1.0 MiB'],
  ])('%s', (_name, value, expected) => {
    expect(formatBytes(value)).toBe(expected)
  })
})
