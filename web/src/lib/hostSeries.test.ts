import { describe, expect, it } from 'vitest'

import { niceCeil, subtractSeries, toPercent, usedRatio } from './hostSeries'

const at = (minute: number) => `2026-10-29T10:${String(minute).padStart(2, '0')}:00Z`

describe('host series', () => {
  it('HOSTCH-001 subtracts aligned buckets and keeps a gap in either series', () => {
    const total = [
      { ts: at(0), value: 100 },
      { ts: at(1), value: 100 },
      { ts: at(2), value: null },
    ]
    const available = [
      { ts: at(0), value: 40 },
      { ts: at(1), value: null },
      { ts: at(2), value: 30 },
    ]
    expect(subtractSeries(total, available)).toEqual([
      { ts: at(0), value: 60 },
      { ts: at(1), value: null },
      { ts: at(2), value: null },
    ])
  })

  it('HOSTCH-002 keeps only the timestamps present in both series', () => {
    const total = [
      { ts: at(0), value: 10 },
      { ts: at(1), value: 10 },
      { ts: at(3), value: 10 },
    ]
    // Same instant written with an offset still matches.
    const free = [
      { ts: '2026-10-29T12:01:00+02:00', value: 4 },
      { ts: at(2), value: 4 },
      { ts: at(3), value: 1 },
    ]
    expect(subtractSeries(total, free)).toEqual([
      { ts: at(1), value: 6 },
      { ts: at(3), value: 9 },
    ])
    expect(subtractSeries([], free)).toEqual([])
    // Exact matching drops buckets that are a few milliseconds apart.
    expect(subtractSeries([{ ts: '2026-10-29T10:00:00.844Z', value: 5 }], free)).toEqual([])
  })

  it('HOSTCH-002 pairs buckets offset by less than the tolerance, nearest first', () => {
    const total = [
      { ts: '2026-10-29T10:00:00.844Z', value: 100 },
      { ts: '2026-10-29T10:00:30.844Z', value: 100 },
      { ts: '2026-10-29T10:01:00.844Z', value: 100 },
    ]
    const available = [
      { ts: '2026-10-29T10:00:30.851Z', value: 70 },
      { ts: '2026-10-29T10:00:00.851Z', value: 60 },
      { ts: '2026-10-29T10:02:00.851Z', value: 10 },
      { ts: 'not a time', value: 1 },
    ]
    expect(subtractSeries(total, available, 15_000)).toEqual([
      { ts: '2026-10-29T10:00:00.844Z', value: 40 },
      { ts: '2026-10-29T10:00:30.844Z', value: 30 },
    ])
    expect(subtractSeries([{ ts: 'bad', value: 1 }], available, 15_000)).toEqual([])
    expect(subtractSeries(total, [])).toEqual([])
  })

  it('HOSTCH-003 turns a ratio into percent keeping gaps', () => {
    expect(
      toPercent([
        { ts: at(0), value: 0.25 },
        { ts: at(1), value: null },
        { ts: at(2), value: 1 },
      ]),
    ).toEqual([
      { ts: at(0), value: 25 },
      { ts: at(1), value: null },
      { ts: at(2), value: 100 },
    ])
  })

  it('HOSTCH-003 computes the used ratio only for a measured, non-empty total', () => {
    expect(usedRatio(200, 50)).toBe(0.75)
    expect(usedRatio(0, 0)).toBeNull()
    expect(usedRatio(null, 5)).toBeNull()
    expect(usedRatio(5, null)).toBeNull()
    expect(usedRatio(Number.NaN, 1)).toBeNull()
  })

  it('HOSTCH-002 breaks a tie between two buckets in favour of the earlier one', () => {
    const total = [{ ts: '2026-10-29T10:00:15Z', value: 10 }]
    const part = [
      { ts: '2026-10-29T10:00:30Z', value: 2 },
      { ts: '2026-10-29T10:00:00Z', value: 1 },
    ]
    expect(subtractSeries(total, part, 15_000)).toEqual([{ ts: total[0]?.ts, value: 9 }])
  })

  it('rounds an axis top up to a 1, 2, 2.5 or 5 tick', () => {
    expect(niceCeil(8.8)).toBe(10)
    expect(niceCeil(3.3)).toBe(5)
    expect(niceCeil(22)).toBe(25)
    expect(niceCeil(1.5)).toBe(2)
    expect(niceCeil(100)).toBe(100)
    expect(niceCeil(0)).toBe(1)
    expect(niceCeil(Number.NaN)).toBe(1)
  })
})
