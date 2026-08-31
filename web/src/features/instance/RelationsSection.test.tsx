import { act, screen } from '@testing-library/react'
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
      indexes: response([item({ indexrelname: 'orders_pkey', idx_scan: 3 })]),
      bloat: response([item({ bloat_bytes: 2048, method: 'estimate' })]),
    })
    await settle()

    expect(
      screen.getByText(
        'Showing 1 of 50 tables shared per instance across databases (truncated; budget 50).',
      ),
    ).toBeInTheDocument()
    expect(screen.getByText('checks.table_stats.top_n')).toBeInTheDocument()
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
})
