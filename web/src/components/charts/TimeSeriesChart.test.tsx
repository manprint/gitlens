import { act, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { EChartsOption } from 'echarts'

import { buildLagOption } from './lag.options'
import { TimeSeriesChart, type TimeSeriesChartProps } from './TimeSeriesChart'
import { renderWithProviders } from '@/test/render'

const echartsMock = vi.hoisted(() => ({ render: vi.fn() }))

vi.mock('echarts-for-react/esm/core', () => ({
  default: (props: {
    option: EChartsOption
    onEvents?: Record<string, (event: unknown) => void>
  }) => {
    echartsMock.render(props)
    return <div data-testid="mock-echarts" />
  },
}))

const points = [
  { ts: '2026-08-31T10:00:00Z', value: 1 },
  { ts: '2026-08-31T10:01:00Z', value: null },
]
const option = buildLagOption({
  timestamps: points.map((point) => point.ts),
  write_lag_sec: [1, null],
  flush_lag_sec: [2, null],
  replay_lag_sec: [3, null],
})
const chartProps: TimeSeriesChartProps = {
  ariaLabel: 'Replication lag for primary to standby',
  option,
  series: [
    { name: 'write_lag_sec', points },
    { name: 'flush_lag_sec', points },
    { name: 'replay_lag_sec', points },
  ],
}

beforeEach(() => {
  echartsMock.render.mockClear()
})

describe('TimeSeriesChart', () => {
  it('UI-CLUS-015 passes the built option to the chart library', () => {
    renderWithProviders(<TimeSeriesChart {...chartProps} />)

    expect(echartsMock.render).toHaveBeenCalledWith(expect.objectContaining({ option }))
  })

  it('UI-CLUS-016 lists every plotted point in the hidden data table', () => {
    renderWithProviders(<TimeSeriesChart {...chartProps} />)

    const table = screen.getByRole('table', { name: 'Replication lag for primary to standby data' })
    expect(table).toHaveTextContent('write_lag_sec')
    expect(table).toHaveTextContent('2026-08-31T10:00:00Z')
    expect(table).toHaveTextContent('Unknown')
    expect(within(table).getAllByRole('row')).toHaveLength(7)
  })

  it('UI-CLUS-017 writes an absolute range to the URL after a brush selection', () => {
    const result = renderWithProviders(<TimeSeriesChart {...chartProps} />, {
      route: '/clusters/9007199254740993?range=1h',
    })
    const props = echartsMock.render.mock.calls[0]?.[0] as {
      onEvents?: Record<string, (event: unknown) => void>
    }
    const datazoom = props.onEvents?.datazoom
    expect(datazoom).toBeDefined()

    act(() => {
      datazoom?.({
        startValue: '2026-08-31T10:00:00Z',
        endValue: '2026-08-31T10:01:00Z',
      })
    })

    const params = new URLSearchParams(result.router.state.location.search)
    expect(params.get('from')).toBe('2026-08-31T10:00:00.000Z')
    expect(params.get('to')).toBe('2026-08-31T10:01:00.000Z')
    expect(params.get('range')).toBeNull()
  })

  it('ignores malformed brush payloads and accepts numeric batch bounds', () => {
    const result = renderWithProviders(<TimeSeriesChart {...chartProps} />, {
      route: '/clusters/9007199254740993?range=1h',
    })
    const props = echartsMock.render.mock.calls[0]?.[0] as {
      onEvents?: Record<string, (event: unknown) => void>
    }
    const datazoom = props.onEvents?.datazoom
    expect(datazoom).toBeDefined()

    const from = Date.parse('2026-08-31T10:00:00Z')
    const to = Date.parse('2026-08-31T10:01:00Z')
    act(() => {
      datazoom?.({ batch: [{ startValue: from, endValue: to }] })
    })
    const changedSearch = result.router.state.location.search

    for (const event of [
      null,
      {},
      { batch: [] },
      { batch: [1] },
      { startValue: 'not-a-date', endValue: '2026-08-31T10:01:00Z' },
      { startValue: to, endValue: from },
      { startValue: Number.NaN, endValue: to },
      { startValue: {}, endValue: to },
    ]) {
      act(() => datazoom?.(event))
    }

    expect(result.router.state.location.search).toBe(changedSearch)
  })
  it('keeps the data table outside the image role, where screen readers reach it', () => {
    renderWithProviders(<TimeSeriesChart {...chartProps} />)

    const image = screen.getByRole('img', { name: 'Replication lag for primary to standby' })
    expect(within(image).queryByRole('table')).not.toBeInTheDocument()
    expect(
      screen.getByRole('table', { name: 'Replication lag for primary to standby data' }),
    ).toBeInTheDocument()
  })

  it('passes a theme built from the page tokens and rebuilds it when the theme changes', async () => {
    renderWithProviders(<TimeSeriesChart {...chartProps} />)
    const first = echartsMock.render.mock.calls.at(-1)?.[0] as { theme?: object }
    expect(first.theme).toMatchObject({ backgroundColor: 'transparent' })

    await act(async () => {
      document.documentElement.classList.add('dark')
      await Promise.resolve()
    })
    const second = echartsMock.render.mock.calls.at(-1)?.[0] as { theme?: object }
    document.documentElement.classList.remove('dark')

    expect(second.theme).not.toBe(first.theme)
  })
})
