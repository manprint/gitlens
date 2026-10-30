import { describe, expect, it } from 'vitest'

import {
  LATEST_VALUE_BUCKETS,
  LATEST_VALUE_MIN_AGE_MS,
  cacheHitRatioValue,
  findResets,
  integrateRate,
  latestValue,
  scaleSeries,
} from './metrics'

const point = (ts: string, value: number | null) => ({ ts, value })

describe('metric helpers', () => {
  // Buckets one minute apart, the newest at 01:00.
  const minute = (m: number, value: number | null) =>
    point(`2026-08-27T00:${String(m).padStart(2, '0')}:00Z`, value)

  it('DL-UI-002 reads the latest measured value past an in-progress empty bucket', () => {
    expect(latestValue([minute(57, 3), minute(58, 7), minute(59, null)])).toBe(7)
    expect(latestValue([minute(57, 3), minute(58, null), minute(59, null)])).toBe(3)
    expect(latestValue([minute(58, 3), minute(59, 0)])).toBe(0)
    expect(latestValue([minute(58, 3), minute(59, 5)])).toBe(5)
    expect(latestValue([minute(59, 4)])).toBe(4)
  })

  it('DL-UI-003 reads unknown when every bucket within the bound is empty', () => {
    expect(LATEST_VALUE_BUCKETS).toBe(3)
    expect(
      latestValue([minute(56, 3), minute(57, null), minute(58, null), minute(59, null)]),
    ).toBeNull()
    expect(latestValue([minute(59, null)])).toBeNull()
    expect(latestValue([])).toBeNull()
    expect(latestValue([point('not a time', 3)])).toBeNull()
  })

  it('DL-UI-005 bounds the latest value by time as well as by buckets', () => {
    expect(LATEST_VALUE_MIN_AGE_MS).toBe(120_000)
    // Missing buckets: the measured point is among the last three, but nine
    // minutes before the newest bucket, beyond max(2 × 1 min, 2 min).
    expect(latestValue([minute(50, 3), minute(58, null), minute(59, null)])).toBeNull()
    // Fifteen-second buckets: the two-minute floor, not 2 × 15 s, bounds the age.
    const quarter = (s: number, value: number | null) =>
      point(new Date(Date.UTC(2026, 7, 27, 1, 0, s)).toISOString(), value)
    expect(latestValue([quarter(15, 8), quarter(30, null), quarter(45, null)])).toBe(8)
    // Five-minute buckets: 2 × step (ten minutes) bounds it.
    const fives = (m: number, value: number | null) =>
      point(`2026-08-27T01:${String(m).padStart(2, '0')}:00Z`, value)
    expect(latestValue([fives(0, 2), fives(5, null), fives(10, null)])).toBe(2)
  })

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
