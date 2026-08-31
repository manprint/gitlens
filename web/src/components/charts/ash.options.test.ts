import { describe, expect, it } from 'vitest'

import { ASH_PALETTE, buildAshOption, type AshChartSeries, OTHER_FOLDED_LABEL } from './ash.options'

const points = [
  { ts: '2026-09-01T10:00:00Z', value: 1, samples: 10, ticks: 20 },
  { ts: '2026-09-01T10:01:00Z', value: null, samples: 0, ticks: 0 },
]

const series: AshChartSeries[] = [
  { name: 'other', points },
  { name: 'IO', points },
  { name: 'CPU', points },
  { name: 'Lock', points },
]

interface AshSeriesOption {
  name?: string
  stack?: string
  connectNulls?: boolean
  data?: unknown[]
  itemStyle?: { color?: string }
  markLine?: unknown
}

function optionSeries(option: ReturnType<typeof buildAshOption>): AshSeriesOption[] {
  return option.series as AshSeriesOption[]
}

describe('buildAshOption', () => {
  it('UI-ASH-010 stacks every group on one time axis', () => {
    const option = buildAshOption(series)
    const plotted = optionSeries(option)

    expect(plotted.map((item) => item.stack)).toEqual(['ash', 'ash', 'ash', 'ash'])
    expect(plotted.every((item) => item.data?.length === 2)).toBe(true)
    expect((option.xAxis as { type?: string }).type).toBe('time')
  })

  it('UI-ASH-011 keeps unsampled gaps disconnected', () => {
    expect(optionSeries(buildAshOption(series)).every((item) => item.connectNulls === false)).toBe(
      true,
    )
    expect(optionSeries(buildAshOption(series))[0]?.data?.[1]).toEqual([
      '2026-09-01T10:01:00Z',
      null,
    ])
  })

  it('UI-ASH-012 puts other last and exposes its folded label and count', () => {
    const option = buildAshOption(series, { foldedCount: 3 })
    const plotted = optionSeries(option)
    const tooltip = (option.tooltip as { formatter?: (params: unknown) => string }).formatter

    expect(plotted.at(-1)?.name).toBe(OTHER_FOLDED_LABEL)
    expect((option.legend as { data?: string[] }).data?.at(-1)).toBe(OTHER_FOLDED_LABEL)
    expect(
      tooltip?.([{ seriesName: OTHER_FOLDED_LABEL, value: ['2026-09-01T10:00:00Z', 1] }]),
    ).toContain('3 groups folded')
  })

  it('UI-ASH-013 maps each known wait type to its fixed palette token', () => {
    const option = buildAshOption(series)
    const plotted = optionSeries(option)

    for (const item of plotted) {
      const sourceName = item.name === OTHER_FOLDED_LABEL ? 'other' : item.name
      expect(item.itemStyle?.color).toBe(ASH_PALETTE[sourceName as keyof typeof ASH_PALETTE])
    }
  })

  it('UI-ASH-014 omits the CPU reference line when the core count is unknown', () => {
    expect(
      optionSeries(buildAshOption(series, { cpuCount: null })).every((item) => !item.markLine),
    ).toBe(true)
    expect(
      optionSeries(buildAshOption(series, { cpuCount: 4 })).some((item) => item.markLine),
    ).toBe(true)
  })
})
