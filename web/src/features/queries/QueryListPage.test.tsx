import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import QueryListPage from './QueryListPage'

import { REFRESH } from '@/api/policy'
import { expectNoA11yViolations } from '@/test/a11y'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { columnTexts, dataRows, seq, sortTable } from '@/test/table'
import { NOW } from '@/test/time'

const INSTANCE_ID = '11111111-1111-4111-8111-111111111111'

function statementResponse(overrides: Record<string, unknown> = {}) {
  return {
    comparable_scope: 'cluster',
    statements: [
      {
        calls: 12,
        datname: 'postgres',
        query_text: 'SELECT  *  FROM users WHERE id = $1',
        queryid: 123,
        rows: 12,
        total_exec_time_ms: 240,
        truncated: false,
      },
    ],
    truncated: false,
    ...overrides,
  }
}

function renderPage(route = `/instances/${INSTANCE_ID}/queries?range=1h`) {
  return renderWithProviders(<QueryListPage instanceId={INSTANCE_ID} />, { route })
}

function statementsHandler(body: object) {
  return http.get('*/api/v1/statements', () => HttpResponse.json(body))
}

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

describe('QueryListPage', () => {
  it('UI-QRY-010 changes the server-side order_by when sorting changes', async () => {
    const orderBys: string[] = []
    server.use(
      http.get('*/api/v1/statements', ({ request }) => {
        orderBys.push(new URL(request.url).searchParams.get('order_by') ?? '')
        return HttpResponse.json(statementResponse())
      }),
    )

    renderPage(`/instances/${INSTANCE_ID}/queries?range=1h&sort=calls`)

    await settle()
    expect(orderBys).toContain('calls')
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort statements' }), {
      target: { value: 'mean_exec_time' },
    })

    await settle()
    expect(orderBys).toContain('mean_exec_time')
    expect(screen.getByRole('combobox', { name: 'Sort statements' })).toHaveValue('mean_exec_time')
  })

  it('UI-QRY-015 ranks by temporary blocks and WAL and shows the WAL written', async () => {
    const orderBys: string[] = []
    server.use(
      http.get('*/api/v1/statements', ({ request }) => {
        orderBys.push(new URL(request.url).searchParams.get('order_by') ?? '')
        return HttpResponse.json(
          statementResponse({
            statements: [
              {
                calls: 4,
                datname: 'postgres',
                query_text: 'INSERT INTO audit SELECT * FROM staging',
                queryid: 7,
                wal_bytes: 3 * 1024 * 1024,
                temp_blks_written: 1024,
                truncated: false,
              },
            ],
          }),
        )
      }),
    )

    renderPage(`/instances/${INSTANCE_ID}/queries?range=1h&sort=temp_blks_written`)
    await settle()
    expect(orderBys).toContain('temp_blks_written')
    expect(screen.getByRole('combobox', { name: 'Sort statements' })).toHaveValue(
      'temp_blks_written',
    )
    expect(screen.getByText('3.0 MiB')).toBeInTheDocument()
    expect(screen.getByText('2.0 MiB')).toBeInTheDocument()

    fireEvent.change(screen.getByRole('combobox', { name: 'Sort statements' }), {
      target: { value: 'wal_bytes' },
    })
    await settle()
    expect(orderBys).toContain('wal_bytes')
  })

  it('UI-QRY-011 shows the configured truncation budget', async () => {
    server.use(
      statementsHandler(
        statementResponse({
          truncated: true,
        }),
      ),
    )

    renderPage()

    await settle()
    // The table takes no budget, so the one notice is the Truncated element below it.
    expect(screen.getAllByText(/truncated; budget 50/)).toHaveLength(1)
    expect(screen.getByText('checks.stat_statements.top_n')).toBeInTheDocument()
  })

  it('UI-QRY-012 renders Disabled when pg_stat_statements is absent', async () => {
    server.use(http.get('*/api/v1/statements', () => new HttpResponse(null, { status: 404 })))

    renderPage()

    await settle()
    expect(screen.getByText(/pg_stat_statements disabled/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /README extension guidance/i })).toHaveAttribute(
      'href',
      '/README.md#agent-configuration',
    )
  })

  it('UI-QRY-046 says why the agent skips the check when nothing was collected', async () => {
    server.use(
      statementsHandler(
        statementResponse({
          disabled: true,
          disabled_reason: 'missing extension pg_stat_statements',
          statements: [],
        }),
      ),
    )

    renderPage()

    await settle()
    expect(screen.getByRole('status')).toHaveTextContent(
      'pg_stat_statements disabled. The agent skips the stat_statements check: missing extension pg_stat_statements.',
    )
    expect(screen.queryByText(/configuration key/)).not.toBeInTheDocument()
  })

  it('UI-QRY-047 keeps statements collected before the check was skipped', async () => {
    server.use(
      statementsHandler(
        statementResponse({
          disabled: true,
          disabled_reason: 'missing extension pg_stat_statements',
        }),
      ),
    )

    renderPage()

    await settle()
    expect(screen.getByRole('link', { name: 'Query 123' })).toBeInTheDocument()
    expect(screen.getByText(/The statements below were collected earlier/)).toHaveTextContent(
      'The agent skips the stat_statements check: missing extension pg_stat_statements. The statements below were collected earlier.',
    )
  })

  it('UI-QRY-013 keeps the pg_stat_statements eviction and queryid scope note visible', async () => {
    server.use(statementsHandler(statementResponse({ statements: [] })))

    renderPage()

    await settle()
    expect(screen.getByText(/Entries beyond/)).toBeInTheDocument()
    expect(document.body.textContent).toContain(
      'queryid values are comparable only within this cluster',
    )
  })

  it('UI-QRY-014 links each row to its detail route without converting queryid to a number', async () => {
    server.use(
      statementsHandler(
        statementResponse({
          statements: [
            {
              calls: 1,
              datname: 'postgres',
              query_text: 'SELECT 1',
              queryid: '9223372036854775807',
              rows: 1,
              total_exec_time_ms: 2,
              truncated: false,
            },
          ],
        }),
      ),
    )

    renderPage()

    await settle()
    const link = screen.getByRole('link', { name: 'Query 9223372036854775807' })
    expect(link).toHaveAttribute(
      'href',
      `/instances/${INSTANCE_ID}/queries/9223372036854775807?db=postgres`,
    )
  })

  it('UI-QRY-016 labels the share column as a share of the listed rows', async () => {
    server.use(
      statementsHandler(
        statementResponse({
          statements: [
            {
              calls: 1,
              datname: 'postgres',
              query_text: 'SELECT 1',
              queryid: '1',
              rows: 1,
              total_exec_time_ms: 2,
              truncated: false,
            },
          ],
        }),
      ),
    )

    renderPage()

    await settle()
    expect(screen.getByRole('columnheader', { name: 'Share of listed rows' })).toBeInTheDocument()
    expect(screen.queryByRole('columnheader', { name: 'Share of total' })).toBeNull()
  })

  it('renders the populated statement list without accessibility violations', async () => {
    server.use(statementsHandler(statementResponse()))

    const { container } = renderPage()

    await settle()
    screen.getByRole('link', { name: 'Query 123' })
    vi.useRealTimers()
    await expectNoA11yViolations(container)
    expect(container).toBeTruthy()
  })

  it('UI-QRY-048 moves the statement window with the clock on each poll', async () => {
    const statementWindows: (string | null)[] = []
    server.events.on('request:start', ({ request }) => {
      const url = new URL(request.url)
      if (url.pathname.endsWith('/api/v1/statements')) {
        statementWindows.push(url.searchParams.get('to'))
      }
    })
    try {
      server.use(statementsHandler(statementResponse()))
      renderPage()
      await settle()
      vi.setSystemTime(new Date(NOW.getTime() + 2 * 60 * 1_000))
      await act(async () => {
        await vi.advanceTimersByTimeAsync(REFRESH.statements.interval + 1)
      })
      await settle()

      expect(statementWindows.length).toBeGreaterThanOrEqual(2)
      expect(statementWindows.at(-1)).not.toBe(statementWindows[0])
    } finally {
      server.events.removeAllListeners()
    }
  })
})

