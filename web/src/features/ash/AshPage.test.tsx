import { act, fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { EChartsOption } from 'echarts'

import { REFRESH } from '@/api/policy'
import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'

const mocks = vi.hoisted(() => ({
  ash: vi.fn(),
  ashTop: vi.fn(),
  settings: vi.fn(),
  chart: vi.fn(),
}))

vi.mock('@/api/queries', () => ({
  useAsh: mocks.ash,
  useAshTop: mocks.ashTop,
  useInstanceSettings: mocks.settings,
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

// Top queries come from samples that carry query IDs. ashResponse has none,
// so the tests that expect the top list use these buckets instead.
const ashQueryIdResponse = {
  ...ashResponse,
  buckets: ashResponse.buckets.map((bucket) => ({ ...bucket, queryid: '9007199254740993' })),
}

function success<T>(data: T, overrides: Record<string, unknown> = {}) {
  return {
    data,
    dataAge: 0,
    dataUpdatedAt: Date.now(),
    error: null,
    isPending: false,
    refetch: vi.fn(),
    ...overrides,
  }
}

function renderAsh(
  route = '/instances/instance-1/ash?range=1h',
  response: object = ashResponse,
  settings: { name: string; value: string | null }[] = [],
  ashResult?: object,
  settingsResult?: object,
) {
  mocks.ash.mockReturnValue(ashResult ?? success(response))
  mocks.ashTop.mockReturnValue(success(ashTopResponse))
  mocks.settings.mockReturnValue(settingsResult ?? success({ instance_id: 'instance-1', settings }))
  return renderWithProviders(<AshPage instanceId="instance-1" />, { route })
}

beforeEach(() => {
  mocks.ash.mockReset()
  mocks.ashTop.mockReset()
  mocks.settings.mockReset()
  mocks.chart.mockReset()
})

describe('AshPage', () => {
  it('UI-ASH-020 round-trips group_by through the group URL state', () => {
    const view = renderAsh('/instances/instance-1/ash?group=wait_event&wait_event_type=Lock')

    expect(new URLSearchParams(view.router.state.location.search).get('group')).toBe('wait_event')
    expect(mocks.ash).toHaveBeenLastCalledWith(
      expect.objectContaining({
        group_by: 'wait_event',
        instance_id: 'instance-1',
        limit: 5000,
        wait_event_type: 'Lock',
      }),
    )
    expect(mocks.ash.mock.lastCall?.[0]).not.toHaveProperty('wait_event')
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
      expect.objectContaining({
        group_by: 'wait_event',
        instance_id: 'instance-1',
        wait_event_type: 'Lock',
      }),
    )
  })

  it('UI-ASH-025 asks the top level for every wait event type, without filters', () => {
    renderAsh('/instances/instance-1/ash?group=wait_event_type&wait_event_type=Lock')

    const params = mocks.ash.mock.lastCall?.[0] as Record<string, unknown>
    expect(params).toMatchObject({ group_by: 'wait_event_type', limit: 5000 })
    expect(params).not.toHaveProperty('wait_event_type')
    expect(params).not.toHaveProperty('wait_event')
  })

  it('UI-ASH-026 plots the rows of a drill-down, which carry only their group column', () => {
    // A grouped answer fills in only the group column (here wait_event); the
    // parent type is echoed by the server when filtered, not guaranteed.
    renderAsh('/instances/instance-1/ash?group=wait_event&wait_event_type=Lock', {
      ...ashResponse,
      buckets: [
        {
          avg_active_sessions: 0.5,
          samples: 5,
          ticks: 10,
          ts: '2026-09-01T10:00:00Z',
          wait_event: 'relation',
        },
        {
          avg_active_sessions: 0.2,
          samples: 2,
          ticks: 10,
          ts: '2026-09-01T10:00:00Z',
          wait_event: 'tuple',
        },
      ],
    })

    const option = (mocks.chart.mock.calls.at(-1)?.[0] as { option: EChartsOption }).option
    const names = (option.series as { name: string }[]).map((series) => series.name)
    expect(names).toEqual(expect.arrayContaining(['relation', 'tuple']))
    expect(
      screen.queryByRole('heading', { name: 'No wait events observed' }),
    ).not.toBeInTheDocument()
  })

  it('UI-ASH-027 marks a missing bucket as a gap at the server resolution', () => {
    renderAsh('/instances/instance-1/ash', {
      ...ashResponse,
      buckets: [
        {
          avg_active_sessions: 1,
          samples: 10,
          ticks: 10,
          ts: '2026-09-01T10:00:00Z',
          wait_event_type: 'CPU',
        },
        {
          avg_active_sessions: 1,
          samples: 10,
          ticks: 10,
          ts: '2026-09-01T10:01:30Z',
          wait_event_type: 'CPU',
        },
      ],
      resolution_seconds: 30,
    })

    const option = (mocks.chart.mock.calls.at(-1)?.[0] as { option: EChartsOption }).option
    const cpu = (option.series as { name: string; data: unknown[] }[]).find(
      (series) => series.name === 'CPU',
    )
    // 10:00:00, 10:00:30 and 10:01:00 unsampled, 10:01:30: two gaps, not
    // one 90-second step between two points.
    expect(cpu?.data).toHaveLength(4)
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

  it('UI-ASH-035 reports the summed samples of every group folded into other', () => {
    const ts = '2026-09-01T10:00:00Z'
    const groups: [string, number][] = [
      ['CPU', 20],
      ...Array.from({ length: 12 }, (_, index): [string, number] => [`W${index + 1}`, 13 - index]),
    ]
    renderAsh('/instances/instance-1/ash', {
      ...ashResponse,
      buckets: groups.map(([name, samples]) => ({
        avg_active_sessions: samples / 10,
        samples,
        ticks: 10,
        ts,
        wait_event_type: name,
      })),
    })

    const option = (mocks.chart.mock.calls.at(-1)?.[0] as { option: EChartsOption }).option
    const formatter = (option.tooltip as { formatter: (params: unknown) => string }).formatter
    // Eleven groups stay visible next to "other": W11 (3 samples) and W12
    // (2) are folded, and "other" is their sum, not the last one read.
    expect(formatter([{ seriesName: 'other (folded)', value: [ts, 0.5] }])).toContain(
      'samples: 5, ticks: 10',
    )
  })

  it('UI-ASH-023 links top queries to the inspector with a string queryid', () => {
    renderAsh('/instances/instance-1/ash?group=queryid&wait_event=relation', ashQueryIdResponse)

    expect(screen.getByRole('link', { name: 'Query 9007199254740993' })).toHaveAttribute(
      'href',
      '/instances/instance-1/queries/9007199254740993',
    )
  })

  it('UI-ASH-028 narrows the queries and the top list to the drilled wait event', () => {
    renderAsh(
      '/instances/instance-1/ash?group=queryid&wait_event_type=Lock&wait_event=relation',
      ashQueryIdResponse,
    )

    expect(mocks.ash).toHaveBeenLastCalledWith(
      expect.objectContaining({
        group_by: 'queryid',
        wait_event: 'relation',
        wait_event_type: 'Lock',
      }),
    )
    expect(mocks.ashTop).toHaveBeenLastCalledWith(
      expect.objectContaining({ wait_event: 'relation', wait_event_type: 'Lock' }),
    )
    expect(screen.getByText(/Ranked by samples in wait event Lock\/relation\./)).toBeInTheDocument()
  })

  it('UI-ASH-029 ranks the unfiltered top list across every wait event', () => {
    renderAsh('/instances/instance-1/ash?group=queryid', ashQueryIdResponse)

    const params = mocks.ashTop.mock.lastCall?.[0] as Record<string, unknown>
    expect(params).not.toHaveProperty('wait_event')
    expect(params).not.toHaveProperty('wait_event_type')
    expect(screen.getByText(/Ranked by samples across every wait event\./)).toBeInTheDocument()
  })

  it('UI-ASH-024 states the deliberate PII boundary in the query section', () => {
    renderAsh('/instances/instance-1/ash?group=queryid', ashQueryIdResponse)

    expect(screen.getByText(/ASH never collects live query text/i)).toBeInTheDocument()
    expect(screen.getByText(/normalised text from pg_stat_statements/i)).toBeInTheDocument()
  })

  it('UI-ASH-030 names checks.ash when ASH is disabled', () => {
    renderAsh('/instances/instance-1/ash', { ...ashResponse, buckets: [], enabled: false })

    expect(
      screen
        .getAllByRole('status')
        .some((element) => /ASH sampling disabled/i.test(element.textContent ?? '')),
    ).toBe(true)
    expect(
      screen
        .getAllByRole('status')
        .some((element) => /checks\.ash/i.test(element.textContent ?? '')),
    ).toBe(true)
    expect(screen.getByText(/avoids its sampling cost/i)).toBeInTheDocument()
  })

  it('UI-ASH-031 distinguishes disabled ASH from an enabled empty result', () => {
    const disabled = renderAsh('/instances/instance-1/ash', {
      ...ashResponse,
      buckets: [],
      enabled: false,
    })
    expect(
      screen.queryByRole('heading', { name: 'No wait event types observed' }),
    ).not.toBeInTheDocument()
    disabled.unmount()

    renderAsh('/instances/instance-1/ash', { ...ashResponse, buckets: [], enabled: true })
    expect(
      screen.getByRole('heading', { name: 'No wait event types observed' }),
    ).toBeInTheDocument()
    expect(screen.queryByText(/ASH sampling disabled/i)).not.toBeInTheDocument()
  })

  it('UI-ASH-040 renders an empty range as empty, not disabled', () => {
    renderAsh('/instances/instance-1/ash?range=1h', { ...ashResponse, buckets: [], enabled: true })

    expect(
      screen.getByRole('heading', { name: 'No wait event types observed' }),
    ).toBeInTheDocument()
    expect(screen.queryByText(/ASH sampling disabled/i)).not.toBeInTheDocument()
  })

  it('UI-ASH-041 marks ASH data stale after its freshness threshold', () => {
    renderAsh(
      '/instances/instance-1/ash',
      ashResponse,
      [],
      success(ashResponse, { dataUpdatedAt: Date.now() - REFRESH.ash.staleAfter }),
    )

    expect(screen.getByRole('status', { name: /Stale data:/i })).toBeInTheDocument()
  })

  it('UI-ASH-042 redirects an unauthorized ASH response to login exactly once', async () => {
    const unauthorized = success(undefined, { error: { kind: 'unauthorized' } })
    const view = renderAsh('/instances/instance-1/ash?range=1h', ashResponse, [], unauthorized)

    await act(async () => {
      await Promise.resolve()
    })

    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe(
      '?next=%2Finstances%2Finstance-1%2Fash%3Frange%3D1h',
    )

    await act(async () => {
      await Promise.resolve()
    })
    expect(view.router.state.location.pathname).toBe('/login')
  })

  it('UI-ASH-043 renders a server error with a working retry action', () => {
    const refetch = vi.fn()
    renderAsh(
      '/instances/instance-1/ash',
      ashResponse,
      [],
      success(undefined, {
        error: { kind: 'server', status: 500, error: 'internal_error', detail: 'ASH unavailable' },
        refetch,
      }),
    )

    expect(screen.getByRole('alert')).toHaveTextContent('Server returned 500')
    fireEvent.click(screen.getByRole('button', { name: 'Retry ASH' }))
    expect(refetch).toHaveBeenCalledOnce()
  })

  it('UI-ASH-044 uses the ASH refresh policy interval', () => {
    renderAsh()

    expect(REFRESH.ash.interval).toBe(30_000)
    expect(REFRESH.ash.staleAfter).toBeGreaterThanOrEqual(REFRESH.ash.interval * 3)
  })

  it('shows a loading state while ASH is pending', () => {
    renderAsh('/instances/instance-1/ash', ashResponse, [], success(undefined, { isPending: true }))

    expect(
      screen.getByRole('status', { name: 'Loading ASH and wait analysis' }),
    ).toBeInTheDocument()
  })

  it('passes the database scope to ASH and top-query requests', () => {
    renderAsh('/instances/instance-1/ash?group=queryid&db=analytics', ashQueryIdResponse)

    expect(mocks.ash).toHaveBeenLastCalledWith(expect.objectContaining({ database: 'analytics' }))
    expect(mocks.ashTop).toHaveBeenLastCalledWith(
      expect.objectContaining({ database: 'analytics' }),
    )
  })

  it('ignores clicks on the folded other series', () => {
    const view = renderAsh()
    const chartProps = mocks.chart.mock.calls.at(-1)?.[0] as {
      onEvents?: Record<string, (event: unknown) => void>
    }

    act(() => chartProps.onEvents?.click?.({ seriesName: 'other (folded)' }))

    expect(view.router.state.location.search).toBe('?range=1h')
  })

  it('drills from a wait event to query ids', () => {
    const view = renderAsh('/instances/instance-1/ash?group=wait_event&wait_event_type=Lock')
    const chartProps = mocks.chart.mock.calls.at(-1)?.[0] as {
      onEvents?: Record<string, (event: unknown) => void>
    }

    act(() => chartProps.onEvents?.click?.({ seriesName: 'relation' }))

    const params = new URLSearchParams(view.router.state.location.search)
    expect(params.get('group')).toBe('queryid')
    expect(params.get('wait_event')).toBe('relation')
  })

  it('labels an unfiltered wait-event breadcrumb as all', () => {
    renderAsh('/instances/instance-1/ash?group=wait_event')

    expect(screen.getByText('Wait events: all')).toBeInTheDocument()
  })

  it('UI-ASH-032 puts the sample warning above the chart and includes its count', () => {
    const view = renderAsh('/instances/instance-1/ash', {
      ...ashResponse,
      warning: 'The range is under-sampled.',
    })

    const warning = screen
      .getAllByRole('status')
      .find((element) => element.textContent?.includes('ASH sampling warning'))
    const chart = screen.getByTestId('mock-echarts')
    if (!warning) throw new Error('ASH sampling warning was not rendered')
    expect(warning).toHaveTextContent('25 samples')
    expect(warning.compareDocumentPosition(chart) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    view.unmount()
  })

  it('UI-ASH-033 explains missing query attribution when compute_query_id is off', () => {
    renderAsh(
      '/instances/instance-1/ash?group=queryid',
      {
        ...ashResponse,
        buckets: ashResponse.buckets.map((bucket) => ({ ...bucket, queryid: null })),
      },
      [{ name: 'compute_query_id', value: 'off' }],
    )

    expect(
      screen
        .getAllByRole('status')
        .some((element) => /compute_query_id is off/i.test(element.textContent ?? '')),
    ).toBe(true)
    expect(
      screen.getByRole('link', { name: /agent configuration in the README/i }),
    ).toHaveAttribute('href', '/README.md#agent-configuration')
    expect(screen.queryByRole('heading', { name: 'Top queries' })).not.toBeInTheDocument()
  })

  it('UI-ASH-046 explains missing query attribution when compute_query_id is left at auto', () => {
    renderAsh('/instances/instance-1/ash?group=queryid', {
      ...ashResponse,
      buckets: ashResponse.buckets.map((bucket) => ({ ...bucket, queryid: null })),
    })

    expect(
      screen
        .getAllByRole('status')
        .some((element) => /pg_stat_statements is loaded/i.test(element.textContent ?? '')),
    ).toBe(true)
    expect(screen.queryByTestId('mock-echarts')).not.toBeInTheDocument()
  })

  it('UI-ASH-047 keeps the breakdown when compute_query_id is on, even with no query IDs', () => {
    renderAsh(
      '/instances/instance-1/ash?group=queryid',
      {
        ...ashResponse,
        buckets: ashResponse.buckets.map((bucket) => ({ ...bucket, queryid: null })),
      },
      [{ name: 'compute_query_id', value: 'on' }],
    )

    expect(screen.getByTestId('mock-echarts')).toBeInTheDocument()
    expect(screen.queryByText(/pg_stat_statements is loaded/i)).not.toBeInTheDocument()
  })

  it('UI-ASH-048 does not call an empty queryid range missing query attribution', () => {
    renderAsh('/instances/instance-1/ash?group=queryid', { ...ashResponse, buckets: [] })

    expect(screen.queryByText(/pg_stat_statements is loaded/i)).not.toBeInTheDocument()
  })

  it('UI-ASH-034 always explains the sampling limits', () => {
    renderAsh()

    expect(screen.getByTestId('ash-sampling-footnote')).toHaveTextContent(
      /1 s statistical sampling/i,
    )
    expect(screen.getByTestId('ash-sampling-footnote')).toHaveTextContent(/100 wait keys/i)
  })

  it('keeps the disabled ASH state accessible', async () => {
    const view = renderAsh('/instances/instance-1/ash', {
      ...ashResponse,
      buckets: [],
      enabled: false,
    })
    expect(view.container).toBeTruthy()
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })

  it('keeps the warning ASH state accessible', async () => {
    const view = renderAsh('/instances/instance-1/ash', {
      ...ashResponse,
      warning: 'The range is under-sampled.',
    })
    expect(view.container).toBeTruthy()
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })

  it('keeps the populated ASH state accessible', async () => {
    const view = renderAsh()
    expect(view.container).toBeTruthy()
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })
})
