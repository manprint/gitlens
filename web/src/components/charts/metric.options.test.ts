import { afterEach, describe, expect, it } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'

import {
  axisScale,
  buildMetricOption,
  formatAxisValue,
  formatMetricValue,
  legendRows,
  mixedUnitTooltip,
  tooltipHtml,
  withSecondaryAxis,
} from './metric.options'

interface LineSeries {
  connectNulls: boolean
  showSymbol: boolean
  data: [string, number | null][]
  markPoint?: unknown
  markLine?: unknown
}

interface AxisOption {
  axisLabel: {
    formatter: (value: number) => string
    hideOverlap: boolean
    showMinLabel: boolean
    showMaxLabel: boolean
  }
}

const start = Date.UTC(2026, 9, 29, 9, 41)
const MINUTE = 60_000
const ts = (offset: number) => new Date(start + offset).toISOString()

/** `values` one minute apart from 09:41 UTC; null is a missing bucket. */
const metric = (name: string, values: readonly (number | null)[], stepMs = MINUTE) => ({
  name,
  points: values.map((value, index) => ({ ts: ts(index * stepMs), value })),
})

const seriesOf = (option: ReturnType<typeof buildMetricOption>) => option.series as LineSeries[]
const xAxisOf = (option: ReturnType<typeof buildMetricOption>) => option.xAxis as AxisOption

/** A series with `resets` isolated null buckets between values. */
const withResets = (resets: number) =>
  metric(
    'tps',
    Array.from({ length: resets * 2 + 1 }, (_, index) => (index % 2 === 0 ? index : null)),
  )

describe('buildMetricOption', () => {
  it('UI-INST-022 preserves nulls and disables interpolation, with no reset marker', () => {
    const option = buildMetricOption([metric('transactions per second', [1, null, 2])])
    const [series] = seriesOf(option)

    expect(series?.data).toEqual([
      [ts(0), 1],
      [ts(MINUTE), null],
      [ts(2 * MINUTE), 2],
    ])
    expect(series?.connectNulls).toBe(false)
    expect(series?.markLine).toBeUndefined()
  })

  it('METOPT-001 formats the x axis by span: HH:mm for 1 h, the date for 48 h', () => {
    const hour = buildMetricOption([metric('a', [1, 2], 60 * MINUTE)])
    expect(xAxisOf(hour).axisLabel.formatter(start)).toBe('09:41')

    const twoDays = buildMetricOption([metric('a', [1, 2], 48 * 60 * MINUTE)])
    expect(xAxisOf(twoDays).axisLabel.formatter(start)).toBe('Oct 29 09:41')
  })

  it('METOPT-002 hides overlapping axis labels and the edge labels', () => {
    const { axisLabel } = xAxisOf(buildMetricOption([metric('a', [1, 2])]))
    expect(axisLabel.hideOverlap).toBe(true)
    expect(axisLabel.showMinLabel).toBe(false)
    expect(axisLabel.showMaxLabel).toBe(false)
  })

  it('METOPT-003 draws no markPoint on any series', () => {
    const option = buildMetricOption([withResets(2), metric('other', [1, null, 3])])
    for (const series of seriesOf(option)) expect(series.markPoint).toBeUndefined()
  })

  it('METOPT-004 draws no line at a gap or counter reset: every mark is in the legend', () => {
    for (const resets of [2, 13]) {
      const [series] = seriesOf(buildMetricOption([withResets(resets)]))
      expect(series?.markLine).toBeUndefined()
    }
  })

  it('METOPT-006 confines an axis tooltip to the chart', () => {
    const tooltip = buildMetricOption([metric('a', [1])]).tooltip as {
      confine: boolean
      trigger: string
      axisPointer: { type: string }
    }
    expect(tooltip.confine).toBe(true)
    expect(tooltip.trigger).toBe('axis')
    expect(tooltip.axisPointer.type).toBe('line')
  })

  it('METOPT-007 renders tooltip rows with the UTC time and a dash for a missing value', () => {
    const html = tooltipHtml(
      start,
      [
        { seriesName: 'commits', value: 12.5 },
        { seriesName: 'rollbacks <b>', value: null },
      ],
      'count',
    )
    expect(html).toContain('2026-10-29 09:41:00 UTC')
    expect(html).toContain('commits: <strong>12.5</strong>')
    expect(html).toContain('rollbacks &#60;b&#62;: <strong>—</strong>')

    const formatter = (
      buildMetricOption([metric('commits', [1])], { unit: 'count' }).tooltip as {
        formatter: (params: unknown) => string
      }
    ).formatter
    expect(
      formatter([
        { seriesName: 'commits', marker: '', axisValue: start, value: [ts(0), 3] },
        { seriesName: 'rollbacks', axisValue: start, value: [ts(0), null] },
      ]),
    ).toBe(
      '<div>2026-10-29 09:41:00 UTC</div><div>commits: <strong>3</strong></div><div>rollbacks: <strong>—</strong></div>',
    )
    // A single param, a string axis value, or only the data timestamp still give the time.
    expect(formatter({ seriesName: 'commits', axisValue: ts(0), value: 4 })).toContain(
      '09:41:00 UTC</div><div>commits: <strong>4</strong>',
    )
    expect(formatter([{ seriesName: 'commits', value: [ts(0), 5] }])).toContain('09:41:00 UTC')
    expect(formatter([{ seriesName: 'commits', value: [start, 6] }])).toContain('09:41:00 UTC')
    // An unparseable time drops the time row instead of throwing.
    expect(formatter([{ seriesName: 'commits', axisValue: 'not a time', value: 7 }])).toBe(
      '<div>commits: <strong>7</strong></div>',
    )
    expect(xAxisOf(buildMetricOption([metric('a', [1])])).axisLabel.formatter(NaN)).toBe('')
  })

  it('METOPT-008 labels the slider with the short axis form', () => {
    const dataZoom = buildMetricOption([metric('a', [1, 2])]).dataZoom as {
      type: string
      labelFormatter?: (value: number) => string
    }[]
    const slider = dataZoom.find((zoom) => zoom.type === 'slider')
    expect(slider?.labelFormatter?.(start)).toBe('09:41')
  })

  it('METOPT-009 keeps gaps and hides symbols', () => {
    const [series] = seriesOf(buildMetricOption([metric('a', [1, null, 2])]))
    expect(series?.connectNulls).toBe(false)
    expect(series?.showSymbol).toBe(false)
  })

  it('METOPT-010 formats the y axis with the unit formatter', () => {
    const yAxis = buildMetricOption([metric('a', [1])], { unit: 'bytes', yAxisName: 'WAL' })
      .yAxis as { name: string; axisLabel: { formatter: (value: number) => string } }
    expect(yAxis.axisLabel.formatter(1536)).toBe('1.5 KiB')
    expect(yAxis.name).toBe('WAL')
    expect(tooltipHtml(start, [{ seriesName: 'r', value: 0.25 }], 'ratio')).toContain('25.0%')
    expect(tooltipHtml(start, [{ seriesName: 'p', value: 25 }], 'percent')).toContain('25.0%')
    expect(tooltipHtml(start, [{ seriesName: 's', value: 90 }], 'seconds')).toContain('1m 30s')
    expect(tooltipHtml(start, [{ seriesName: 'm', value: 5 }], 'ms')).toContain('5')
  })
})