describe('QueryListPage table', () => {
  it('TBL-UI-027 statement-statistics pages and sorts the loaded statements', async () => {
    server.use(
      statementsHandler(
        statementResponse({
          statements: seq(25).map((n, index) => ({
            calls: index + 1,
            datname: 'postgres',
            query_text: `SELECT stmt_${n}`,
            queryid: 1_000 + index,
            rows: 1,
            total_exec_time_ms: 10,
            truncated: false,
          })),
        }),
      ),
    )
    const { router } = renderPage(
      `/instances/${INSTANCE_ID}/queries?range=1h&statement-statistics.page=2`,
    )
    await settle()
    const name = 'Statement statistics'

    expect(dataRows(name)).toHaveLength(5)
    // A new server ranking returns to page 1.
    fireEvent.change(screen.getByRole('combobox', { name: 'Sort statements' }), {
      target: { value: 'mean_exec_time' },
    })
    await settle()
    expect(router.state.location.search).not.toContain('statement-statistics.page')
    expect(dataRows(name)).toHaveLength(20)

    sortTable(name, 'Calls', 'descending')
    expect(columnTexts(name)[0]).toContain('stmt_25')
    sortTable(name, 'Calls', 'ascending')
    expect(columnTexts(name)[0]).toContain('stmt_01')
  })
})

