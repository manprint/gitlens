import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import IndexAnalysisPage from './IndexAnalysisPage'

import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as clusters } from '@/test/fixtures/getClusters'
import { base as history, empty as emptyHistory } from '@/test/fixtures/getInstanceTableHistory'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, filterTable, searchTable, seq, sortTable } from '@/test/table'

function table(name: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'app',
    schemaname: 'public',
    relname: name,
    n_live_tup: 1000,
    n_dead_tup: 0,
    total_bytes: 1_000_000,
    table_bytes: 800_000,
    ts: '2026-08-27T02:00:00Z',
    ...extra,
  }
}

const TABLES = [
  table('orders', {
    n_live_tup: 30_000,
    n_dead_tup: 12_000,
    total_bytes: 100_000_000,
    table_bytes: 80_000_000,
    last_autovacuum_at: '2026-08-26T22:00:00Z',
    hot_update_ratio: 0.2,
    n_tup_ins_rate: 1,
    n_tup_upd_rate: 3,
  }),
  table('events', { n_live_tup: 500, n_dead_tup: 5 }),
  table('customers', { n_live_tup: 100, last_vacuum_at: '2026-08-25T02:00:00Z' }),
]

const FINDINGS = [
  {
    finding_id: 'd1',
    rule_id: 'table.dead_tuples_high',
    severity: 'warning',
    state: 'open',
    scope: 'relation',
    datname: 'app',
    object_name: 'public.orders',
    title: 'Dead tuples are high',
    detail: '12000 dead tuples against 30000 live ones',
    remediation: 'Check the xmin horizon and the autovacuum settings',
  },
]

const SETTINGS = [
  { name: 'autovacuum_vacuum_threshold', value: '50' },
  { name: 'autovacuum_vacuum_scale_factor', value: '0.2' },
].map((setting) => ({
  unit: '',
  source: 'default',
  context: 'sighup',
  pending_restart: 'false',
  first_seen: '2026-08-27T01:00:00Z',
  last_seen: '2026-08-27T02:00:00Z',
  changed_at: '2026-08-27T01:00:00Z',
  ...setting,
}))

interface Options {
  tables?: unknown[]
  truncated?: boolean
  settings?: unknown[]
  trend?: unknown
  route?: string
}

function render({
  tables = TABLES,
  truncated = false,
  settings = SETTINGS,
  trend = history,
  route = '/indexes?tab=dead-rows',
}: Options = {}) {
  server.use(
    ok('getClusters', clusters),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items: [],
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getInstanceTables', {
      instance_id: INSTANCE_ID,
      items: tables,
      truncated,
      relations_not_reported: truncated ? 1 : null,
    }),
    ok('getInstanceSettings', { instance_id: INSTANCE_ID, settings }),
    ok('getInstanceTableHistory', trend),
    ok('getFindings', FINDINGS),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route })
}

