import { describe, expect, it } from 'vitest'

import { cacheHitRatioValue, findResets, integrateRate, scaleSeries } from './metrics'

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

  it('integrates stored per-second rates over the time each one covers', () => {
    const series = [
      point('2026-01-01T00:00:00Z', 10),
      point('2026-01-01T00:01:00Z', 2),
      point('2026-01-01T00:02:00Z', null),
      point('2026-01-01T00:03:00Z', 1),
    ]

    // 10/s over the first step, 2/s over one minute, 1/s over the two
    // minutes since the previous measured bucket.
    expect(integrateRate(series, 60)).toBe(600 + 120 + 120)
    expect(integrateRate([point('2026-01-01T00:00:00Z', null)], 60)).toBeNull()
    expect(integrateRate([], 60)).toBeNull()
  })

  it('scales measured buckets and keeps gaps', () => {
    expect(
      scaleSeries([point('2026-01-01T00:00:00Z', 0.5), point('2026-01-01T00:01:00Z', null)], 3600),
    ).toEqual([point('2026-01-01T00:00:00Z', 1800), point('2026-01-01T00:01:00Z', null)])
  })
})
