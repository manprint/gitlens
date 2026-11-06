import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import IndexAnalysisPage from './IndexAnalysisPage'

import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { custom as advisorWindows } from '@/test/fixtures/getAdvisorWindows'
import { base as clusters } from '@/test/fixtures/getClusters'
import { CAPTIONS } from '@/lib/captions'
import { formatRate } from '@/lib/format'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, filterTable, searchTable, seq, sortTable } from '@/test/table'

function index(name: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    indexrelname: name,
    index_bytes: 8192,
    idx_scan_rate: 1,
    is_primary: false,
    is_unique: false,
    is_valid: true,
    last_scan_status: 'scanned',
    last_scan_at: '2026-08-27T01:00:00Z',
    definition: `CREATE INDEX ${name} ON public.orders USING btree (customer_id)`,
    ts: '2026-08-27T02:00:00Z',
    ...extra,
  }
}

const ITEMS = [
  index('orders_pkey', {
    is_primary: true,
    is_unique: true,
    last_scan_status: 'never_since_reset',
    definition: 'CREATE UNIQUE INDEX orders_pkey ON public.orders USING btree (id)',
  }),
  index('orders_bad_idx', { is_valid: false }),
  index('orders_a_idx', { idx_scan_rate: 5 }),
  index('orders_dup_idx', { idx_scan_rate: 0 }),
  index('orders_old_idx', { last_scan_status: 'never_since_reset', last_scan_at: null }),
  index('orders_raw_idx', { last_scan_status: 'never_since_reset', last_scan_at: null }),
  index('orders_busy_idx'),
  index('other_bad_idx', { relname: 'other', is_valid: false, definition: null }),
]

function finding(id: string, rule: string, object: string, extra: Record<string, unknown> = {}) {
  return {
    finding_id: id,
    rule_id: rule,
    severity: 'warning',
    state: 'open',
    scope: 'relation',
    datname: 'app',
    object_name: object,
    title: rule,
    ...extra,
  }
}

const FINDINGS = [
  finding('c1', 'index.duplicate', 'public.orders_a_idx', {
    evidence: { indexes: ['public.orders_a_idx', 'public.orders_dup_idx'] },
  }),
  finding('c2', 'index.unused', 'public.orders_old_idx'),
]

const TABLES = [
  {
    datname: 'app',
    schemaname: 'public',
    relname: 'orders',
    n_tup_ins_rate: 2,
    n_tup_upd_rate: 5,
    n_tup_hot_upd_rate: 3,
  },
]

const STANDBY_ID = '00000000-0000-4000-8000-000000000002'

function withStandby() {
  const [cluster] = clusters
  const [primary] = cluster!.instances
  return [
    {
      ...cluster!,
      instance_count: 2,
      instances: [
        primary,
        { ...primary, instance_id: STANDBY_ID, addr: 'standby.example.test', role: 'standby' },
      ],
    },
  ]
}

