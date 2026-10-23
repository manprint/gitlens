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
    ['WD-09 sub-byte value keeps its magnitude', 0.5, '0.5 B'],
    ['WD-09 negative sub-byte value keeps its sign', -0.5, '-0.5 B'],
    ['WD-09 small per-call average keeps significant digits', 0.008192, '0.0082 B'],
    ['WD-09 rounding up to 1024 bytes moves to KiB', 1023.9, '1.0 KiB'],
    ['WD-09 rounding up to 1024 KiB moves to MiB', 1048575.9, '1.0 MiB'],
  ])('%s', (_name, value, expected) => {
    expect(formatBytes(value)).toBe(expected)
  })
})
