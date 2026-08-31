import { act, fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { EChartsOption } from 'echarts'

import { renderWithProviders } from '@/test/render'

const mocks = vi.hoisted(() => ({
  ash: vi.fn(),
  ashTop: vi.fn(),
  chart: vi.fn(),
}))

vi.mock('@/api/queries', () => ({
  useAsh: mocks.ash,
  useAshTop: mocks.ashTop,
}))

vi.mock('echarts-for-react', () => ({
  default: (props: {
    option: EChartsOption
    onEvents?: Record<string, (event: unknown) => void>
  }) => {
    mocks.chart(props)
    return <div data-testid="mock-echarts" />
  },
}))

import { AshPage } from './AshPage'

const ashResponse = {
  buckets: [
    {
      avg_active_sessions: 1.5,
      samples: 15,
      ticks: 10,
      ts: '2026-09-01T10:00:00Z',
      wait_event: 'relation',
      wait_event_type: 'Lock',
    },
    {
      avg_active_sessions: 1,
      samples: 10,
      ticks: 10,
      ts: '2026-09-01T10:00:00Z',
      wait_event: 'ClientRead',
      wait_event_type: 'Client',
    },
  ],
  enabled: true,
  resolution_seconds: 30,
  statistical: false,
}

const ashTopResponse = {
  enabled: true,
  entries: [
    {
      avg_active_sessions: 2.5,
      query_text: 'SELECT * FROM users WHERE id = $1',
      queryid: '9007199254740993',
      samples: 25,
      ticks: 10,
    },
  ],
  resolution_seconds: 30,
  statistical: false,
}

function success<T>(data: T) {
  return {
    data,
    dataAge: 0,
    error: null,
    isPending: false,
    refetch: vi.fn(),
  }
}

function renderAsh(route = '/instances/instance-1/ash?range=1h') {
  mocks.ash.mockReturnValue(success(ashResponse))
  mocks.ashTop.mockReturnValue(success(ashTopResponse))
  return renderWithProviders(<AshPage instanceId="instance-1" />, { route })
}

beforeEach(() => {
  mocks.ash.mockReset()
  mocks.ashTop.mockReset()
  mocks.chart.mockReset()
})

describe('AshPage', () => {
  it('UI-ASH-020 round-trips group_by through the group URL state', () => {
    const view = renderAsh('/instances/instance-1/ash?group=wait_event&wait_event_type=Lock')

    expect(new URLSearchParams(view.router.state.location.search).get('group')).toBe('wait_event')
    expect(mocks.ash).toHaveBeenLastCalledWith(
      expect.objectContaining({ group_by: 'wait_event', instance_id: 'instance-1' }),
    )
    expect(screen.getByRole('heading', { name: 'Wait events' })).toBeInTheDocument()
  })

  it('UI-ASH-021 selecting a series drills to wait_event filtered by type', () => {
    const view = renderAsh()
    const chartProps = mocks.chart.mock.calls.at(-1)?.[0] as {
      onEvents?: Record<string, (event: unknown) => void>
    }

    act(() => chartProps.onEvents?.click?.({ seriesName: 'Lock' }))

    const params = new URLSearchParams(view.router.state.location.search)
    expect(params.get('group')).toBe('wait_event')
    expect(params.get('wait_event_type')).toBe('Lock')
    expect(mocks.ash).toHaveBeenLastCalledWith(
      expect.objectContaining({ group_by: 'wait_event', instance_id: 'instance-1' }),
    )
  })

  it('UI-ASH-022 breadcrumb navigates back out of the drill path', () => {
    const view = renderAsh(
      '/instances/instance-1/ash?group=queryid&wait_event_type=Lock&wait_event=relation',
    )

    fireEvent.click(screen.getByRole('button', { name: 'Wait events: relation' }))
    let params = new URLSearchParams(view.router.state.location.search)
    expect(params.get('group')).toBe('wait_event')
    expect(params.get('wait_event_type')).toBe('Lock')
    expect(params.get('wait_event')).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'Wait event types' }))
    params = new URLSearchParams(view.router.state.location.search)
    expect(params.get('group')).toBe('wait_event_type')
    expect(params.get('wait_event_type')).toBeNull()
  })

  it('UI-ASH-023 links top queries to the inspector with a string queryid', () => {
    renderAsh('/instances/instance-1/ash?group=queryid&wait_event=relation')

    expect(screen.getByRole('link', { name: 'Query 9007199254740993' })).toHaveAttribute(
      'href',
      '/instances/instance-1/queries/9007199254740993',
    )
  })

  it('UI-ASH-024 states the deliberate PII boundary in the query section', () => {
    renderAsh('/instances/instance-1/ash?group=queryid')

    expect(screen.getByText(/ASH never collects live query text/i)).toBeInTheDocument()
    expect(screen.getByText(/normalised text from pg_stat_statements/i)).toBeInTheDocument()
  })
})