function render({
  items = ITEMS,
  tables = TABLES,
  cluster = clusters,
  windows = advisorWindows,
}: { items?: unknown[]; tables?: unknown[]; cluster?: unknown; windows?: unknown } = {}) {
  server.use(
    ok('getAdvisorWindows', windows),
    ok('getClusters', cluster),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items,
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getInstanceTables', {
      instance_id: INSTANCE_ID,
      items: tables,
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getFindings', FINDINGS),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route: '/indexes?tab=cleanup' })
}

describe('CleanupPanel', () => {
  it('CLEAN-UI-001 lists what looks removable and leaves the constraints out', async () => {
    render()
    await flushNetwork(30)

    const table = screen.getByRole('table', { name: 'Cleanup candidates' })
    // The header and the five candidates.
    expect(within(table).getAllByRole('row')).toHaveLength(6)
    expect(within(table).getByText('orders_bad_idx')).toBeInTheDocument()
    expect(within(table).getByText('orders_dup_idx')).toBeInTheDocument()
    expect(within(table).getByText('orders_old_idx')).toBeInTheDocument()
    expect(within(table).getByText('orders_raw_idx')).toBeInTheDocument()
    expect(within(table).queryByText('orders_pkey')).not.toBeInTheDocument()
    expect(within(table).queryByText('orders_a_idx')).not.toBeInTheDocument()
    expect(within(table).queryByText('orders_busy_idx')).not.toBeInTheDocument()
    expect(screen.getByRole('article', { name: 'Candidates' })).toHaveTextContent('5')
  })

  it('CLEAN-UI-002 shows the cost of the writes it can measure and says when it cannot', async () => {
    render()
    await flushNetwork(30)

    const rows = within(screen.getByRole('table', { name: 'Cleanup candidates' })).getAllByRole(
      'row',
    )
    const orders = rows.find((row) => row.textContent?.includes('orders_dup_idx'))
    const other = rows.find((row) => row.textContent?.includes('other_bad_idx'))
    expect(orders).toHaveTextContent(formatRate(4))
    expect(other).toHaveTextContent('Not measured')
    expect(screen.getByRole('article', { name: 'Index writes saved' })).toHaveTextContent(
      '1 not measured',
    )
  })

  it('CLEAN-UI-003 expands a candidate to the drop, the rebuild and the checklist', async () => {
    render()
    await flushNetwork(30)

    fireEvent.click(screen.getByRole('button', { name: 'Show the drop of orders_old_idx' }))
    expect(
      screen.getByText('DROP INDEX CONCURRENTLY IF EXISTS "public"."orders_old_idx";'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        'CREATE INDEX CONCURRENTLY orders_old_idx ON public.orders USING btree (customer_id);',
      ),
    ).toBeInTheDocument()
    expect(screen.getByText(/back a foreign key/)).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Copy drop statement of orders_old_idx' }),
    ).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Hide the drop of orders_old_idx' }))
    expect(screen.queryByText(/back a foreign key/)).not.toBeInTheDocument()
  })

  it('CLEAN-UI-004 names the index that stays when one is a duplicate', async () => {
    render()
    await flushNetwork(30)

    fireEvent.click(screen.getByRole('button', { name: 'Show the drop of orders_dup_idx' }))
    expect(screen.getByText('public.orders_a_idx')).toBeInTheDocument()
  })

  it('CLEAN-UI-005 says there is nothing to rebuild from when the definition is missing', async () => {
    render()
    await flushNetwork(30)

    fireEvent.click(screen.getByRole('button', { name: 'Show the drop of other_bad_idx' }))
    expect(screen.getByText(/did not report the definition/)).toBeInTheDocument()
  })

  it('CLEAN-UI-006 offers the whole script and says which candidates rest on raw counters', async () => {
    render()
    await flushNetwork(30)

    expect(screen.getByRole('button', { name: 'Copy cleanup script' })).toBeInTheDocument()
    expect(screen.getByText('1 candidate rests on raw counters.')).toBeInTheDocument()
    // The configured window (1 hour here), not a fixed 7 days (13.3).
    expect(
      screen.getByText(/The advisor calls an index unused only after 1 hour with no scan/),
    ).toBeInTheDocument()
    expect(screen.queryByText(/7 days/)).not.toBeInTheDocument()
  })

  it('CLEAN-UI-007 sends the reader to the standbys before a drop', async () => {
    render({ cluster: withStandby() })
    await flushNetwork(30)

    const link = screen.getAllByRole('link', { name: 'standby.example.test:5432' })[0]
    expect(link).toHaveAttribute('href', `/indexes?instance=${STANDBY_ID}&tab=cleanup`)
    expect(screen.getByText('This server is part of a cluster.')).toBeInTheDocument()
  })

  it('CLEAN-UI-008 does not talk about standbys on a lone server', async () => {
    render()
    await flushNetwork(30)

    expect(screen.queryByText('This server is part of a cluster.')).not.toBeInTheDocument()
  })

  it('CLEAN-UI-009 says so when there is nothing to clean up', async () => {
    render({ items: [index('orders_busy_idx')] })
    await flushNetwork(30)

    expect(screen.getByText('Nothing to clean up')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Copy cleanup script' })).not.toBeInTheDocument()
  })

  it('CLEAN-UI-010 says so when no index was reported', async () => {
    render({ items: [] })
    await flushNetwork(30)

    expect(screen.getByText(/No index usage statistics arrived from the agent/)).toBeInTheDocument()
  })

  it('CLEAN-UI-012 keeps the confirmed totals apart from the raw-counter ones', async () => {
    render()
    await flushNetwork(30)

    expect(screen.getByRole('article', { name: 'Candidates' })).toHaveTextContent(
      '4 confirmed · 1 raw counters',
    )
    expect(screen.getByRole('article', { name: 'Space to give back' })).toHaveTextContent(
      'on raw counters',
    )
    expect(screen.getByRole('article', { name: 'Index writes saved' })).toHaveTextContent(
      'on raw counters',
    )
  })

  it('CLEAN-UI-011 has no accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render()
    await screen.findByRole('table', { name: 'Cleanup candidates' })

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})

describe('CleanupPanel table controls', () => {
  it('TBL-UI-101 cleanup pages, searches, filters and sorts the candidates', async () => {
    const items = seq(25).map((n, position) =>
      index(`cand_${n}`, {
        index_bytes: (position + 1) * 1024,
        ...(position < 5
          ? { last_scan_status: 'never_since_reset', last_scan_at: null }
          : { is_valid: false }),
      }),
    )
    render({ items })
    await flushNetwork(30)
    const name = 'Cleanup candidates'

    expect(dataRows(name)).toHaveLength(20)
    filterTable(name, 'Reason', 'never-scanned')
    expect(dataRows(name)).toHaveLength(5)
    filterTable(name, 'Reason', '')
    searchTable(name, 'cand_2')
    expect(dataRows(name)).toHaveLength(6)
    searchTable(name, '')
    sortTable(name, 'Size', 'descending')
    expect(columnTexts(name, 1)[0]).toBe('cand_25')
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(dataRows(name)).toHaveLength(5)
  })
})

/** The header line of the open tab's panel. */
const panelHeader = () => screen.getByTestId('index-panel-header')

/** The status note that starts with `headline`. */
function noteFor(headline: string): HTMLElement {
  const note = screen
    .getAllByRole('status')
    .find((status) => status.textContent?.startsWith(headline))
  expect(note, headline).toBeDefined()
  return note!
}

describe('CleanupPanel on a Full HD screen (15.3)', () => {
  it('FHD-APPLY-036 one table under a one-row header: title, description, figures and notes, compact rows', async () => {
    render({ cluster: withStandby() })
    await flushNetwork(30)

    expect(screen.getAllByRole('table')).toHaveLength(1)
    const line = panelHeader()
    expect(line).toContainElement(
      screen.getByRole('heading', { name: 'Indexes that look removable' }),
    )
    expect(line).toHaveClass('min-[1600px]:flex-row')
    const tile = screen.getByRole('article', { name: 'Candidates' })
    expect(line).toContainElement(tile)
    // The breakdown leaves the line from 1600 px and stays in the title.
    expect(within(tile).getByText('4 confirmed · 1 raw counters')).toHaveClass(
      'min-[1600px]:hidden',
    )
    expect(tile).toHaveAttribute('title', 'Candidates: 5 indexes 4 confirmed · 1 raw counters')
    expect(line).toContainElement(screen.getByRole('button', { name: 'Copy cleanup script' }))
    for (const headline of [
      'This server is part of a cluster.',
      '1 candidate rests on raw counters.',
    ]) {
      const note = noteFor(headline)
      expect(line).toContainElement(note)
      expect(note.className).not.toMatch(/truncate/)
      expect(note.getAttribute('title')).toContain(headline)
    }
    const description = within(line).getByText(/^Indexes that look unused, duplicate or invalid/)
    expect(description).toHaveAttribute('title', description.textContent)
    expect(description).toHaveClass('min-[1600px]:line-clamp-3')
    // What the short line leaves out is in the card's ⓘ.
    expect(CAPTIONS['indexes.cleanup'].howToRead).toContain(
      'Primary keys and unique indexes are never listed',
    )
    expect(CAPTIONS['indexes.cleanup'].howToRead).toContain('pglens drops nothing')

    const table = screen.getByRole('table', { name: 'Cleanup candidates' })
    expect(table).toHaveClass('min-[1600px]:table-fixed')
    expect(within(table).getByTitle('orders_old_idx')).toHaveClass('min-[1600px]:truncate')
    expect(within(table).getAllByText('Unused')[0]).toHaveClass('min-[1600px]:py-0')
  })
})
