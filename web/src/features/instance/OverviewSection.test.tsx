import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { REFRESH } from '@/api/policy'
import { useTimeRange } from '@/hooks/useTimeRange'
import { formatBytes } from '@/lib/format'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'
import { expectNoA11yViolations } from '@/test/a11y'

import { OverviewSection } from './OverviewSection'

const chartMock = vi.hoisted(() => ({ options: [] as unknown[] }))

vi.mock('echarts-for-react/esm/core', () => ({
  default: (props: { option: unknown }) => {
    chartMock.options.push(props.option)
    return null
  },
}))

const metricsUrl = 'http://localhost/api/v1/metrics/query'
const resetSeries = {
  series: [
    { ts: '2026-01-01T00:00:00Z', value: 10 },
    { ts: '2026-01-01T00:01:00Z', value: null },
    { ts: '2026-01-01T00:02:00Z', value: 12 },
  ],
}

afterEach(() => {
  server.events.removeAllListeners()
})

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function metricsHandler(body: typeof resetSeries | { series: [] }, steps: string[] = []) {
  return http.get(metricsUrl, ({ request }) => {
    const url = new URL(request.url)
    steps.push(url.searchParams.get('step') ?? '')
    return HttpResponse.json(body)
  })
}

function RangeProbe() {
  const { setPreset } = useTimeRange()
  return <button onClick={() => setPreset('7d')}>Use seven days</button>
}

