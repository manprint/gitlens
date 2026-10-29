import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import {
  ASH_PALETTE,
  buildAshGroupOption,
  buildAshOption,
  type AshChartSeries,
  isolatedPoints,
  OTHER_FOLDED_LABEL,
} from './ash.options'
import { timeAxisOption } from './timeAxis'

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
  it('gives every wait-event type its own colour, with BufferPin sharing Buffer', () => {
    const colours = Object.entries(ASH_PALETTE)
      .filter(([name]) => name !== 'BufferPin')
      .map(([, colour]) => colour)

    expect(new Set(colours).size).toBe(colours.length)
    expect(ASH_PALETTE.BufferPin).toBe(ASH_PALETTE.Buffer)
  })
})

describe('ASH time axis (plan 005 1.4)', () => {
  const at = Date.UTC(2026, 8, 1, 10, 0)

  it('ASHOPT-001 uses the shared time axis with short UTC labels', () => {
    const option = buildAshOption(series)
    expect(option.xAxis).toBe(timeAxisOption(60_000))
    const axis = option.xAxis as { axisLabel: { formatter: (value: number) => string } }
    expect(axis.axisLabel.formatter(Date.UTC(2026, 9, 29, 9, 41))).toBe('09:41')
  })

  it('ASHOPT-002 labels the slider with the short form', () => {
    const zoom = buildAshOption(series).dataZoom as {
      type: string
      labelFormatter?: (value: number) => string
    }[]
    expect(zoom.find((item) => item.type === 'slider')?.labelFormatter?.(at)).toBe('10:00')
  })

  it('ASHOPT-003 keeps the CPU-count line and the tooltip rows, with a UTC tooltip time', () => {
    const option = buildAshOption(series, { cpuCount: 4 })
    const cpu = optionSeries(option).find((item) => item.name === 'CPU') as {
      markLine?: { data: { yAxis: number; name: string }[] }
    }
    expect(cpu.markLine?.data[0]).toEqual(
      expect.objectContaining({ yAxis: 4, name: 'CPU count (4)' }),
    )
    const tooltip = (option.tooltip as { formatter: (params: unknown) => string }).formatter
    const html = tooltip([{ seriesName: 'CPU', value: ['2026-09-01T10:00:00Z', 1] }])
    expect(html).toContain('2026-09-01 10:00:00 UTC')
    expect(html).toContain(
      '<strong>CPU</strong>: 1.00 avg active backends (samples: 10, ticks: 20)',
    )
  })

  it('ASHOPT-010 builds one stack per group on its own y axis from zero', () => {
    const locks: AshChartSeries[] = [
      { name: 'LWLock', points },
      { name: 'Lock', points },
    ]
    const spanMs = 6 * 60 * 60 * 1_000
    const option = buildAshGroupOption(locks, { cpuCount: 4, stack: 'locks', spanMs })
    const lines = optionSeries(option)

    expect(lines.map(({ name }) => name)).toEqual(['Lock', 'LWLock'])
    expect(lines.every(({ stack }) => stack === 'locks')).toBe(true)
    expect(option.yAxis).toEqual({ type: 'value', min: 0, name: 'avg active backends' })
    expect(option.xAxis).toBe(timeAxisOption(spanMs))
    expect(option.legend).toEqual({ show: false })
    expect(lines.some(({ markLine }) => markLine !== undefined)).toBe(false)

    const cpu = buildAshGroupOption([{ name: 'CPU', points }], {
      cpuCount: 4,
      showCpuCount: true,
      stack: 'cpu',
      spanMs,
    })
    expect(JSON.stringify(optionSeries(cpu)[0]?.markLine)).toContain('CPU count (4)')
    expect(
      optionSeries(
        buildAshGroupOption([{ name: 'CPU', points }], { stack: 'cpu', showCpuCount: true }),
      )[0]?.markLine,
    ).toBeUndefined()
  })

  it('ASHOPT-012 marks isolated samples so sparse data stays visible', () => {
    const sparse = [
      { ts: '2026-09-01T10:00:00Z', value: null },
      { ts: '2026-09-01T10:01:00Z', value: 0.05 },
      { ts: '2026-09-01T10:02:00Z', value: null },
      { ts: '2026-09-01T10:03:00Z', value: 1 },
      { ts: '2026-09-01T10:04:00Z', value: 2 },
    ]
    expect([...isolatedPoints(sparse)]).toEqual([1])

    const line = optionSeries(
      buildAshGroupOption([{ name: 'Client', points: sparse }], { stack: 'client' }),
    )[0] as {
      showSymbol?: boolean
      symbolSize?: (value: unknown, params: { dataIndex?: number }) => number
    }
    expect(line.showSymbol).toBe(true)
    expect(line.symbolSize?.(null, { dataIndex: 1 })).toBeGreaterThan(0)
    expect(line.symbolSize?.(null, { dataIndex: 3 })).toBe(0)
    expect(line.symbolSize?.(null, {})).toBe(0)
  })

  it('ASHOPT-013 pins the time axis to the selected window', () => {
    const window = { from: new Date('2026-09-01T09:30:00Z'), to: new Date('2026-09-01T10:30:00Z') }
    const hour = 60 * 60 * 1_000
    for (const option of [
      buildAshOption(series, { window }),
      buildAshGroupOption(series, { stack: 'cpu', window }),
    ]) {
      expect(option.xAxis).toEqual({
        ...timeAxisOption(hour),
        min: window.from.getTime(),
        max: window.to.getTime(),
      })
    }
    expect(buildAshOption(series).xAxis).toBe(timeAxisOption(60 * 1_000))
  })

  it('ASHOPT-011 keeps the stacked option unchanged', () => {
    expect(buildAshOption(series, { cpuCount: 4, foldedCount: 2 })).toMatchSnapshot()
    expect(buildAshOption(series)).toMatchSnapshot()
  })

  it('ASHOPT-004 no option builder formats times with toISOString', () => {
    const dir = dirname(fileURLToPath(import.meta.url))
    for (const file of ['ash.options.ts', 'lag.options.ts', 'metric.options.ts', 'timeAxis.ts']) {
      expect(readFileSync(join(dir, file), 'utf8'), file).not.toContain('toISOString')
    }
  })
})
