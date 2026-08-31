import { describe, expect, it } from 'vitest'

import type { AlignedReplicationSeries } from '@/lib/replication'

import { buildLagOption, LAG_METRICS } from './lag.options'

const aligned: AlignedReplicationSeries = {
  timestamps: ['2026-08-31T10:00:00Z', '2026-08-31T10:01:00Z', '2026-08-31T10:02:00Z'],
  write_lag_sec: [1, null, 3],
  flush_lag_sec: [2, 4, null],
  replay_lag_sec: [3, 5, 6],
}

interface LagSeriesOption {
  name?: string
  data?: unknown[]
  connectNulls?: boolean
}

function optionSeries(option: ReturnType<typeof buildLagOption>): LagSeriesOption[] {
  return option.series as LagSeriesOption[]
}

describe('buildLagOption', () => {
  it('UI-CLUS-010 contains one series per metric with the exact names', () => {
    expect(optionSeries(buildLagOption(aligned)).map((series) => series.name)).toEqual(LAG_METRICS)
  })

  it('UI-CLUS-011 sets connectNulls to false for every metric', () => {
    expect(optionSeries(buildLagOption(aligned)).map((series) => series.connectNulls)).toEqual([
      false,
      false,
      false,
    ])
  })

  it("UI-CLUS-012 keeps a null datum null in the option's data array", () => {
    const series = optionSeries(buildLagOption(aligned))

    expect(series[0]?.data?.[1]).toEqual(['2026-08-31T10:01:00Z', null])
    expect(series[1]?.data?.[2]).toEqual(['2026-08-31T10:02:00Z', null])
  })

  it('UI-CLUS-013 uses a time axis with UTC formatting', () => {
    const option = buildLagOption(aligned)
    const xAxis = option.xAxis as {
      type?: string
      axisLabel?: { formatter?: (value: number) => string }
    }

    expect(xAxis.type).toBe('time')
    expect(xAxis.axisLabel?.formatter?.(Date.parse('2026-08-31T10:00:00Z'))).toBe(
      '2026-08-31T10:00:00.000Z',
    )
  })

  it('UI-CLUS-014 is deterministic for the same input', () => {
    expect(buildLagOption(aligned)).toEqual(buildLagOption(aligned))
  })
})