describe('withSecondaryAxis', () => {
  const lines = [
    { name: 'Temporary files', points: [{ ts: '2026-01-01T00:00:00Z', value: 2 }] },
    { name: 'Temporary bytes', points: [{ ts: '2026-01-01T00:00:00Z', value: 2048 }] },
  ]

  it('OVC-AXIS-001 moves the lines of a second unit to a right axis in that unit', () => {
    const option = withSecondaryAxis(buildMetricOption(lines, { unit: 'count' }), [
      'count',
      'bytes',
    ])
    const axes = option.yAxis as { axisLabel?: { formatter?: (value: number) => string } }[]
    const series = option.series as { yAxisIndex?: number }[]

    expect(axes).toHaveLength(2)
    expect(axes[1]?.axisLabel?.formatter?.(2048)).toBe('2.0 KiB')
    expect(series[0]?.yAxisIndex).toBeUndefined()
    expect(series[1]?.yAxisIndex).toBe(1)
  })

  it('OVC-AXIS-002 leaves a single-unit chart unchanged', () => {
    const option = buildMetricOption(lines, { unit: 'count' })
    expect(withSecondaryAxis(option, ['count', 'count'])).toBe(option)
    expect(withSecondaryAxis(option, [])).toBe(option)
  })

  it('OVC-AXIS-003 formats each tooltip row in the unit of its line', () => {
    const html = mixedUnitTooltip(['count', 'bytes'])([
      {
        axisValue: '2026-01-01T00:00:00Z',
        seriesIndex: 0,
        seriesName: 'Temporary files',
        value: ['2026-01-01T00:00:00Z', 2],
      },
      {
        axisValue: '2026-01-01T00:00:00Z',
        seriesIndex: 1,
        seriesName: 'Temporary bytes',
        value: ['2026-01-01T00:00:00Z', 2048],
      },
    ])
    expect(html).toContain('Temporary files: <strong>2</strong>')
    expect(html).toContain('Temporary bytes: <strong>2.0 KiB</strong>')
    expect(html).toContain('2026-01-01')
  })
})

