import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import IndexAnalysisPage from './IndexAnalysisPage'

import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as clusters } from '@/test/fixtures/getClusters'
import { expectClearCard } from '@/test/clearCard'
import { never, ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

vi.mock('echarts-for-react/esm/core', () => ({ default: () => null }))

function index(name: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    indexrelname: name,
    index_bytes: 8192,
    idx_scan_rate: 0.5,
    is_primary: false,
    is_unique: false,
    is_valid: true,
    last_scan_status: 'scanned',
    last_scan_at: '2026-08-27T01:00:00Z',
    definition: `CREATE INDEX ${name} ON public.orders USING btree (a, b)`,
    ts: '2026-08-27T02:00:00Z',
    ...extra,
  }
}

function finding(id: string, rule: string, extra: Record<string, unknown> = {}) {
  return {
    finding_id: id,
    rule_id: rule,
    severity: 'info',
    state: 'open',
    scope: 'relation',
    datname: 'app',
    object_name: `public.${id}`,
    title: rule,
    ...extra,
  }
}

/** The one primary figure of a tile (D31). */
const FIGURE = { selector: '.text-2xl' }

const REDUNDANT = Array.from({ length: 9 }, (_, n) => finding(`r${n}`, 'index.redundant_prefix'))

const ITEMS = [
  index('orders_pkey', { is_primary: true, is_unique: true }),
  index('orders_bad_idx', { is_valid: false }),
  index('orders_lower_idx', {
    definition: 'CREATE INDEX orders_lower_idx ON public.orders USING btree (lower(name))',
  }),
]

function render(findings: unknown[], items: unknown[] = ITEMS) {
  server.use(
    ok('getClusters', clusters),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items,
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getFindings', findings),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route: '/indexes' })
}

async function openSummary() {
  await flushNetwork(30)
  fireEvent.mouseDown(screen.getByRole('tab', { name: 'Summary' }))
}

describe('Index analysis summary cards (D31)', () => {
  it('CARD-INDEX-001 counts exact duplicates and redundant prefixes on separate cards', async () => {
    render([
      ...REDUNDANT,
      finding('fk1', 'index.fk_unindexed', {
        evidence: { table: 'public.orders', constraint: 'fk', columns: 'a', referenced: 'x' },
      }),
    ])
    await openSummary()

    const duplicates = screen.getByRole('article', { name: 'Exact duplicate indexes' })
    expect(within(duplicates).getByText('0', FIGURE)).toBeInTheDocument()
    expect(duplicates).toHaveTextContent('none found')
    expect(duplicates).toHaveTextContent('groups of indexes with identical definitions')
    expect(duplicates).not.toHaveTextContent('redundant')

    const redundant = screen.getByRole('article', { name: 'Redundant prefix indexes' })
    expect(within(redundant).getByText('9', FIGURE)).toBeInTheDocument()
    expect(redundant).toHaveTextContent('indexes whose columns lead a longer index')
    expect(screen.queryByRole('article', { name: 'Duplicate groups' })).not.toBeInTheDocument()

    // The second number of a tile never stands alone: it is in its own card.
    expect(within(duplicates).queryByText(/\b9\b/)).not.toBeInTheDocument()
    expect(within(redundant).queryByText(/duplicate/i)).not.toBeInTheDocument()
  })

  it('CARD-INDEX-002 every summary tile has one primary figure and names every other number', async () => {
    render([
      ...REDUNDANT,
      finding('d1', 'index.duplicate', { severity: 'warning' }),
      finding('d2', 'index.duplicate', { severity: 'warning' }),
      finding('fk1', 'index.fk_unindexed', {
        evidence: { table: 'public.orders', constraint: 'fk', columns: 'a', referenced: 'x' },
      }),
    ])
    await openSummary()

    const tiles = screen.getAllByRole('article')
    expect(tiles.map((tile) => tile.getAttribute('aria-label'))).toEqual([
      'Indexes',
      'Total size',
      'Need attention',
      'Never scanned',
      'Primary key indexes',
      'Unique indexes',
      'Partial indexes',
      'Expression indexes',
      'Composite indexes',
      'Invalid indexes',
      'Exact duplicate indexes',
      'Redundant prefix indexes',
      'Foreign keys without an index',
    ])
    for (const tile of tiles) expectClearCard(tile)

    const invalid = screen.getByRole('article', { name: 'Invalid indexes' })
    expect(invalid).toHaveTextContent('Rebuild or drop')
    const duplicates = screen.getByRole('article', { name: 'Exact duplicate indexes' })
    expect(within(duplicates).getByText('2', FIGURE)).toBeInTheDocument()
    expect(duplicates).toHaveTextContent('Review before dropping')
  })

  it('CARD-INDEX-003 shows an unknown figure while the advisor findings load', async () => {
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceIndexes', {
        instance_id: INSTANCE_ID,
        items: ITEMS,
        truncated: false,
        relations_not_reported: null,
      }),
      never('getFindings'),
    )
    renderWithProviders(<IndexAnalysisPage />, { route: '/indexes?view=summary' })
    await flushNetwork(30)
    const gaps = screen.getByRole('article', { name: 'Foreign keys without an index' })
    expect(within(gaps).getByLabelText('not measured')).toBeInTheDocument()
    expect(gaps).toHaveTextContent('the advisor findings are loading')
    expectClearCard(gaps)
  })
})
