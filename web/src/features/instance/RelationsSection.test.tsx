import { act, fireEvent, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { RelationsSection } from './RelationsSection'

type RelationResponse = Schemas['RelationResponse']
type RelationItem = RelationResponse['items'][number]

const OBSERVED_AT = '2026-08-28T12:00:00Z'

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
}: {
  tables?: RelationResponse
  indexes?: RelationResponse
  bloat?: RelationResponse
} = {}) {
  server.use(
    ok('getInstanceTables', tables),
    ok('getInstanceIndexes', indexes),
    ok('getInstanceBloat', bloat),
  )
  return renderWithProviders(<RelationsSection instanceId={INSTANCE_ID} />, {
    route: `/instances/${INSTANCE_ID}`,
  })
}

describe('RelationsSection', () => {
  it('UI-INST-050 renders the shared truncation budget and configuration key', async () => {
    renderRelations({
      tables: response([item({ n_live_tup: 100, n_dead_tup: 10 })], { truncated: true }),
      indexes: response([item({ indexrelname: 'orders_pkey', idx_scan: 3 })], { truncated: true }),
      bloat: response([item({ bloat_bytes: 2048, method: 'estimate' })], { truncated: true }),
    })
    await settle()

    expect(
      screen.getByText(
        'Showing 1 of 50 tables shared per instance across databases (truncated; budget 50).',
      ),
    ).toBeInTheDocument()
    expect(screen.getAllByText('checks.table_stats.top_n')).toHaveLength(3)
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

  it('UI-INST-054 disables exact bloat guidance when pgstattuple is absent', async () => {
    const { container } = renderRelations({
      tables: response([item({ n_live_tup: 100 })]),
      indexes: response([item({ indexrelname: 'orders_pkey' })]),
      bloat: response([item({ bloat_bytes: 2048, method: 'estimate' })]),
    })
    await settle()

    expect(screen.getByRole('button', { name: /Exact bloat unavailable/ })).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent('pgstattuple')
    expect(screen.getByRole('status')).toHaveTextContent('pglens never installs extensions')

    vi.useRealTimers()
    await expectNoA11yViolations(container)
  })

  it('UI-INST-055 shows loading states and links exact bloat to Query Inspector', async () => {
    renderRelations({
      tables: response([item({ n_live_tup: 100 })]),
      indexes: response([item({ indexrelname: 'orders_pkey' })]),
      bloat: response([item({ bloat_bytes: 2048, method: 'pgstattuple' })]),
    })

    expect(screen.getByRole('status', { name: /Loading Tables/i })).toBeInTheDocument()
    expect(screen.getByText(/Checking whether pgstattuple/i)).toBeInTheDocument()

    await settle()

    expect(screen.getByRole('link', { name: /Run exact bloat with pgstattuple/i })).toHaveAttribute(
      'href',
      `/instances/${INSTANCE_ID}/queries?command=pgstattuple`,
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
          size_bytes: 1024,
          seq_scan: null,
          ts: null,
        }),
        item({
          schemaname: 'public',
          relname: '',
          n_live_tup: 7,
          n_dead_tup: 1,
          dead_ratio: 0.1,
          total_bytes: 2048,
          seq_scan: 3,
          ts: OBSERVED_AT,
        }),
      ]),
      indexes: response([
        item({
          schemaname: null,
          relname: 'orders',
          indexrelname: 'orders_pkey',
          idx_scan: 3,
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
          idx_scan: 'unknown',
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
    renderWithProviders(<RelationsSection instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}`,
    })
    await settle()

    expect(screen.getAllByRole('alert')).toHaveLength(1)
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load table relations.')
    fireEvent.click(screen.getByRole('button', { name: 'Retry table relations' }))
    await settle()
    expect(screen.getAllByRole('alert')).toHaveLength(1)
  })
})
