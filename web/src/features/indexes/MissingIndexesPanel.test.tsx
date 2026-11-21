import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { afterEach, describe, expect, it, vi } from 'vitest'

import IndexAnalysisPage from './IndexAnalysisPage'

import type { Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as clusters } from '@/test/fixtures/getClusters'
import { base, empty } from '@/test/fixtures/getIndexSuggestions'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { columnTexts, dataRows, searchTable, seq, sortTable } from '@/test/table'

type Finding = Schemas['Finding']

const NAME = 'Missing index candidates'
const SEARCH = 'Search missing indexes'
const FIRST = base.suggestions[0]!
const MIB = 1024 * 1024

function suggestion(name: string, extra: Record<string, unknown> = {}) {
  return {
    ...FIRST,
    id: `id-${name}`,
    table: name,
    columns: [{ name: `${name}_col`, desc: false }],
    create_statement: `CREATE INDEX CONCURRENTLY idx_${name} ON public.${name} (${name}_col);`,
    statements: [
      {
        queryid: '11',
        share_pct: 100,
        query_text: `SELECT id FROM ${name} WHERE ${name}_col = $1`,
      },
    ],
    ...extra,
  }
}

function fkFinding(extra: Record<string, unknown> = {}): Finding {
  return {
    finding_id: 'fk-1',
    rule_id: 'index.fk_unindexed',
    severity: 'warning',
    state: 'open',
    scope: 'relation',
    datname: 'odoo',
    object_name: 'public.orders.orders_customer_fk',
    title: 'Foreign key without an index',
    evidence: {
      table: 'public.orders',
      constraint: 'orders_customer_fk',
      columns: 'customer_id',
      referenced: 'public.customers',
      table_bytes: 20 * MIB,
      ...extra,
    },
  }
}

function table(name: string, extra: Record<string, unknown> = {}) {
  return {
    datname: 'odoo',
    schemaname: 'public',
    relname: name,
    table_bytes: 1 * MIB,
    seq_tup_read_rate: 0,
    seq_scan_rate: 0,
    n_tup_del_rate: 0,
    n_tup_upd_rate: 0,
    ...extra,
  }
}

function render(
  response: unknown = base,
  route = '/indexes?tab=missing',
  { findings = [], tables = [] }: { findings?: Finding[]; tables?: unknown[] } = {},
) {
  server.use(
    ok('getClusters', clusters),
    ok('getInstanceIndexes', {
      instance_id: INSTANCE_ID,
      items: [],
      truncated: false,
      relations_not_reported: null,
    }),
    ok('getFindings', findings),
    ok('getIndexSuggestions', response),
    ok('getInstanceTables', {
      instance_id: INSTANCE_ID,
      items: tables as never[],
      truncated: false,
      relations_not_reported: 0,
    }),
  )
  return renderWithProviders(<IndexAnalysisPage />, { route })
}

function stubClipboard(writeText: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'clipboard')
})

