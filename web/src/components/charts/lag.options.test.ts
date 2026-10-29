import { describe, expect, it } from 'vitest'

import type { AlignedReplicationSeries } from '@/lib/replication'

import { buildLagOption, LAG_METRICS } from './lag.options'
import { timeAxisOption } from './timeAxis'

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
    // Short UTC labels (plan 005 1.4): the ISO form overlapped on narrow charts.
    expect(xAxis.axisLabel?.formatter?.(Date.parse('2026-08-31T10:00:00Z'))).toBe('10:00')
  })

  it('UI-CLUS-014 is deterministic for the same input', () => {
    expect(buildLagOption(aligned)).toEqual(buildLagOption(aligned))
  })
})

describe('lag time axis (plan 005 1.4)', () => {
  it('LAGOPT-001 uses the shared time axis and a short slider label', () => {
    const option = buildLagOption(aligned)
    expect(option.xAxis).toBe(timeAxisOption(2 * 60_000))
    const zoom = option.dataZoom as { type: string; labelFormatter?: (value: number) => string }[]
    expect(
      zoom
        .find((item) => item.type === 'slider')
        ?.labelFormatter?.(Date.parse('2026-08-31T10:01:00Z')),
    ).toBe('10:01')
  })

  it('LAGOPT-002 shows the UTC time and each lag in seconds in the tooltip', () => {
    const tooltip = buildLagOption(aligned).tooltip as {
      confine: boolean
      formatter: (params: unknown) => string
    }
    expect(tooltip.confine).toBe(true)
    const html = tooltip.formatter([
      { seriesName: 'write_lag_sec', value: ['2026-08-31T10:00:00Z', 1.5] },
      { seriesName: 'flush_lag_sec', value: ['2026-08-31T10:00:00Z', null] },
    ])
    expect(html).toContain('2026-08-31 10:00:00 UTC')
    expect(html).toContain('write_lag_sec: <strong>1.5s</strong>')
    expect(html).toContain('flush_lag_sec: <strong>—</strong>')
  })
})
