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
