import { screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { EChartsOption } from 'echarts'

import { AshChart } from './AshChart'
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
    renderWithProviders(<AshChart foldedCount={2} series={series} />)

    const rendered = echartsMock.render.mock.calls[0]?.[0] as { option?: EChartsOption }
    expect(rendered.option).toEqual(
      expect.objectContaining({ yAxis: { type: 'value', min: 0, name: 'avg active backends' } }),
    )
    expect(
      screen.getByRole('img', { name: 'Average active backends by wait event' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('table', { name: 'Average active backends by wait event data' }),
    ).toHaveTextContent('Average active backends')
  })

  it('UI-ASH-016 has no wheel zoom and leaves the page range alone', () => {
    const result = renderWithProviders(<AshChart series={series} />, {
      route: '/ash?range=1h',
    })
    const props = echartsMock.render.mock.calls[0]?.[0] as {
      option?: EChartsOption
      onEvents?: Record<string, (event: unknown) => void>
    }

    expect((props.option?.dataZoom as { type: string }[]).map((zoom) => zoom.type)).toEqual([
      'slider',
    ])
    expect(props.onEvents?.datazoom).toBeUndefined()
    expect(result.router.state.location.search).toBe('?range=1h')
  })
})
