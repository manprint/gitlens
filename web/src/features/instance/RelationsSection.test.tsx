import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import type { PermTier, Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { RelationsSection } from './RelationsSection'

type RelationResponse = Schemas['RelationResponse']
type RelationItem = RelationResponse['items'][number]

const OBSERVED_AT = '2026-08-28T12:00:00Z'
const COMMAND_ID = '22222222-2222-4222-8222-222222222222'

function item(overrides: Record<string, unknown> = {}): RelationItem {
  return {
    schemaname: 'public',
    relname: 'orders',
    ts: OBSERVED_AT,
    ...overrides,
  }
}

function response(
  items: RelationItem[] = [],
  overrides: Partial<RelationResponse> = {},
): RelationResponse {
  return {
    instance_id: INSTANCE_ID,
    items,
    truncated: false,
    relations_not_reported: null,
    ...overrides,
  }
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function renderRelations({
  tables = response(),
  indexes = response(),
  bloat = response(),
  currentTier = 'T1',
}: {
  tables?: RelationResponse
  indexes?: RelationResponse
  bloat?: RelationResponse
  currentTier?: PermTier
} = {}) {
  server.use(
    ok('getInstanceTables', tables),
    ok('getInstanceIndexes', indexes),
    ok('getInstanceBloat', bloat),
  )
  return renderWithProviders(
    <RelationsSection instanceId={INSTANCE_ID} currentTier={currentTier} />,
    { route: `/instances/${INSTANCE_ID}` },
  )
}

describe('RelationsSection', () => {
  it('UI-INST-050 renders the shared truncation budget and configuration key', async () => {
    renderRelations({
      tables: response([item({ n_live_tup: 100, n_dead_tup: 10 })], { truncated: true }),
      indexes: response([item({ indexrelname: 'orders_pkey', idx_scan_rate: 3 })], {
        truncated: true,
      }),
      bloat: response([item({ bloat_bytes: 2048, method: 'estimate' })], { truncated: true }),
    })
    await settle()

    expect(
      screen.getByText(
        'Showing 1 of 50 tables shared per instance across databases (truncated; budget 50).',
      ),
    ).toBeInTheDocument()
    expect(screen.getByText('checks.table_stats.top_n')).toBeInTheDocument()
    expect(screen.getByText('checks.index_stats.top_n')).toBeInTheDocument()
    expect(screen.getByText('checks.bloat_estimate.top_n')).toBeInTheDocument()
    expect(
      screen.getByText(
        'Showing 1 of 50 indexes shared per instance across databases (truncated; budget 50).',
      ),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        'Showing 1 of 50 bloat estimates shared per instance across databases (truncated; budget 50).',
      ),
    ).toBeInTheDocument()
  })

  it('UI-INST-051 does not show a truncation notice for complete responses', async () => {
    renderRelations({
      tables: response([item({ n_live_tup: 100 })]),
      indexes: response([item({ indexrelname: 'orders_pkey' })]),
      bloat: response([item({ bloat_bytes: 2048, method: 'estimate' })]),
    })
    await settle()

    expect(screen.queryByText(/truncated; budget/)).not.toBeInTheDocument()
  })

  it('UI-INST-052 states the bloat estimate caveat once', async () => {
    renderRelations({
      bloat: response([item({ bloat_bytes: 2048, method: 'estimate' })]),
    })
    await settle()

    expect(screen.getAllByText(/statistical estimate, not a measurement/)).toHaveLength(1)
  })

  it('UI-INST-053 omits never-analysed relations instead of showing zero bloat', async () => {
    renderRelations({
      tables: response([item({ n_live_tup: 100 })]),
      bloat: response([]),
    })
    await settle()

    expect(screen.getByRole('heading', { name: 'No bloat estimates' })).toBeInTheDocument()
    expect(screen.queryByText('never_analysed')).not.toBeInTheDocument()
    expect(screen.queryByText('0 B')).not.toBeInTheDocument()
  })

  it('UI-INST-054 gates the exact measurement on T1 and states the extension requirement', async () => {
    const { container } = renderRelations({
      tables: response([item({ n_live_tup: 100 })]),
      indexes: response([item({ indexrelname: 'orders_pkey' })]),
      bloat: response([item({ bloat_bytes: 2048, method: 'estimate', object_kind: 'table' })]),
      currentTier: 'T0',
    })
    await settle()

    expect(
      screen.getByRole('button', { name: 'Measure public.orders exactly with pgstattuple' }),
    ).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent(
      'Requires T1 permission; current tier is T0.',
    )
    expect(screen.getByText(/pglens never installs extensions/)).toBeInTheDocument()

    vi.useRealTimers()
    await expectNoA11yViolations(container)
  })

  it('UI-INST-055 runs pgstattuple on the table through the agent and refreshes the bloat list', async () => {
    const bodies: unknown[] = []
    let bloatRequests = 0
    server.use(
      ok('getInstanceTables', response([item({ n_live_tup: 100 })])),
      ok('getInstanceIndexes', response([item({ indexrelname: 'orders_pkey' })])),
      http.get('*/api/v1/instances/:id/bloat', () => {
        bloatRequests += 1
        return HttpResponse.json(
          response([
            item({ datname: 'app', bloat_bytes: 2048, method: 'estimate', object_kind: 'table' }),
            item({
              datname: 'app',
              relname: 'orders_archive',
              indexrelname: 'orders_archive_pkey',
              object_kind: 'index',
              method: 'estimate',
            }),
          ]),
        )
      }),
      http.post('*/api/v1/instances/:id/commands', async ({ request }) => {
        bodies.push(await request.json())
        return HttpResponse.json({ command_id: COMMAND_ID }, { status: 202 })
      }),
      http.get('*/api/v1/commands/:id', () =>
        HttpResponse.json({ command_id: COMMAND_ID, state: 'done', result: { table_len: 8192 } }),
      ),
    )
    renderWithProviders(<RelationsSection instanceId={INSTANCE_ID} currentTier="T1" />, {
      route: `/instances/${INSTANCE_ID}`,
    })

    expect(screen.getByRole('status', { name: /Loading Tables/i })).toBeInTheDocument()
    await settle()
    vi.useRealTimers()
    const measure = screen.getByRole('button', {
      name: 'Measure public.orders exactly with pgstattuple',
    })
    expect(
      screen.queryByRole('button', { name: /public\.orders_archive exactly/ }),
    ).not.toBeInTheDocument()
    const before = bloatRequests
    fireEvent.click(measure)

    await waitFor(() =>
      expect(screen.getByText('Command succeeded and its result is ready.')).toBeInTheDocument(),
    )
    expect(bodies).toEqual([
      { kind: 'pgstattuple', args: { schema: 'public', relation: 'orders', datname: 'app' } },
    ])
    await waitFor(() => expect(bloatRequests).toBeGreaterThan(before))
  })

  it('UI-INST-059 surfaces the agent rejection when pgstattuple is missing', async () => {
    renderRelations({
      bloat: response([item({ bloat_bytes: 2048, method: 'estimate', object_kind: 'table' })]),
    })
    server.use(
      http.post('*/api/v1/instances/:id/commands', () =>
        HttpResponse.json({ command_id: COMMAND_ID }, { status: 202 }),
      ),
      http.get('*/api/v1/commands/:id', () =>
        HttpResponse.json({
          command_id: COMMAND_ID,
          state: 'failed',
          error:
            'pgstattuple extension is not installed in database "app"; pglens does not install it',
        }),
      ),
    )
    await settle()
    vi.useRealTimers()
    fireEvent.click(
      screen.getByRole('button', { name: 'Measure public.orders exactly with pgstattuple' }),
    )

    await waitFor(() =>
      expect(
        screen.getByText(
          'Command failed: pgstattuple extension is not installed in database "app"; pglens does not install it',
        ),
      ).toBeInTheDocument(),
    )
  })

  it('UI-INST-056 renders unknown relation values and exercises table sorting', async () => {
    renderRelations({
      tables: response([
        item({
          schemaname: null,
          relname: 'orders',
          n_live_tup: null,
          n_dead_tup: 2,
          dead_ratio: null,
          total_bytes: null,
          seq_scan_rate: null,
          ts: null,
        }),
        item({
          schemaname: 'public',
          relname: '',
          n_live_tup: 7,
          n_dead_tup: 1,
          dead_ratio: 0.1,
          total_bytes: 2048,
          seq_scan_rate: 3,
          ts: OBSERVED_AT,
        }),
      ]),
      indexes: response([
        item({
          schemaname: null,
          relname: 'orders',
          indexrelname: 'orders_pkey',
          idx_scan_rate: 3,
          index_bytes: null,
          is_unique: true,
          is_primary: false,
          is_valid: true,
          ts: null,
        }),
        item({
          schemaname: 'public',
          relname: '',
          indexrelname: '',
          idx_scan_rate: 'unknown',
          index_bytes: 2048,
          is_unique: null,
          is_primary: null,
          is_valid: null,
          ts: '',
        }),
      ]),
      bloat: response([
        item({
          schemaname: null,
          relname: 'orders',
          indexrelname: null,
          real_bytes: null,
          expected_bytes: null,
          bloat_bytes: null,
          bloat_ratio: null,
          method: 'estimate',
          ts: null,
        }),
      ]),
    })
    await settle()

    expect(screen.getAllByText('orders').length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('not measured').length).toBeGreaterThan(0)
    expect(screen.getAllByText('yes').length).toBeGreaterThan(0)
    expect(screen.getByText('no')).toBeInTheDocument()

    const liveTuples = screen.getByRole('button', { name: 'Sort Tables by Live tuples' })
    fireEvent.click(liveTuples)
    fireEvent.click(liveTuples)
    fireEvent.click(screen.getByRole('button', { name: 'Sort Tables by Relation' }))
  })

  it('UI-INST-057 renders a retryable error when table relations fail', async () => {
    renderRelations()
    server.use(
      http.get('/api/v1/instances/:id/tables', () =>
        HttpResponse.json({ message: 'table relations unavailable' }, { status: 500 }),
      ),
    )
    renderWithProviders(<RelationsSection instanceId={INSTANCE_ID} currentTier="T1" />, {
      route: `/instances/${INSTANCE_ID}`,
    })
    await settle()

    expect(screen.getAllByRole('alert')).toHaveLength(1)
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load table relations.')
    fireEvent.click(screen.getByRole('button', { name: 'Retry table relations' }))
    await settle()
    expect(screen.getAllByRole('alert')).toHaveLength(1)
  })

  it('UI-INST-058 shows scan rates, the database and the newest maintenance of each table', async () => {
    renderRelations({
      tables: response([
        item({
          datname: 'app',
          n_live_tup: 100,
          seq_scan_rate: 0.02,
          idx_scan_rate: 4.5,
          last_vacuum_at: '2026-08-27T01:00:00Z',
          last_autovacuum_at: '2026-08-27T01:40:00Z',
          last_analyze_at: '2026-08-27T01:50:00Z',
          last_autoanalyze_at: '2026-08-27T01:41:00Z',
        }),
        item({ datname: 'reporting', relname: 'events', n_live_tup: 5 }),
      ]),
      indexes: response([
        item({ datname: 'app', indexrelname: 'orders_pkey', idx_scan_rate: 12.5 }),
      ]),
    })
    await settle()

    const tables = screen.getByRole('table', { name: 'Tables' })
    const orders = within(tables).getByRole('row', { name: /public\.orders/ })
    expect(orders).toHaveTextContent('app')
    expect(orders).toHaveTextContent('0.02/s')
    expect(orders).toHaveTextContent('4.5/s')
    expect(orders).toHaveTextContent('2026-08-27 01:40:00 UTC (auto)')
    expect(orders).toHaveTextContent('2026-08-27 01:50:00 UTC (manual)')
    const events = within(tables).getByRole('row', { name: /public\.events/ })
    expect(events).toHaveTextContent('reporting')
    expect(within(events).getAllByTitle(/since the statistics were reset/)).toHaveLength(2)

    const indexes = screen.getByRole('table', { name: 'Indexes' })
    expect(within(indexes).getByRole('row', { name: /orders_pkey/ })).toHaveTextContent('13/s')

    fireEvent.click(screen.getByRole('button', { name: 'Sort Tables by Last vacuum' }))
    fireEvent.click(screen.getByRole('button', { name: 'Sort Tables by Last analyze' }))
    expect(within(tables).getAllByRole('row')).toHaveLength(3)
  })
})
