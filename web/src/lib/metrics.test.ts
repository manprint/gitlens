import { describe, expect, it } from 'vitest'

import { cacheHitRatioValue, counterRateSeries, findResets, rangeDelta } from './metrics'

const point = (ts: string, value: number | null) => ({ ts, value })

describe('metric helpers', () => {
  it('UI-INST-020 marks each null bucket bounded by values', () => {
    expect(
      findResets([
        point('2026-01-01T00:00:00Z', 10),
        point('2026-01-01T00:01:00Z', null),
        point('2026-01-01T00:02:00Z', null),
        point('2026-01-01T00:03:00Z', 12),
      ]),
    ).toEqual([1, 2])
  })

  it('UI-INST-021 does not mark a leading or trailing null', () => {
    expect(
      findResets([
        point('2026-01-01T00:00:00Z', null),
        point('2026-01-01T00:01:00Z', 10),
        point('2026-01-01T00:02:00Z', null),
      ]),
    ).toEqual([])
  })

  it('UI-INST-023 treats an idle database as having an unknown hit ratio', () => {
    expect(cacheHitRatioValue(0, 0)).toBeNull()
    expect(cacheHitRatioValue(75, 25)).toBe('75.0%')
  })

  it('derives rates and range totals without turning resets into spikes', () => {
    const series = [
      point('2026-01-01T00:00:00Z', 10),
      point('2026-01-01T00:01:00Z', 16),
      point('2026-01-01T00:02:00Z', null),
      point('2026-01-01T00:03:00Z', 4),
      point('2026-01-01T00:04:00Z', 9),
    ]

    expect(counterRateSeries(series).map(({ value }) => value)).toEqual([
      null,
      0.1,
      null,
      null,
      0.08333333333333333,
    ])
    expect(rangeDelta(series)).toBe(11)
  })
})
