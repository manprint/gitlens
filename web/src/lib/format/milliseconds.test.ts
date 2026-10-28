import { describe, expect, it } from 'vitest'

import { formatMilliseconds } from './milliseconds'

describe('formatMilliseconds', () => {
  it('reports an absent value as unknown, never as zero', () => {
    expect(formatMilliseconds(null)).toBe('—')
    expect(formatMilliseconds(undefined)).toBe('—')
    expect(formatMilliseconds(Number.NaN)).toBe('—')
  })

  it('keeps sub-millisecond precision', () => {
    expect(formatMilliseconds(0)).toBe('0 ms')
    expect(formatMilliseconds(0.0425)).toBe('0.043 ms')
  })

  it('uses milliseconds below one second', () => {
    expect(formatMilliseconds(4.567)).toBe('4.57 ms')
    expect(formatMilliseconds(123.45)).toBe('123.5 ms')
  })

  it('moves to seconds, minutes and hours as the value grows', () => {
    expect(formatMilliseconds(1_234)).toBe('1.23 s')
    expect(formatMilliseconds(90_000)).toBe('1.5 min')
    expect(formatMilliseconds(5_400_000)).toBe('1.5 h')
  })

  it('keeps the sign of a negative value', () => {
    expect(formatMilliseconds(-12)).toBe('-12 ms')
  })
})
