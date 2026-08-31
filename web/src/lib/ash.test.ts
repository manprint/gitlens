import { describe, expect, it } from 'vitest'

import {
  avgActiveSessions,
  foldOther,
  isUnderSampled,
  orderGroups,
  toStackedSeries,
  topQueries,
  totalSamples,
  type AshBucket,
  type AshSeries,
  type AshTopEntry,
} from './ash'

const bucket = (
  ts: string,
  samples: number,
  wait_event_type?: string,
  overrides: Partial<AshBucket> = {},
): AshBucket => ({
  avg_active_sessions: samples,
  samples,
  ticks: samples,
  ts,
  ...(wait_event_type === undefined ? {} : { wait_event_type }),
  ...overrides,
})

const series = (name: string, values: (number | null)[]): AshSeries => ({
  name,
  points: values.map((value, index) => ({
    ts: `2026-08-31T12:0${index}:00Z`,
    value,
  })),
})

describe('ASH derivation helpers', () => {
  it('UI-ASH-001 makes a missing group zero in a sampled window', () => {
    const output = toStackedSeries(
      [
        bucket('2026-08-31T12:00:00Z', 2, 'CPU'),
        bucket('2026-08-31T12:00:00Z', 3, 'IO'),
        bucket('2026-08-31T12:01:00Z', 4, 'CPU'),
      ],
      'wait_event_type',
    )

    expect(output.find(({ name }) => name === 'IO')?.points.map(({ value }) => value)).toEqual([
      3, 0,
    ])
  })

  it('UI-ASH-002 represents a missing window as a gap, not zero', () => {
    const output = toStackedSeries(
      [
        bucket('2026-08-31T12:00:00Z', 2, 'CPU'),
        bucket('2026-08-31T12:01:00Z', 2, 'CPU'),
        bucket('2026-08-31T12:03:00Z', 2, 'CPU'),
      ],
      'wait_event_type',
    )

    expect(output[0]?.points.map(({ value }) => value)).toEqual([2, 2, null, 2])
  })

  it('UI-ASH-003 orders CPU, waits by descending total, and other last', () => {
    const ordered = orderGroups([
      series('other', [100]),
      series('IO', [2]),
      series('CPU', [1]),
      series('Lock', [5]),
    ])

    expect(ordered.map(({ name }) => name)).toEqual(['CPU', 'Lock', 'IO', 'other'])
  })

  it('UI-ASH-004 folds without changing the total sample values', () => {
    const input = [series('CPU', [1, 2]), series('IO', [3, 4]), series('Lock', [5, 6])]
    const result = foldOther(input, 2)

    expect(result.series).toEqual([
      input[0],
      {
        name: 'other',
        points: [
          { ts: '2026-08-31T12:00:00Z', value: 8 },
          { ts: '2026-08-31T12:01:00Z', value: 10 },
        ],
      },
    ])
    expect(
      result.series
        .flatMap(({ points }) => points)
        .reduce((total, point) => total + (point.value ?? 0), 0),
    ).toBe(21)
  })

  it('UI-ASH-005 reports how many series were folded', () => {
    expect(
      foldOther(
        [series('CPU', [1]), series('IO', [1]), series('Lock', [1]), series('LWLock', [1])],
        2,
      ).folded,
    ).toBe(3)
  })

  it('UI-ASH-006 returns null for ticks=0', () => {
    expect(avgActiveSessions(10, 0)).toBeNull()
  })

  it('UI-ASH-007 marks fewer than 60 samples as under-sampled', () => {
    expect(isUnderSampled([{ samples: 59 }])).toBe(true)
    expect(isUnderSampled([{ samples: 60 }])).toBe(false)
    expect(totalSamples([{ samples: 20 }, { samples: 40 }])).toBe(60)
  })

  it('UI-ASH-008 keeps a large queryid as a string', () => {
    const entries = [
      {
        avg_active_sessions: 1,
        query_text: 'select 1',
        queryid: '9007199254740993',
        samples: 10,
        ticks: 10,
      },
    ] as AshTopEntry[]

    expect(topQueries(entries)[0]?.queryid).toBe('9007199254740993')
  })

  it('UI-ASH-009 produces stable ordering for identical inputs', () => {
    const input = [series('IO', [2]), series('CPU', [1]), series('Lock', [2])]

    expect(orderGroups(input)).toEqual(orderGroups(input))
  })
})