describe('OverviewSection', () => {
  beforeEach(() => {
    chartMock.options = []
  })

  it('UI-INST-024 renders a reset annotation while preserving the chart gap', async () => {
    server.use(metricsHandler(resetSeries))

    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
    })
    await settle()

    const option = chartMock.options.find((candidate) => {
      const series = (candidate as { series?: { markLine?: unknown }[] }).series
      return series?.some((item) => item.markLine !== undefined)
    }) as {
      series: {
        connectNulls: boolean
        data: [string, number | null][]
        markPoint?: unknown
        markLine?: { data: { xAxis: string }[] }
      }[]
    }
    const series = option.series.find((item) => item.markLine !== undefined)

    expect(series?.connectNulls).toBe(false)
    expect(series?.data[1]?.[1]).toBeNull()
    expect(series?.markPoint).toBeUndefined()
    expect(series?.markLine?.data).toEqual([{ xAxis: series?.data[1]?.[0] }])
  })

  it('UI-INST-025 renders every tile as not measured when its value is null', async () => {
    server.use(metricsHandler({ series: [] }))

    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
    })
    await settle()

    for (const label of [
      'Connections in use',
      'Transactions',
      'Cache hit ratio',
      'WAL generated in range',
      'Checkpoints in range',
      'Temporary bytes in range',
      'Temporary files in range',
      'Deadlocks in range',
    ]) {
      expect(
        within(screen.getByRole('article', { name: label })).getAllByLabelText('not measured'),
      ).not.toHaveLength(0)
    }
  })

  it('UI-INST-026 refetches all metric queries with a new step after a range change', async () => {
    const steps: string[] = []
    server.use(metricsHandler({ series: [] }, steps))

    renderWithProviders(
      <>
        <OverviewSection database="app" instanceId={INSTANCE_ID} />
        <RangeProbe />
      </>,
      { route: `/instances/${INSTANCE_ID}?db=app&range=15m` },
    )
    await settle()
    const initialCalls = steps.length

    fireEvent.click(screen.getByRole('button', { name: 'Use seven days' }))
    await settle()

    expect(initialCalls).toBeGreaterThan(0)
    expect(steps).toContain('15s')
    expect(steps).toContain('15m')
    expect(steps.length).toBeGreaterThan(initialCalls)
  })

  function stepsByMetric() {
    const steps = new Map<string, string>()
    server.use(
      http.get(metricsUrl, ({ request }) => {
        const url = new URL(request.url)
        steps.set(url.searchParams.get('metric') ?? '', url.searchParams.get('step') ?? '')
        return HttpResponse.json({ series: [] })
      }),
    )
    return steps
  }

  it('OV-STEP-001 queries a counter at one minute and a gauge at its sampling over 1 h', async () => {
    const steps = stepsByMetric()
    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
    })
    await settle()

    expect(steps.get('pg_xact_commit_total')).toBe('1m')
    expect(steps.get('pg_xact_rollback_total')).toBe('1m')
    expect(steps.get('pg_wal_lsn_bytes')).toBe('1m')
    // Connections are sampled by the activity check every 10 s: the 1 h base step of 15 s.
    expect(steps.get('pg_connections_used')).toBe('15s')
    expect(steps.get('pg_connections_limit')).toBe('15s')
  })

  it('OV-STEP-002 queries a counter at five minutes or more over 24 h', async () => {
    const steps = stepsByMetric()
    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=24h`,
    })
    await settle()

    expect(steps.get('pg_xact_commit_total')).toBe('5m')
    expect(steps.get('pg_connections_used')).toBe('5m')
  })

  it('OV-CARD-001 places every chart in a card that states its range and step', async () => {
    server.use(metricsHandler(resetSeries))
    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
    })
    await settle()

    for (const [title, step] of [
      ['Connections', '15s'],
      ['Transactions per second', '1m'],
      ['Deadlocks and conflicts', '1m'],
      ['Cache hit ratio', '1m'],
      ['Blocks read vs hit', '1m'],
      ['Temp files and bytes', '1m'],
      ['WAL generation', '1m'],
      ['Checkpoints', '1m'],
      ['Checkpoint write and sync time', '1m'],
    ] as const) {
      // The unit hint follows the title in the heading.
      const name = new RegExp(`^${title}( ?\\(|$)`)
      const card = screen.getByRole('region', { name })
      expect(within(card).getByRole('heading', { level: 3, name })).toBeVisible()
      expect(within(card).getByText(new RegExp(`UTC · step ${step}$`))).toBeVisible()
      expect(within(card).getByRole('img', { name: new RegExp(title) })).toBeVisible()
    }
  })

  it('DL-UI-004 keeps the tiles measured when the newest bucket is still empty', async () => {
    const values: Record<string, number> = {
      pg_connections_used: 7,
      pg_connections_limit: 100,
      pg_xact_commit_total: 4,
      pg_xact_rollback_total: 2,
      pg_blks_hit_total: 99,
      pg_blks_read_total: 1,
    }
    server.use(
      http.get(metricsUrl, ({ request }) => {
        const metric = new URL(request.url).searchParams.get('metric') ?? ''
        return HttpResponse.json({
          series: [
            { ts: '2026-08-27T00:58:00Z', value: values[metric] ?? null },
            { ts: '2026-08-27T00:59:00Z', value: values[metric] ?? null },
            { ts: '2026-08-27T01:00:00Z', value: null },
          ],
        })
      }),
    )

    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
    })
    await settle()

    const connections = screen.getByRole('article', { name: 'Connections in use' })
    expect(connections).toHaveTextContent('7')
    expect(connections).toHaveTextContent('of 100')
    expect(screen.getByRole('article', { name: 'Transactions' })).toHaveTextContent(
      /6\s*per second/,
    )
    expect(screen.getByRole('article', { name: 'Cache hit ratio' })).toHaveTextContent('99')
  })

  it('UI-INST-028 counts rolled-back transactions in the transactions tile', async () => {
    server.use(
      http.get(metricsUrl, ({ request }) => {
        const metric = new URL(request.url).searchParams.get('metric') ?? ''
        const rate =
          metric === 'pg_xact_commit_total' ? 4 : metric === 'pg_xact_rollback_total' ? 2 : null
        return HttpResponse.json({ series: [{ ts: '2026-08-27T01:00:00Z', value: rate }] })
      }),
    )

    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=24h`,
    })
    await settle()

    expect(screen.getByRole('article', { name: 'Transactions' })).toHaveTextContent(
      /6\s*per second/,
    )
  })

  it('UI-INST-027 asks for instance-wide metrics without a database and totals the stored rates', async () => {
    const databases = new Map<string, string | null>()
    server.use(
      http.get(metricsUrl, ({ request }) => {
        const url = new URL(request.url)
        const metric = url.searchParams.get('metric') ?? ''
        databases.set(metric, url.searchParams.get('database'))
        const rate =
          metric === 'pg_wal_lsn_bytes'
            ? 1024
            : metric === 'pg_deadlocks_total'
              ? 0.01
              : metric === 'pg_temp_files_total'
                ? 0.5
                : metric === 'pg_xact_commit_total'
                  ? 12.5
                  : metric === 'pg_xact_rollback_total'
                    ? 0.5
                    : null
        return HttpResponse.json({
          series: [
            { ts: '2026-08-27T01:00:00Z', value: rate },
            { ts: '2026-08-27T01:01:00Z', value: rate },
          ],
        })
      }),
    )

    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=24h`,
    })
    await settle()

    for (const metric of [
      'pg_connections_used',
      'pg_connections_limit',
      'pg_checkpoints_timed_total',
      'pg_checkpoints_requested_total',
      'pg_wal_lsn_bytes',
    ]) {
      expect(databases.get(metric)).toBeNull()
    }
    for (const metric of [
      'pg_xact_commit_total',
      'pg_xact_rollback_total',
      'pg_blks_hit_total',
      'pg_blks_read_total',
      'pg_temp_bytes_total',
      'pg_temp_files_total',
      'pg_deadlocks_total',
    ]) {
      expect(databases.get(metric)).toBe('app')
    }
    // The 24h range uses 5-minute steps: the first bucket counts one step,
    // the second the minute since the first.
    expect(screen.getByRole('article', { name: 'WAL generated in range' })).toHaveTextContent(
      formatBytes(1024 * 360),
    )
    expect(screen.getByRole('article', { name: 'Deadlocks in range' })).toHaveTextContent('4')
    // Half a file a second over the same 360 seconds as the deadlocks tile.
    expect(screen.getByRole('article', { name: 'Temporary files in range' })).toHaveTextContent(
      '180',
    )
    expect(screen.getByRole('article', { name: 'Transactions' })).toHaveTextContent(
      /13\s*per second/,
    )
  })

  // A relative window read from the render clock gave every poll a second request
  // for each metric tile.
  it('UI-INST-029 sends one request per poll for each relative metric window', async () => {
    const checkpointRequests: string[] = []
    server.events.on('request:start', ({ request }) => {
      const url = new URL(request.url)
      if (
        url.pathname.endsWith('/api/v1/metrics/query') &&
        url.searchParams.get('metric') === 'pg_checkpoints_timed_total'
      ) {
        checkpointRequests.push(request.url)
      }
    })
    server.use(metricsHandler(resetSeries))
    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
    })
    await settle()

    const polls = 3
    for (let poll = 0; poll < polls; poll += 1) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(REFRESH.activity.interval)
      })
      await settle()
    }

    // The mount fetch, then one per poll.
    expect(checkpointRequests).toHaveLength(polls + 1)
  })

  it('OVC-006 separates the tiles from the first chart group with a rule and spacing', async () => {
    server.use(metricsHandler(resetSeries))
    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
    })
    await settle()

    const charts = screen.getByTestId('overview-charts')
    const tile = screen.getByRole('article', { name: 'Connections in use' })
    expect(charts).toHaveClass('mt-12')
    expect(within(charts).getByRole('separator')).toHaveClass('mb-6')
    expect(charts).not.toContainElement(tile)
    expect(within(charts).getByRole('region', { name: 'Workload' })).toHaveClass(
      'mt-6',
      'scroll-mt-32',
    )
    expect(within(charts).getByRole('region', { name: 'Memory & I/O' })).toHaveClass(
      'mt-10',
      'scroll-mt-32',
    )
    expect(within(charts).getByTestId('workload-grid')).toHaveClass('gap-6')
    expect(within(charts).getByRole('navigation', { name: 'Chart groups' })).toBeVisible()
  })

  it('OVC-009 the whole Overview has no axe violations', async () => {
    server.use(metricsHandler(resetSeries))
    const { container } = renderWithProviders(
      <OverviewSection database="app" instanceId={INSTANCE_ID} />,
      { route: `/instances/${INSTANCE_ID}?db=app&range=1h` },
    )
    await settle()

    expect(screen.getAllByRole('region').length).toBeGreaterThan(20)
    vi.useRealTimers()
    await expectNoA11yViolations(container)
  })
})
