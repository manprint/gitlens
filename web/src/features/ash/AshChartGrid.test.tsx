import { fireEvent, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { EChartsOption } from 'echarts'

import type { AshChartSeries } from '@/components/charts/ash.options'
import { groupSeries } from '@/lib/ashGroups'
import { renderWithProviders } from '@/test/render'

const mocks = vi.hoisted(() => ({
  chart: vi.fn(),
  connect: vi.fn(),
}))

vi.mock('@/components/charts/echarts', () => ({ echarts: { connect: mocks.connect } }))

vi.mock('echarts-for-react/esm/core', () => ({
  default: (props: {
    option: EChartsOption
    onChartReady?: (instance: { group: string }) => void
  }) => {
    mocks.chart(props)
    const instance = { group: '' }
    props.onChartReady?.(instance)
    mocks.chart.mock.lastCall?.push(instance)
    return <div data-testid="mock-echarts" />
  },
}))

import { ASH_CHART_GROUP, AshChartGrid } from './AshChartGrid'

const points = (...values: (number | null)[]) =>
  values.map((value, index) => ({ ts: `2026-10-29T10:0${index}:00Z`, value }))

const series: AshChartSeries[] = [
  { name: 'CPU', points: points(2, 2) },
  { name: 'Client', points: points(1, 1) },
  { name: 'Lock', points: points(0.5, 0.5) },
  { name: 'LWLock', points: points(0.5, 0.5) },
]

const range = { from: new Date('2026-10-29T10:00:00Z'), to: new Date('2026-10-29T11:00:00Z') }

function renderGrid(input = series) {
  return renderWithProviders(
    <AshChartGrid
      cpuCount={4}
      foldedCount={0}
      groupBy="wait_event_type"
      groups={groupSeries(input, 'wait_event_type')}
      range={range}
      stepSeconds={20}
    />,
    { route: '/instances/instance-1/ash?range=1h' },
  )
}

type ChartCall = [{ option: EChartsOption }, { group: string }]
const chartCalls = () => mocks.chart.mock.calls as unknown as ChartCall[]
const seriesNames = (option: EChartsOption) =>
  (option.series as { name: string }[]).map(({ name }) => name)

beforeEach(() => {
  mocks.chart.mockReset()
  mocks.connect.mockReset()
})

describe('AshChartGrid', () => {
  it('ASHGRID-001 renders one card per non-empty group with its share', () => {
    renderGrid()

    const cards = screen.getAllByRole('region')
    expect(cards.map((card) => within(card).getByRole('heading').textContent)).toEqual([
      'CPU',
      'Locks and contention',
      'Client and inter-process',
    ])
    expect(cards[0]).toHaveTextContent('avg 2.00 active backends · 50% of all waits')
    expect(cards[1]).toHaveTextContent('avg 1.00 active backends · 25% of all waits')
    expect(cards[2]).toHaveTextContent('avg 1.00 active backends · 25% of all waits')
    expect(
      within(cards[1]!).getByRole('list', { name: 'Locks and contention legend' }),
    ).toHaveTextContent('Lock — Waiting for a heavyweight lock held by another transaction')
    const options = chartCalls().map(([props]) => props.option)
    expect(options.map(seriesNames)).toEqual([['CPU'], ['Lock', 'LWLock'], ['Client']])
    expect(JSON.stringify(options[0]?.series)).toContain('CPU count (4)')
    expect(JSON.stringify(options[1]?.series)).not.toContain('CPU count')
  })

  it('ASHGRID-002 joins every chart to one connected group', () => {
    renderGrid()

    expect(chartCalls()).toHaveLength(3)
    expect(chartCalls().every(([, instance]) => instance.group === ASH_CHART_GROUP)).toBe(true)
    expect(mocks.connect).toHaveBeenCalledWith(ASH_CHART_GROUP)
    const xAxes = chartCalls().map(([props]) => props.option.xAxis)
    // The theme resolver copies the option, so the axes are equal, not identical.
    expect(new Set(xAxes.map((axis) => JSON.stringify(axis))).size).toBe(1)
  })

  it('ASHGRID-003 a legend row shows its series alone in its own chart only', () => {
    renderGrid()

    fireEvent.click(screen.getByRole('button', { name: /^.?LWLock —/u }))
    const last = chartCalls()
      .slice(-3)
      .map(([props]) => seriesNames(props.option))
    expect(last).toEqual([['CPU'], ['LWLock'], ['Client']])

    fireEvent.click(screen.getByRole('button', { name: /^.?LWLock —/u }))
    expect(seriesNames(chartCalls().at(-2)![0].option)).toEqual(['Lock', 'LWLock'])
  })

  it('ASHGRID-004 colours wait events apart and describes them by their type', () => {
    renderWithProviders(
      <AshChartGrid
        eventTypes={
          new Map([
            ['WALWrite', 'LWLock'],
            ['BufferMapping', 'LWLock'],
          ])
        }
        foldedCount={0}
        groupBy="wait_event"
        groups={groupSeries(
          [
            { name: 'WALWrite', points: points(1) },
            { name: 'BufferMapping', points: points(1) },
          ],
          'wait_event',
          new Map([
            ['WALWrite', 'LWLock'],
            ['BufferMapping', 'LWLock'],
          ]),
        )}
        range={range}
        stepSeconds={20}
      />,
      { route: '/' },
    )

    const legend = screen.getByRole('list', { name: 'Locks and contention legend' })
    expect(legend).toHaveTextContent('WALWrite — LWLock: Waiting for an internal lightweight lock')
    const swatches = within(legend).getAllByTestId('ash-legend-swatch')
    expect(new Set(swatches.map((swatch) => swatch.style.backgroundColor)).size).toBe(2)
  })
})
