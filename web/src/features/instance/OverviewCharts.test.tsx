import { act, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { INSTANCE_ID } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'

import { OverviewCharts } from './OverviewCharts'
import { OVERVIEW_GROUPS } from './overviewCharts'

const chartMock = vi.hoisted(() => ({ options: [] as unknown[] }))

vi.mock('echarts-for-react/esm/core', () => ({
  default: (props: { option: unknown }) => {
    chartMock.options.push(props.option)
    return null
  },
}))

const metricsUrl = '*/api/v1/metrics/query'
const workloadUrl = '*/api/v1/instances/:id/workload/series'
const TS = ['2026-08-27T01:00:00Z', '2026-08-27T01:05:00Z']

interface ChartOption {
  legend: { data: string[] }
  yAxis: unknown
  series: { name: string; data: [string, number | null][]; yAxisIndex?: number }[]
}

afterEach(() => {
  server.events.removeAllListeners()
})

beforeEach(() => {
  chartMock.options = []
})

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

const LADDER: Record<string, number> = { s: 1, m: 60, h: 3_600, d: 86_400 }

function stepSeconds(step: string | null): number {
  const match = /^(\d+)([smhd])$/.exec(step ?? '')
  return match ? Number.parseInt(match[1] ?? '', 10) * (LADDER[match[2] ?? 's'] ?? 1) : Number.NaN
}

interface Recorded {
  metrics: URLSearchParams[]
  workload: URLSearchParams[]
}

/** Every metric and workload series answers `value(name)` at two buckets. */
function serve(
  value: (name: string) => number | null,
  { workloadStatus = 200, workloadStep }: { workloadStatus?: number; workloadStep?: number } = {},
): Recorded {
  const recorded: Recorded = { metrics: [], workload: [] }
  server.use(
    http.get(metricsUrl, ({ request }) => {
      const params = new URL(request.url).searchParams
      recorded.metrics.push(params)
      const metric = params.get('metric') ?? ''
      return HttpResponse.json({ series: TS.map((ts) => ({ ts, value: value(metric) })) })
    }),
    http.get(workloadUrl, ({ request }) => {
      const params = new URL(request.url).searchParams
      recorded.workload.push(params)
      if (workloadStatus !== 200) {
        return HttpResponse.json(
          { error: 'internal_error', detail: 'database unavailable' },
          { status: workloadStatus },
        )
      }
      const names = (params.get('series') ?? '').split(',')
      return HttpResponse.json({
        step_seconds: workloadStep ?? stepSeconds(params.get('step')),
        source: params.get('source'),
        series: names.map((name) => ({
          name,
          unit: 'x',
          kind: 'rate',
          points: TS.map((ts) => ({ ts, value: value(name) })),
        })),
      })
    }),
  )
  return recorded
}

function renderCharts(range = '1h') {
  return renderWithProviders(<OverviewCharts database="app" instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}?db=app&range=${range}`,
  })
}

function optionWithLegend(...names: string[]): ChartOption | undefined {
  return (chartMock.options as ChartOption[])
    .filter((option) => names.every((name) => option.legend.data.includes(name)))
    .at(-1)
}

function card(title: string) {
  return screen.getByRole('region', { name: new RegExp(`^${title}( ?\\(|$)`) })
}

describe('OverviewCharts', () => {
  it('OVC-003 renders every group with its heading, description and cards', async () => {
    serve(() => 1)
    renderCharts()
    await settle()

    for (const group of OVERVIEW_GROUPS) {
      const region = screen.getByRole('region', { name: group.title })
      expect(region).toHaveAttribute('id', group.id)
      expect(within(region).getByRole('heading', { level: 2, name: group.title })).toBeVisible()
      expect(within(region).getByText(group.description)).toBeVisible()
      const titles = within(region)
        .getAllByRole('heading', { level: 3 })
        .map((heading) => heading.firstChild?.textContent)
      expect(titles).toEqual(group.cards.map((item) => item.title))
    }
    expect(screen.getAllByRole('img').length).toBe(
      OVERVIEW_GROUPS.reduce((sum, group) => sum + group.cards.length, 0),
    )
  })

  it('OVC-004 shows "No data in this range" on a card whose queries return no value', async () => {
    serve(() => null)
    renderCharts()
    await settle()

    const cards = OVERVIEW_GROUPS.flatMap((group) => group.cards)
    expect(screen.getAllByText('No data in this range')).toHaveLength(cards.length)
    expect(within(card('Calls per second')).getByText('No data in this range')).toBeVisible()
    expect(within(card('Connections')).getByText('No data in this range')).toBeVisible()
  })

  it('OVC-004 a metric missing on an older PostgreSQL empties only its own card', async () => {
    serve((name) => (name === 'pg_conflicts_total' || name === 'pg_deadlocks_total' ? null : 1))
    renderCharts()
    await settle()

    expect(screen.getAllByText('No data in this range')).toHaveLength(1)
    expect(within(card('Deadlocks and conflicts')).getByText('No data in this range')).toBeVisible()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('OVC-007 asks statements for source=statements&series=calls at 60 s or more', async () => {
    const recorded = serve(() => 1)
    renderCharts()
    await settle()

    const calls = recorded.workload.find((params) => params.get('series') === 'calls')
    expect(calls?.get('source')).toBe('statements')
    expect(stepSeconds(calls?.get('step') ?? null)).toBeGreaterThanOrEqual(60)
    expect(calls?.has('database')).toBe(false)
    for (const params of recorded.workload.filter((item) => item.get('source') === 'statements')) {
      expect(stepSeconds(params.get('step'))).toBeGreaterThanOrEqual(60)
    }
    expect(
      recorded.workload
        .filter((item) => item.get('source') === 'statements')
        .map((item) => item.get('series')),
    ).toEqual(expect.arrayContaining(['calls', 'exec_time', 'rows', 'blks_read', 'temp_blks']))
  })

  it('OVC-008 asks tables and indexes at 300 s or more, over 15 minutes too', async () => {
    const recorded = serve(() => 1)
    renderCharts('15m')
    await settle()

    const slow = recorded.workload.filter((params) => params.get('source') !== 'statements')
    expect(new Set(slow.map((params) => params.get('series')))).toEqual(
      new Set([
        'seq_scans,idx_scans',
        'tup_ins,tup_upd,tup_hot_upd,tup_del',
        'dead_tuples',
        'idx_scans',
        'index_bytes',
      ]),
    )
    for (const params of slow) {
      expect(stepSeconds(params.get('step'))).toBeGreaterThanOrEqual(300)
    }
  })

  it('OVC-003 asks per-database metrics with the database and instance metrics without it', async () => {
    const recorded = serve(() => 1)
    renderCharts()
    await settle()

    const database = new Map(
      recorded.metrics.map((params) => [params.get('metric'), params.get('database')]),
    )
    expect(database.get('pg_xact_commit_total')).toBe('app')
    expect(database.get('pg_conflicts_total')).toBe('app')
    expect(database.get('pg_temp_bytes_total')).toBe('app')
    expect(database.get('pg_connections_used')).toBeNull()
    expect(database.get('pg_wal_wal_bytes')).toBeNull()
    expect(database.get('pg_checkpoint_sync_time_ms_total')).toBeNull()
    // Two cards read the block counters: React Query asks once per metric.
    expect(
      recorded.metrics.filter((params) => params.get('metric') === 'pg_blks_hit_total'),
    ).toHaveLength(1)
  })

  it('OVC-003 derives the hit ratio, scales checkpoints per hour and splits units on two axes', async () => {
    serve((name) =>
      name === 'pg_blks_hit_total'
        ? 3
        : name === 'pg_blks_read_total'
          ? 1
          : name === 'pg_checkpoints_timed_total'
            ? 0.001
            : 2,
    )
    renderCharts()
    await settle()

    expect(optionWithLegend('Cache hit ratio')?.series[0]?.data[0]?.[1]).toBe(0.75)
    expect(optionWithLegend('Timed', 'Requested')?.series[0]?.data[0]?.[1]).toBeCloseTo(3.6)
    const temp = optionWithLegend('Temporary files', 'Temporary bytes')
    expect(Array.isArray(temp?.yAxis) && temp.yAxis).toHaveLength(2)
    expect(temp?.series.map((line) => line.yAxisIndex)).toEqual([undefined, 1])
    const wal = optionWithLegend('WAL bytes', 'WAL records', 'Full-page images')
    expect(wal?.series.map((line) => line.yAxisIndex)).toEqual([undefined, 1, 1])
    expect(optionWithLegend('Commits', 'Rollbacks')?.series).toHaveLength(2)
  })

  it('OVC-007 labels workload lines by card and states the step the server used', async () => {
    serve(() => 4, { workloadStep: 600 })
    renderCharts()
    await settle()

    expect(optionWithLegend('Inserted', 'Updated', 'HOT updated', 'Deleted')?.series).toHaveLength(
      4,
    )
    expect(optionWithLegend('Calls per second')?.series[0]?.data[0]?.[1]).toBe(4)
    expect(within(card('Calls per second')).getByText(/UTC · step 10m$/)).toBeVisible()
  })

  it('OVC-004 shows the error state with a retry on a failed workload card only', async () => {
    serve(() => 1, { workloadStatus: 500 })
    renderCharts()
    await settle()

    expect(within(card('Calls per second')).getByRole('button', { name: /retry/i })).toBeVisible()
    expect(within(card('Index size')).getByRole('button', { name: /retry/i })).toBeVisible()
    expect(within(card('Connections')).queryByRole('button', { name: /retry/i })).toBeNull()
    expect(within(card('Connections')).getByRole('img')).toBeVisible()
  })
})