describe('formatMetricValue count', () => {
  it('OVC-AXIS-004 keeps two significant digits below 1 so small ticks stay distinct', () => {
    const ticks = [0, 0.002, 0.004, 0.006, 0.008, 0.01].map((value) =>
      formatMetricValue(value, 'count'),
    )
    expect(ticks).toEqual(['0', '0.002', '0.004', '0.006', '0.008', '0.01'])
    expect(formatMetricValue(0.12345, 'count')).toBe('0.12')
    expect(formatMetricValue(1234.567, 'count')).toBe('1,234.57')
    expect(formatMetricValue(-0.0042, 'count')).toBe('-0.0042')
  })
})

describe('legend', () => {
  it('OVC-AXIS-005 wraps the legend and moves the plot below its rows', () => {
    const point = [{ ts: '2026-01-01T00:00:00Z', value: 1 }]
    const two = buildMetricOption([
      { name: 'Commits', points: point },
      { name: 'Rollbacks', points: point },
    ])
    const four = buildMetricOption(
      ['Inserted', 'Updated', 'HOT updated', 'Deleted'].map((name) => ({ name, points: point })),
    )
    expect((two.legend as { type: string }).type).toBe('plain')
    expect((two.grid as { top: number }).top).toBe(40)
    expect(legendRows(['Inserted', 'Updated', 'HOT updated', 'Deleted'])).toBe(2)
    expect((four.grid as { top: number }).top).toBe(62)
    expect(legendRows([])).toBe(1)
    expect(legendRows(['A very long series name that fills a whole row', 'Next'])).toBe(2)
  })
})

describe('axis scale and labels (review 2026-11-21)', () => {
  afterEach(async () => {
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('AXIS-NICE-001 steps byte axes by binary units and duration axes by clock units', () => {
    expect(axisScale('bytes', [0, 3.3 * 1024 ** 2])).toEqual({
      interval: 1024 ** 2,
      max: 4 * 1024 ** 2,
    })
    expect(axisScale('bytes', [146.5 * 1024])).toEqual({ interval: 32 * 1024, max: 160 * 1024 })
    expect(axisScale('ms', [5 * 60_000])).toEqual({ interval: 60_000, max: 300_000 })
    expect(axisScale('seconds', [95])).toEqual({ interval: 30, max: 120 })
  })

  it('AXIS-NICE-002 keeps the automatic scale for counts, empty, zero or negative data', () => {
    expect(axisScale('count', [12])).toEqual({})
    expect(axisScale('bytes', [])).toEqual({})
    expect(axisScale('bytes', [0, null])).toEqual({})
    expect(axisScale('bytes', [-5, 10])).toEqual({})
    expect(axisScale(undefined, [10])).toEqual({})
  })

  it('AXIS-NICE-003 the primary and secondary axes take the scale of their own lines', () => {
    const ts = '2026-11-21T00:00:00Z'
    const option = withSecondaryAxis(
      buildMetricOption(
        [
          { name: 'files', points: [{ ts, value: 0.2 }] },
          { name: 'bytes', points: [{ ts, value: 3 * 1024 ** 2 }] },
        ],
        { unit: 'count' },
      ),
      ['count', 'bytes'],
    )
    const [primary, secondary] = option.yAxis as Record<string, unknown>[]
    expect(primary).not.toHaveProperty('interval')
    expect(primary).not.toHaveProperty('minInterval')
    expect(secondary).toMatchObject({ interval: 1024 ** 2, max: 3 * 1024 ** 2 })
  })

  it('AXIS-NICE-005 a count axis of whole numbers never ticks between them', () => {
    const ts = '2026-11-21T00:00:00Z'
    const whole = buildMetricOption([{ name: 'checkpoints', points: [{ ts, value: 2 }] }], {
      unit: 'count',
    })
    const rate = buildMetricOption([{ name: 'rows/s', points: [{ ts, value: 1.5 }] }], {
      unit: 'count',
    })
    expect(whole.yAxis).toMatchObject({ minInterval: 1 })
    expect(rate.yAxis).not.toHaveProperty('minInterval')
  })

  it("AXIS-NICE-004 axis numbers follow the console language and drop a whole percent's decimal", async () => {
    expect(formatAxisValue(100, 'percent')).toBe('100%')
    expect(formatAxisValue(0.125, 'ratio')).toBe('12.5%')
    await setServerLanguage('it')
    expect(formatMetricValue(7000, 'count')).toBe('7000')
    expect(formatMetricValue(12345.5, 'count')).toBe('12.345,5')
    expect(formatMetricValue(0.25, 'count')).toBe('0,25')
    expect(formatAxisValue(12.5, 'percent')).toBe('12,5%')
  })
})