describe('DeadRowsPanel', () => {
  it('DEAD-UI-001 ranks the tables by dead rows with the vacuum state beside them', async () => {
    render()
    await flushNetwork(30)

    const rows = within(screen.getByRole('table', { name: 'Tables' })).getAllByRole('row')
    // The header and one row per table, the worst first.
    expect(rows).toHaveLength(4)
    expect(rows[1]).toHaveTextContent('orders')
    expect(rows[1]).toHaveTextContent('40.0%')
    expect(rows[1]).toHaveTextContent('(auto)')
    expect(rows[1]).toHaveTextContent('flagged')
    expect(rows[2]).toHaveTextContent('events')
    expect(rows[2]).toHaveTextContent('Never')
    expect(screen.getByRole('article', { name: 'Tables with dead rows' })).toHaveTextContent('2')
    expect(screen.getByRole('article', { name: 'Dead rows' })).toHaveTextContent('12,005')
    expect(screen.getByRole('article', { name: 'Due for automatic vacuum' })).toHaveTextContent('1')
    expect(screen.getByRole('article', { name: 'Never vacuumed' })).toHaveTextContent('1')
    expect(screen.getByRole('article', { name: 'Worst table' })).toHaveTextContent('orders')
  })

  it('DEAD-UI-002 says the trigger is approximate until the instance settings are known', async () => {
    render({ settings: [] })
    await flushNetwork(30)

    expect(screen.getByText('Vacuum due is approximate.')).toBeInTheDocument()
  })

  it('DEAD-UI-003 does not call the trigger approximate when the settings are known', async () => {
    render()
    await flushNetwork(30)

    expect(screen.queryByText('Vacuum due is approximate.')).not.toBeInTheDocument()
  })

  it('DEAD-UI-004 filters the tables and searches them', async () => {
    render()
    await flushNetwork(30)

    const show = () => screen.getByRole('combobox', { name: 'Filter Tables by Show' })
    fireEvent.change(show(), { target: { value: 'due' } })
    let table_ = screen.getByRole('table', { name: 'Tables' })
    expect(within(table_).getAllByRole('row')).toHaveLength(2)
    expect(within(table_).getByText('orders')).toBeInTheDocument()

    fireEvent.change(show(), { target: { value: 'never' } })
    table_ = screen.getByRole('table', { name: 'Tables' })
    expect(within(table_).getByText('events')).toBeInTheDocument()

    fireEvent.change(show(), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Search tables'), { target: { value: 'custom' } })
    table_ = screen.getByRole('table', { name: 'Tables' })
    expect(within(table_).getAllByRole('row')).toHaveLength(2)
    expect(within(table_).getByText('customers')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Search tables'), { target: { value: 'zzz' } })
    expect(screen.getByText('No rows match')).toBeInTheDocument()
  })

  it('DEAD-UI-005 sorts by a column and flips the direction on a second click', async () => {
    render()
    await flushNetwork(30)

    fireEvent.click(screen.getByRole('button', { name: 'Table' }))
    let rows = within(screen.getByRole('table', { name: 'Tables' })).getAllByRole('row')
    expect(rows[1]).toHaveTextContent('customers')
    expect(screen.getByRole('columnheader', { name: /Table/ })).toHaveAttribute(
      'aria-sort',
      'ascending',
    )

    fireEvent.click(screen.getByRole('button', { name: 'Table' }))
    rows = within(screen.getByRole('table', { name: 'Tables' })).getAllByRole('row')
    expect(rows[1]).toHaveTextContent('orders')
  })

  it('DEAD-UI-006 expands a table to its write pattern and the advisor finding', async () => {
    render()
    await flushNetwork(30)

    fireEvent.click(screen.getByRole('button', { name: 'Show details of orders' }))
    expect(screen.getByText('Autovacuum starts at')).toBeInTheDocument()
    expect(screen.getByText(/Dead tuples are high\./)).toBeInTheDocument()
    expect(screen.getByText(/Check the xmin horizon/)).toBeInTheDocument()
    expect(screen.getByText(/Only 20.0% of the updates are heap-only/)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Hide details of orders' }))
    expect(screen.queryByText('Autovacuum starts at')).not.toBeInTheDocument()
  })

  it('DEAD-UI-007 draws the dead-tuple trend of the worst tables', async () => {
    render()
    await flushNetwork(30)

    expect(
      screen.getByRole('img', { name: 'Dead rows of orders over the selected range' }),
    ).toBeInTheDocument()
    expect(screen.getByText('growing')).toBeInTheDocument()
    // No series for the other tables: a dash, not an invented line.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)
  })

  it('DEAD-UI-008 draws no trend when the history is empty', async () => {
    render({ trend: emptyHistory })
    await flushNetwork(30)

    expect(screen.queryByRole('img', { name: /Dead rows of/ })).not.toBeInTheDocument()
  })

  it('DEAD-UI-009 warns that the list is incomplete when the agent truncated it', async () => {
    render({ truncated: true })
    await flushNetwork(30)

    expect(screen.getByText('The list is incomplete.')).toBeInTheDocument()
  })

  it('DEAD-UI-010 says so when no table was reported', async () => {
    render({ tables: [] })
    await flushNetwork(30)

    expect(screen.getByText('No tables to show')).toBeInTheDocument()
  })

  it('DEAD-UI-011 switches between the index inventory and the dead rows', async () => {
    render({ route: '/indexes' })
    await flushNetwork(30)

    expect(screen.queryByRole('table', { name: 'Tables' })).not.toBeInTheDocument()
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Dead rows' }))
    await flushNetwork(30)
    expect(screen.getByRole('table', { name: 'Tables' })).toBeInTheDocument()

    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Indexes' }))
    await flushNetwork(30)
    expect(screen.queryByRole('table', { name: 'Tables' })).not.toBeInTheDocument()
  })

  it('DEAD-UI-013 does not name a worst table when none holds enough dead tuples', async () => {
    render({ tables: [table('tiny', { n_live_tup: 3, n_dead_tup: 9 })] })
    await flushNetwork(30)

    expect(screen.getByRole('article', { name: 'Worst table' })).toHaveTextContent(
      'none past 10,000 dead rows',
    )
  })

  it('DEAD-UI-012 has no accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render()
    await screen.findByRole('table', { name: 'Tables' })

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})

describe('DeadRowsPanel table controls', () => {
  it('TBL-UI-102 dead-rows pages, searches, filters and sorts the tables', async () => {
    const tables = seq(25).map((n, position) =>
      table(`t_${n}`, {
        n_dead_tup: (position + 1) * 100,
        ...(position < 3 ? {} : { last_autovacuum_at: '2026-08-26T22:00:00Z' }),
      }),
    )
    render({ tables })
    await flushNetwork(30)
    const name = 'Tables'

    expect(dataRows(name)).toHaveLength(20)
    // compareDeadRows keeps its default: most dead tuples first.
    expect(columnTexts(name, 1)[0]).toContain('t_25')
    filterTable(name, 'Show', 'never')
    expect(dataRows(name)).toHaveLength(3)
    filterTable(name, 'Show', '')
    searchTable(name, 't_2', 'Search tables')
    expect(dataRows(name)).toHaveLength(6)
    searchTable(name, '', 'Search tables')
    sortTable(name, 'Table', 'ascending')
    expect(columnTexts(name, 1)[0]).toContain('t_01')
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

describe('DeadRowsPanel on a Full HD screen (15.3)', () => {
  it('FHD-APPLY-037 the title and description on a first row, the figures and notes on a second, each table row one line', async () => {
    render({ settings: [], truncated: true })
    await flushNetwork(30)

    expect(screen.getAllByRole('table')).toHaveLength(1)
    const header = panelHeader()
    const first = screen.getByTestId('index-panel-first-row')
    const second = screen.getByTestId('index-panel-second-row')
    expect(header).toContainElement(first)
    expect(header).toContainElement(second)
    expect(first).toContainElement(screen.getByRole('heading', { name: 'Tables by dead rows' }))
    expect(first).toHaveClass('min-[1600px]:flex-row')
    expect(second).toContainElement(screen.getByRole('article', { name: 'Dead rows' }))
    for (const headline of ['The list is incomplete.', 'Vacuum due is approximate.']) {
      const note = noteFor(headline)
      expect(second).toContainElement(note)
      expect(note.className).not.toMatch(/truncate/)
      expect(note.getAttribute('title')).toContain(headline)
    }

    const table = screen.getByRole('table', { name: 'Tables' })
    expect(table).toHaveClass('min-[1600px]:table-fixed')
    // Most dead tuples first: orders.
    const row = within(dataRows('Tables')[0]!)
    expect(row.getByText('orders')).toBeInTheDocument()
    // The second lines of a cell sit beside the figure from 1600 px.
    expect(row.getByText(/^≈/)).toHaveClass('min-[1600px]:inline')
    expect(row.getByText('flagged')).toHaveClass('min-[1600px]:py-0')
    expect(row.getByRole('img', { name: /Dead rows of orders/ })).toHaveClass('min-[1600px]:h-4')
    expect(row.getByRole('button', { expanded: false })).toHaveClass('min-[1600px]:size-5')
  })
})