describe('MissingIndexesPanel', () => {
  it('MISS-UI-001 says the figures are estimates and never run by pglens', async () => {
    render()
    await flushNetwork(30)

    const callout = screen
      .getAllByRole('status')
      .find((status) => status.textContent?.includes('pglens never creates an index'))
    expect(callout).toBeDefined()
  })

  it('MISS-UI-002 renders a statement row with what to do, where it was found and why', async () => {
    render()
    await flushNetwork(30)

    const row = within(dataRows(NAME)[0]!)
    expect(row.getByText('Create')).toBeInTheDocument()
    expect(row.getByText('Statements')).toBeInTheDocument()
    expect(row.getByText('res_partner')).toBeInTheDocument()
    expect(row.getByText('ref')).toBeInTheDocument()
    expect(row.getByText(/the most the index could save/)).toBeInTheDocument()
    expect(row.getByRole('button', { name: 'Copy create statement' })).toBeInTheDocument()
  })

  it('MISS-UI-003 lists Create before Test first before Not worth it, and sorts by table', async () => {
    render({
      ...base,
      suggestions: [
        suggestion('ccc', { verdict: 'write_heavy' }),
        suggestion('bbb', { verdict: 'moderate' }),
        suggestion('aaa', { verdict: 'strong' }),
      ],
    })
    await flushNetwork(30)

    expect(columnTexts(NAME, 1)).toEqual(['Create', 'Test first', 'Not worth it'])
    sortTable(NAME, 'Table', 'descending')
    expect(columnTexts(NAME, 3).map((text) => text.slice(-3))).toEqual(['ccc', 'bbb', 'aaa'])
  })

  it('MISS-UI-004 searches the table, the columns and the statement text', async () => {
    render({
      ...base,
      suggestions: [
        suggestion('orders', {
          statements: [
            { queryid: '5', share_pct: 100, query_text: 'SELECT 1 WHERE flag_xyz = $1' },
          ],
        }),
        suggestion('events'),
        suggestion('users'),
      ],
    })
    await flushNetwork(30)

    searchTable(NAME, 'events', SEARCH)
    expect(dataRows(NAME)).toHaveLength(1)
    searchTable(NAME, 'users_col', SEARCH)
    expect(dataRows(NAME)).toHaveLength(1)
    searchTable(NAME, 'flag_xyz', SEARCH)
    expect(dataRows(NAME)).toHaveLength(1)
    expect(dataRows(NAME)[0]).toHaveTextContent('orders')
    searchTable(NAME, '', SEARCH)
    expect(dataRows(NAME)).toHaveLength(3)
  })

  it('MISS-UI-005 pages the table by 20 rows', async () => {
    render({ ...base, suggestions: seq(25).map((n) => suggestion(`t_${n}`)) })
    await flushNetwork(30)

    expect(dataRows(NAME)).toHaveLength(20)
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(dataRows(NAME)).toHaveLength(5)
  })

  it('MISS-UI-006 expands a row to the four steps and the statement analysis', async () => {
    render({
      ...base,
      suggestions: [
        suggestion('orders', {
          overlapping_indexes: ['orders_ref_hash'],
          notes: ['width_assumed'],
        }),
      ],
    })
    await flushNetwork(30)
    fireEvent.click(screen.getByRole('button', { name: /Show details of orders/ }))

    const steps = within(screen.getByRole('region', { name: 'What to do' }))
    expect(steps.getByText('1. Check.')).toBeInTheDocument()
    expect(steps.getByText('2. Create.')).toBeInTheDocument()
    expect(steps.getByText('3. Verify.')).toBeInTheDocument()
    expect(steps.getByText('4. Undo.')).toBeInTheDocument()
    expect(steps.getByText(/EXPLAIN \(ANALYZE, BUFFERS\)/)).toBeInTheDocument()
    expect(
      steps.getByText(/DROP INDEX CONCURRENTLY IF EXISTS public.idx_orders;/),
    ).toBeInTheDocument()
    expect(
      steps.getByRole('link', { name: /Help: how to act on a missing index/ }),
    ).toHaveAttribute('href', '/help/indexes#missing-act')

    const analysis = within(screen.getByRole('region', { name: 'Statement analysis' }))
    expect(analysis.getByText('SELECT id FROM orders WHERE orders_col = $1')).toBeInTheDocument()
    expect(analysis.getByRole('link', { name: '11' })).toHaveAttribute(
      'href',
      `/instances/${INSTANCE_ID}/queries/11`,
    )
    expect(analysis.getByText('orders_ref_hash')).toBeInTheDocument()
    expect(analysis.getByText(/32 bytes were assumed/)).toBeInTheDocument()
    expect(analysis.getByText('0.5 inserts/s · 1 updates/s · 0.1 deletes/s')).toBeInTheDocument()
  })

  it('MISS-UI-007 copies the create statement and announces Copied', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    stubClipboard(writeText)
    render()
    await flushNetwork(30)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy create statement' }))
      await Promise.resolve()
    })
    expect(writeText).toHaveBeenCalledWith(FIRST.create_statement)
    expect(screen.getByRole('button', { name: 'Create statement copied' })).toHaveTextContent(
      'Copied',
    )
  })

  it('MISS-UI-008 tells nothing missing from statistics not collected', async () => {
    const { unmount } = render({ ...base, suggestions: [] })
    await flushNetwork(30)
    expect(screen.getByRole('heading', { name: 'No missing index found' })).toBeInTheDocument()
    unmount()

    render(empty)
    await flushNetwork(30)
    expect(
      screen.getByRole('heading', { name: 'Statement statistics not collected' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /readiness/ })).toHaveAttribute(
      'href',
      `/instances/${INSTANCE_ID}`,
    )
  })

  it('MISS-UI-009 states how many statements were analysed, wrapping the line', async () => {
    render()
    await flushNetwork(30)

    const coverage = screen.getByTestId('suggestion-coverage')
    expect(coverage).toHaveTextContent(
      'Statements: Analysed 2 of 3 statements. Skipped (a statement can have several reasons): 1 no stored text.',
    )
    expect(coverage.className).not.toMatch(/truncate/)
  })

  it('MISS-UI-010 shows the response notes beside the estimate callout', async () => {
    const note =
      'The agent does not report every index definition: an index that already exists may be suggested again.'
    render({ ...base, notes: [note] })
    await flushNetwork(30)

    expect(screen.getByTestId('suggestion-coverage')).not.toHaveTextContent(note)
    expect(screen.getAllByRole('status').some((status) => status.textContent?.includes(note))).toBe(
      true,
    )
  })

  it('MISS-UI-011 has no accessibility violation', async () => {
    vi.useRealTimers()
    const { container } = render(base, '/indexes?tab=missing', {
      findings: [fkFinding()],
      tables: [table('customers', { n_tup_del_rate: 2 })],
    })
    await screen.findByRole('table', { name: NAME })
    fireEvent.click(screen.getByRole('button', { name: /Show details of res_partner/ }))

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('MISS-UI-012 opens from the old ?tab=suggestions link and keeps the window', async () => {
    render(base, '/indexes?tab=suggestions&window=6h')
    await flushNetwork(30)

    expect(screen.getByRole('tab', { name: 'Missing indexes' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('combobox', { name: 'Analysis window' })).toHaveValue('6h')
  })

  it('MISS-UI-013 shows an error, not "statistics not collected", when the analysis fails', async () => {
    render(base)
    server.use(
      http.get('/api/v1/instances/:id/index-suggestions', () =>
        HttpResponse.json({ error: 'analysis timed out' }, { status: 503 }),
      ),
    )
    await flushNetwork(60)

    expect(
      screen.queryByRole('heading', { name: 'Statement statistics not collected' }),
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
  })

  it('MISS-UI-014 links a statement by its exact 64-bit query id', async () => {
    render({
      ...base,
      suggestions: [
        suggestion('big', {
          statements: [{ queryid: '-6200000000000000000', share_pct: 100, query_text: 'SELECT 1' }],
        }),
      ],
    })
    await flushNetwork(30)
    fireEvent.click(screen.getByRole('button', { name: /Show details of big/ }))

    expect(screen.getByRole('link', { name: '-6200000000000000000' })).toHaveAttribute(
      'href',
      `/instances/${INSTANCE_ID}/queries/-6200000000000000000`,
    )
  })

  it('MISS-UI-015 lists an unindexed foreign key with its create statement', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    stubClipboard(writeText)
    render({ ...base, suggestions: [] }, '/indexes?tab=missing', {
      findings: [fkFinding()],
      tables: [table('customers', { n_tup_del_rate: 2 })],
    })
    await flushNetwork(30)

    const row = within(dataRows(NAME)[0]!)
    expect(row.getByText('Create')).toBeInTheDocument()
    expect(row.getByText('Foreign key')).toBeInTheDocument()
    expect(row.getByText('customer_id')).toBeInTheDocument()
    expect(row.getByText(/Rows of public.customers are being deleted now/)).toBeInTheDocument()
    await act(async () => {
      fireEvent.click(row.getByRole('button', { name: 'Copy create statement' }))
      await Promise.resolve()
    })
    expect(writeText).toHaveBeenCalledWith(
      'CREATE INDEX CONCURRENTLY IF NOT EXISTS orders_customer_id_idx ON public.orders (customer_id);',
    )
  })

  it('MISS-UI-016 asks to check the workload when no parent row is deleted', async () => {
    render({ ...base, suggestions: [] }, '/indexes?tab=missing', { findings: [fkFinding()] })
    await flushNetwork(30)

    expect(within(dataRows(NAME)[0]!).getByText('Check workload')).toBeInTheDocument()
  })

  it('MISS-UI-017 lists a table read by full scans with steps instead of a command', async () => {
    render({ ...base, suggestions: [] }, '/indexes?tab=missing', {
      tables: [
        table('timetable', { table_bytes: 10 * MIB, seq_tup_read_rate: 1000, seq_scan_rate: 2 }),
        table('tiny', { table_bytes: 1 * MIB, seq_tup_read_rate: 5000 }),
      ],
    })
    await flushNetwork(30)

    expect(dataRows(NAME)).toHaveLength(1)
    const row = within(dataRows(NAME)[0]!)
    expect(row.getByText('Full table scans')).toBeInTheDocument()
    expect(row.getByText('See steps')).toBeInTheDocument()
    expect(row.getByText(/about 500 rows per scan/)).toBeInTheDocument()
  })

  it('MISS-UI-018 copies a psql script of the list, check rows commented out', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    stubClipboard(writeText)
    render(base, '/indexes?tab=missing', { findings: [fkFinding()] })
    await flushNetwork(30)

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy missing index script' }))
      await Promise.resolve()
    })
    const script = (writeText.mock.calls[0] as unknown as [string])[0]
    expect(script).toContain('\\connect odoo')
    expect(script).toContain(`\n${FIRST.create_statement}\n`)
    expect(script).toContain(
      '-- CREATE INDEX CONCURRENTLY IF NOT EXISTS orders_customer_id_idx ON public.orders (customer_id);',
    )
  })

  it('MISS-UI-019 lists every source together and links the help chapter', async () => {
    render(base, '/indexes?tab=missing', { findings: [fkFinding()] })
    await flushNetwork(30)

    expect(dataRows(NAME)).toHaveLength(2)
    expect(screen.getByRole('link', { name: /Help: Missing indexes/ })).toHaveAttribute(
      'href',
      '/help/indexes#missing',
    )
  })
})