describe('QueryListPage search (D20)', () => {
  function capture(requests: URL[], body: object) {
    return http.get('*/api/v1/statements', ({ request }) => {
      requests.push(new URL(request.url))
      return HttpResponse.json(body)
    })
  }

  const partner = statementResponse({
    statements: [
      {
        calls: 3,
        datname: 'odoo',
        query_text: 'SELECT id FROM res_partner WHERE name = $1',
        queryid: 77,
        rows: 3,
        total_exec_time_ms: 30,
        truncated: false,
      },
    ],
    has_more: false,
  })

  it('SRCH-UI-007 asks the server for ?q= and marks the matches in the query text', async () => {
    const requests: URL[] = []
    server.use(capture(requests, partner))
    renderPage(`/instances/${INSTANCE_ID}/queries?range=1h&q=res_partner`)
    await settle()

    expect(requests.at(-1)?.searchParams.get('q')).toBe('res_partner')
    expect(requests.at(-1)?.searchParams.get('order_by')).toBe('total_exec_time')
    expect(screen.getByRole('searchbox', { name: 'Search statements' })).toHaveValue('res_partner')
    // One search box: the table has no search of its own.
    expect(screen.getAllByRole('searchbox')).toHaveLength(1)
    const table = screen.getByRole('table', { name: 'Statement statistics' })
    expect(within(table).getAllByRole('mark')[0]).toHaveTextContent('res_partner')
    expect(screen.getByText(/1 match\./)).toBeInTheDocument()
  })

  it('SRCH-UI-007 typing searches after the wait and keeps the ranking', async () => {
    const requests: URL[] = []
    server.use(capture(requests, partner))
    const { router } = renderPage(`/instances/${INSTANCE_ID}/queries?range=1h&sort=calls`)
    await settle()
    expect(requests.at(-1)?.searchParams.has('q')).toBe(false)

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search statements' }), {
      target: { value: 'res_partner' },
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await settle()
    expect(router.state.location.search).toContain('q=res_partner')
    expect(router.state.location.search).toContain('sort=calls')
    expect(requests.at(-1)?.searchParams.get('q')).toBe('res_partner')
    expect(requests.at(-1)?.searchParams.get('order_by')).toBe('calls')
  })

  it('SRCH-UI-008 a search without matches shows the no-match state with a clear action', async () => {
    const requests: URL[] = []
    server.use(capture(requests, statementResponse({ statements: [], has_more: false })))
    const { router } = renderPage(`/instances/${INSTANCE_ID}/queries?range=1h&q=zzzzqqqq`)
    await settle()

    expect(
      screen.getByRole('heading', { name: 'No statement matches “zzzzqqqq” in this range' }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'No statements' })).not.toBeInTheDocument()
    // The box stays, so the term can be changed.
    expect(screen.getByRole('searchbox', { name: 'Search statements' })).toHaveValue('zzzzqqqq')
    fireEvent.click(screen.getByRole('button', { name: 'Show all statements' }))
    await settle()
    // The button is gone: the search box keeps the keyboard.
    expect(screen.getByRole('searchbox', { name: 'Search statements' })).toHaveFocus()
    expect(router.state.location.search).not.toContain('q=')
  })

  it('says when more statements match than were loaded (I-9)', async () => {
    server.use(
      capture(
        [],
        statementResponse({
          statements: seq(50).map((n, index) => ({
            calls: 1,
            datname: 'odoo',
            query_text: `SELECT ${n} FROM res_partner`,
            queryid: 2_000 + index,
            rows: 1,
            total_exec_time_ms: 1,
            truncated: false,
          })),
          has_more: true,
        }),
      ),
    )
    renderPage(`/instances/${INSTANCE_ID}/queries?range=1h&q=res_partner`)
    await settle()
    expect(screen.getByText(/More than 50 matches — narrow the search\./)).toBeInTheDocument()
  })

  it('keeps typing that goes on while a search loads, when the box moves out of the table', async () => {
    const requests: URL[] = []
    server.use(
      http.get('*/api/v1/statements', ({ request }) => {
        const url = new URL(request.url)
        requests.push(url)
        return HttpResponse.json(
          url.searchParams.has('q')
            ? statementResponse({ statements: [], has_more: false })
            : statementResponse(),
        )
      }),
    )
    const { router } = renderPage()
    await flushNetwork(30)
    expect(screen.getByRole('table', { name: 'Statement statistics' })).toBeInTheDocument()

    const box = () => screen.getByRole('searchbox', { name: 'Search statements' })
    act(() => box().focus())
    fireEvent.change(box(), { target: { value: 'zz' } })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    expect(router.state.location.search).toContain('q=zz')
    // More text before the answer to `zz`, which has no rows: the box leaves the toolbar.
    fireEvent.change(box(), { target: { value: 'zzqq' } })
    await flushNetwork(30)
    expect(
      screen.getByRole('heading', { name: 'No statement matches “zz” in this range' }),
    ).toBeInTheDocument()
    expect(box()).toHaveValue('zzqq')
    expect(box()).toHaveFocus()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await flushNetwork(30)
    expect(router.state.location.search).toContain('q=zzqq')
    expect(requests.at(-1)?.searchParams.get('q')).toBe('zzqq')
    expect(box()).toHaveValue('zzqq')
  })

  it('keeps the box when the server refuses the search, to change or clear it', async () => {
    const long = 'x'.repeat(201)
    server.use(
      http.get('*/api/v1/statements', ({ request }) =>
        new URL(request.url).searchParams.has('q')
          ? HttpResponse.json(
              { error: 'invalid q', detail: 'q must be 2–200 characters' },
              { status: 400 },
            )
          : HttpResponse.json(statementResponse()),
      ),
    )
    const { router } = renderPage(`/instances/${INSTANCE_ID}/queries?range=1h&q=${long}`)
    await flushNetwork(60)

    const box = screen.getByRole('searchbox', { name: 'Search statements' })
    expect(box).toHaveValue(long)
    expect(box).toHaveAttribute('maxlength', '200')
    // The term can be edited: the shorter text is searched after the wait.
    fireEvent.change(box, { target: { value: 'users' } })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await flushNetwork(60)
    expect(router.state.location.search).toContain('q=users')
    fireEvent.click(screen.getByRole('button', { name: 'Clear search' }))
    await flushNetwork(60)
    expect(router.state.location.search).not.toContain('q=')
    expect(screen.getByRole('table', { name: 'Statement statistics' })).toBeInTheDocument()
  })

  it('keeps the box, its text and focus while a corrected search loads after a failed one', async () => {
    let release: (() => void) | undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    server.use(
      http.get('*/api/v1/statements', async ({ request }) => {
        const q = new URL(request.url).searchParams.get('q')
        if (q === 'users') {
          await gate
          return HttpResponse.json(statementResponse({ has_more: false }))
        }
        return HttpResponse.json(
          { error: 'invalid q', detail: 'q must be 2–200 characters' },
          { status: 400 },
        )
      }),
    )
    renderPage(`/instances/${INSTANCE_ID}/queries?range=1h&q=${'x'.repeat(201)}`)
    await flushNetwork(60)

    const box = () => screen.getByRole('searchbox', { name: 'Search statements' })
    act(() => box().focus())
    fireEvent.change(box(), { target: { value: 'users' } })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await flushNetwork(30)
    expect(screen.getByText('Loading statements…')).toBeInTheDocument()
    expect(box()).toHaveValue('users')
    expect(box()).toHaveFocus()

    release?.()
    await flushNetwork(30)
    expect(screen.getByRole('table', { name: 'Statement statistics' })).toBeInTheDocument()
    expect(box()).toHaveFocus()
  })

  it('never labels the shown statements with a search that failed (I-9)', async () => {
    let release: (() => void) | undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const abc = {
      calls: 1,
      datname: 'odoo',
      query_text: 'SELECT abc FROM t',
      queryid: 41,
      rows: 1,
      total_exec_time_ms: 1,
      truncated: false,
    }
    server.use(
      http.get('*/api/v1/statements', async ({ request }) => {
        const q = new URL(request.url).searchParams.get('q')
        if (q === 'ab') {
          return HttpResponse.json({ error: 'search failed', detail: 'boom' }, { status: 500 })
        }
        if (q === 'abc') await gate
        return HttpResponse.json(
          statementResponse({ statements: [abc], ...(q === null ? {} : { has_more: false }) }),
        )
      }),
    )
    renderPage()
    await flushNetwork(30)
    const box = () => screen.getByRole('searchbox', { name: 'Search statements' })
    for (const value of ['ab', 'abc']) {
      fireEvent.change(box(), { target: { value } })
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300)
      })
      await flushNetwork(60)
    }
    expect(screen.queryAllByRole('mark')).toHaveLength(0)
    expect(screen.queryByText(/\bmatch(es)?\./)).not.toBeInTheDocument()

    release?.()
    await flushNetwork(30)
    expect(new Set(screen.getAllByRole('mark').map((mark) => mark.textContent))).toEqual(
      new Set(['abc']),
    )
  })

  it('an older link that searched the table now searches the server', async () => {
    const requests: URL[] = []
    server.use(capture(requests, partner))
    const { router } = renderPage(
      `/instances/${INSTANCE_ID}/queries?range=1h&statement-statistics.q=res_partner`,
    )
    await flushNetwork(30)
    expect(router.state.location.search).toContain('q=res_partner')
    expect(router.state.location.search).not.toContain('statement-statistics.q')
    expect(requests.at(-1)?.searchParams.get('q')).toBe('res_partner')
  })
})

describe('QueryListPage Full HD layout (D26)', () => {
  it('FHD-APPLY-005 shows the sort select in the section header and the query on one line', async () => {
    server.use(statementsHandler(statementResponse()))
    renderPage()
    await settle()
    expect(screen.getByRole('table', { name: 'Statement statistics' })).toBeInTheDocument()
    const list = screen.getByRole('region', { name: 'Statements' })
    const sort = within(list).getByLabelText('Sort statements')
    // The select sits in the section header, before the table.
    expect(
      sort.compareDocumentPosition(screen.getByRole('table', { name: 'Statement statistics' })) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Query 123' })).toHaveClass(
      'min-[1600px]:whitespace-nowrap',
    )
  })
})
