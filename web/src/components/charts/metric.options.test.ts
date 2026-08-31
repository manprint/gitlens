import { describe, expect, it } from 'vitest'

import { buildMetricOption } from './metric.options'

describe('buildMetricOption', () => {
  it('UI-INST-022 preserves nulls, disables interpolation, and annotates resets', () => {
    const option = buildMetricOption([
      {
        name: 'transactions per second',
        points: [
          { ts: '2026-01-01T00:00:00Z', value: 1 },
          { ts: '2026-01-01T00:01:00Z', value: null },
          { ts: '2026-01-01T00:02:00Z', value: 2 },
        ],
      },
    ])
    const series = option.series as {
      connectNulls: boolean
      data: [string, number | null][]
      markPoint?: { data: { name: string }[] }
    }[]

    expect(series[0]?.data).toEqual([
      ['2026-01-01T00:00:00Z', 1],
      ['2026-01-01T00:01:00Z', null],
      ['2026-01-01T00:02:00Z', 2],
    ])
    expect(series[0]?.connectNulls).toBe(false)
    expect(series[0]?.markPoint?.data).toEqual([expect.objectContaining({ name: 'Counter reset' })])
  })
})
