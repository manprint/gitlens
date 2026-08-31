import { act, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { EChartsOption } from 'echarts'

import { AshChart } from './AshChart'
import { renderWithProviders } from '@/test/render'

const echartsMock = vi.hoisted(() => ({ render: vi.fn() }))

vi.mock('echarts-for-react', () => ({
  default: (props: {
    option: EChartsOption
    onEvents?: Record<string, (event: unknown) => void>
  }) => {
    echartsMock.render(props)
    return <div data-testid="mock-echarts" />
  },
}))

const series = [
  {
    name: 'CPU',
    points: [
      { ts: '2026-09-01T10:00:00Z', value: 1, samples: 10, ticks: 20 },
      { ts: '2026-09-01T10:01:00Z', value: null, samples: 0, ticks: 0 },
    ],
  },
]

beforeEach(() => {
  echartsMock.render.mockClear()
})

describe('AshChart', () => {
  it('UI-ASH-015 passes the built ASH option through the accessible wrapper', () => {
    renderWithProviders(<AshChart cpuCount={4} foldedCount={2} series={series} />)

    const rendered = echartsMock.render.mock.calls[0]?.[0] as { option?: EChartsOption }
    expect(rendered.option).toEqual(
      expect.objectContaining({ yAxis: { type: 'value', min: 0, name: 'avg active sessions' } }),
    )
    expect(
      screen.getByRole('img', { name: 'Average active sessions by wait event' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('table', { name: 'Average active sessions by wait event data' }),
    ).toHaveTextContent('Average active sessions')
  })

  it('UI-ASH-016 writes an absolute range after brushing', () => {
    const result = renderWithProviders(<AshChart series={series} />, {
      route: '/ash?range=1h',
    })
    const props = echartsMock.render.mock.calls[0]?.[0] as {
      onEvents?: Record<string, (event: unknown) => void>
    }

    act(() => {
      props.onEvents?.datazoom?.({
        startValue: '2026-09-01T10:00:00Z',
        endValue: '2026-09-01T10:01:00Z',
      })
    })

    const params = new URLSearchParams(result.router.state.location.search)
    expect(params.get('from')).toBe('2026-09-01T10:00:00.000Z')
    expect(params.get('to')).toBe('2026-09-01T10:01:00.000Z')
    expect(params.get('range')).toBeNull()
  })
})
