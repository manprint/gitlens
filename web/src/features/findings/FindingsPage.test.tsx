import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { qk } from '@/api/keys'
import { REFRESH } from '@/api/policy'
import { NOW } from '@/test/time'
import { CLUSTER, CLUSTER_ID, INSTANCE_ID } from '@/test/fixture-helpers'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { expectNoA11yViolations } from '@/test/a11y'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import type { AdvisorRule, FindingRecord } from '@/lib/findings'

import { FindingsPage } from './FindingsPage'

const baseFinding: FindingRecord = {
  finding_id: 'finding-1',
  rule_id: 'rule-1',
  severity: 'warning',
  state: 'open',
  scope: 'cluster',
  title: 'Replication lag',
  evidence: {},
}

const baseRule: AdvisorRule = {
  id: 'rule-1',
  severity: 'warning',
  scope: 'cluster',
  needs: [],
  min_tier: 'T0',
  enabled: true,
  overridden: false,
}

const mutedUntil = '2026-08-27T04:00:00.000Z'

function finding(overrides: Partial<FindingRecord> = {}): FindingRecord {
  return { ...baseFinding, ...overrides }
}

function rule(overrides: Partial<AdvisorRule> = {}): AdvisorRule {
  return { ...baseRule, ...overrides }
}

async function settlePage() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

// The findings table shows one line per finding; the card with the detail,
// evidence and mute action opens under its row (15.3).
function expandFindings() {
  for (const button of screen.queryAllByRole('button', { name: /^Show details of / })) {
    fireEvent.click(button)
  }
}

async function renderPage(route = '/findings') {
  const view = renderWithProviders(<FindingsPage />, { route })
  await settlePage()
  expandFindings()
  return view
}

async function renderFindings(
  findings: FindingRecord[] = [finding()],
  rules: AdvisorRule[] = [rule()],
  route = '/findings',
) {
  server.use(ok('getFindings', findings), ok('getAdvisorRules', rules))
  return renderPage(route)
}

// The Cluster and Instance filters list the fleet (10.2); every test gets one cluster.
beforeEach(() => {
  server.use(ok('getClusters', [CLUSTER]))
})

async function settleMutation() {
  await flushNetwork()
}

describe('FindingsPage', () => {
  afterEach(() => server.resetHandlers())

  it('UI-FIND-010 shows open severity counts separately from rules not evaluated', async () => {
    await renderFindings([
      finding({ finding_id: 'critical', severity: 'critical' }),
      finding({ finding_id: 'warning', severity: 'warning' }),
      finding({ finding_id: 'info', severity: 'info' }),
      finding({ finding_id: 'degraded', state: 'degraded' }),
    ])

    expect(screen.getByRole('button', { name: /Critical\s+1/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Warning\s+1/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Info\s+1/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Rules not evaluated\s+1/ })).toBeInTheDocument()
  })

  it('UI-FIND-011 hides muted and resolved findings in the default view and states their counts', async () => {
    await renderFindings([
      finding({ finding_id: 'open', title: 'Visible open' }),
      finding({ finding_id: 'degraded', state: 'degraded', title: 'Visible degraded' }),
      finding({ finding_id: 'muted', state: 'muted', title: 'Hidden muted' }),
      finding({ finding_id: 'resolved', state: 'resolved', title: 'Hidden resolved' }),
    ])

    expect(screen.getByRole('heading', { name: 'Visible open' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Visible degraded' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Hidden muted' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Hidden resolved' })).not.toBeInTheDocument()
    expect(screen.getByText(/1 muted and 1 resolved findings are hidden/)).toBeInTheDocument()
  })

  it('UI-FIND-012 round-trips state, severity, scope, cluster, and instance filters through the URL', async () => {
    const view = await renderFindings(
      [
        finding({
          state: 'degraded',
          severity: 'critical',
          scope: 'instance',
          cluster_id: '42',
          instance_id: '00000000-0000-0000-0000-000000000007',
        }),
      ],
      [rule()],
      '/findings?state=degraded&severity=critical&scope=instance&cluster_id=42&instance_id=00000000-0000-0000-0000-000000000007',
    )

    expect(screen.getByLabelText('State')).toHaveValue('degraded')
    expect(screen.getByLabelText('Severity')).toHaveValue('critical')
    expect(screen.getByLabelText('Scope')).toHaveValue('instance')
    // Ids the fleet does not list stay selected, as themselves.
    expect(screen.getByLabelText('Cluster')).toHaveValue('42')
    expect(screen.getByLabelText('Instance')).toHaveValue('00000000-0000-0000-0000-000000000007')

    fireEvent.change(screen.getByLabelText('State'), { target: { value: 'all' } })
    fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'warning' } })
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: 'cluster' } })
    fireEvent.change(screen.getByLabelText('Cluster'), { target: { value: CLUSTER_ID } })
    fireEvent.change(screen.getByLabelText('Instance'), { target: { value: INSTANCE_ID } })

    expect(view.router.state.location.search).toBe(
      `?state=all&severity=warning&scope=cluster&cluster_id=${CLUSTER_ID}&instance_id=${INSTANCE_ID}`,
    )
  })

  it('UI-FIND-013 names missing catalogue metrics, checks, and host views for degraded findings', async () => {
    await renderFindings(
      [finding({ state: 'degraded', title: 'Missing inputs' })],
      [rule({ needs: ['metric:replication_lag', 'check:host', 'host'] })],
    )

    expect(screen.getByText(/metric replication_lag/)).toBeInTheDocument()
    expect(screen.getByText(/check host/)).toBeInTheDocument()
    expect(screen.getByText(/host view/)).toBeInTheDocument()
  })

  it('UI-FIND-014 explains that a degraded seven-day rule is waiting for history', async () => {
    await renderFindings(
      [finding({ state: 'degraded', title: 'History unavailable' })],
      [rule({ needs: ['history_7d'] })],
    )

    const card = screen.getByRole('article', { name: 'History unavailable' })
    expect(within(card).getByRole('status')).toHaveTextContent(/seven days of history/)
    expect(screen.getByText(/^Remedy: waiting for seven days of history\.$/)).toBeInTheDocument()
  })

  it('UI-FIND-015 names the required permission tier and grant remedy', async () => {
    await renderFindings(
      [finding({ state: 'degraded', title: 'Permission unavailable' })],
      [rule({ min_tier: 'T1' })],
    )

    expect(screen.getByText(/permission tier T1/)).toBeInTheDocument()
    expect(screen.getByText(/grant T1 access/)).toBeInTheDocument()
  })

  it('UI-FIND-016 renders evidence values', async () => {
    await renderFindings([finding({ evidence: { lag_seconds: 42, threshold: 10, primary: true } })])

    expect(screen.getByText('lag_seconds')).toBeInTheDocument()
    expect(screen.getByText('42')).toBeInTheDocument()
    expect(screen.getByText('true')).toBeInTheDocument()
  })

  it('UI-FIND-040 presents an affirmative empty state with the evaluated rule count', async () => {
    await renderFindings([], [rule(), rule({ id: 'rule-2', severity: 'info', scope: 'instance' })])

    expect(screen.getByRole('heading', { name: 'No active findings' })).toBeInTheDocument()
    expect(screen.getByText(/No findings are currently firing/)).toBeInTheDocument()
    expect(screen.getByText(/2 advisor rules evaluated successfully/)).toBeInTheDocument()
  })

  it('UI-FIND-041 makes an all-degraded result explicit instead of presenting it as healthy', async () => {
    await renderFindings([finding({ state: 'degraded', title: 'Evaluation unavailable' })])

    expect(screen.getByText(/not a healthy “no findings” result/i)).toBeInTheDocument()
  })

  it('UI-FIND-042 keeps findings visible when the rule catalogue fails', async () => {
    server.use(
      ok('getFindings', [finding()]),
      status('getAdvisorRules', 500, { error: 'internal_error', detail: 'catalogue unavailable' }),
    )
    await renderPage()

    expect(screen.getByRole('heading', { name: 'Replication lag' })).toBeInTheDocument()
    expect(screen.getByText(/explanations are unavailable/i)).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load advisor rules')
  })

  it('UI-FIND-047 does not call a degraded finding a missing rule when the catalogue fails to load', async () => {
    server.use(
      ok('getFindings', [finding({ state: 'degraded', title: 'Evaluation unavailable' })]),
      status('getAdvisorRules', 500, { error: 'internal_error', detail: 'catalogue unavailable' }),
    )
    await renderPage()

    const card = screen.getByRole('article', { name: 'Evaluation unavailable' })
    expect(within(card).queryByText(/collect the missing inputs/)).not.toBeInTheDocument()
    expect(within(card).queryByText('Rule metadata unavailable')).not.toBeInTheDocument()
    expect(screen.getByText(/explanations are unavailable/i)).toBeInTheDocument()
  })

  it('UI-FIND-043 renders Stale after the last findings snapshot ages past its threshold', async () => {
    const view = await renderFindings()

    view.queryClient.setQueryData(
      qk.findings('all', undefined, undefined, undefined, undefined, undefined, undefined, 1000),
      [finding()],
      { updatedAt: Date.now() - REFRESH.findings.staleAfter },
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })

    expect(screen.getByRole('status', { name: /Stale data:/i })).toBeInTheDocument()
  })

  it('UI-FIND-044 navigates a 401 to login exactly once', async () => {
    server.use(status('getFindings', 401), ok('getAdvisorRules', [rule()]))
    const view = await renderPage()

    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe('?next=%2Ffindings')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.findings.interval * 2)
    })
    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe('?next=%2Ffindings')
  })

  it('UI-FIND-045 renders a retryable server error and recovers on retry', async () => {
    server.use(
      status('getFindings', 500, { error: 'internal_error', detail: 'findings unavailable' }),
      ok('getAdvisorRules', [rule()]),
    )
    await renderPage()

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load findings')
    server.use(ok('getFindings', [finding()]))
    fireEvent.click(screen.getByRole('button', { name: 'Retry findings' }))
    await settlePage()
    await settlePage()
    expandFindings()

    expect(screen.getByRole('heading', { name: 'Replication lag' })).toBeInTheDocument()
  })

  it('UI-FIND-046 polls findings at the configured interval', async () => {
    server.use(sequence('getFindings', [finding()], []), ok('getAdvisorRules', [rule()]))
    await renderPage()

    expect(screen.getByRole('heading', { name: 'Replication lag' })).toBeInTheDocument()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.findings.interval)
      await vi.runOnlyPendingTimersAsync()
    })
    await settlePage()

    expect(screen.getByText(/No findings are currently firing/)).toBeInTheDocument()
  })

  it('UI-FIND-020 the mute dialog requires a reason', async () => {
    await renderFindings([finding()], [rule()], '/findings?state=all')

    fireEvent.click(screen.getByRole('button', { name: 'Mute finding' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Mute finding' }))

    expect(within(dialog).getByRole('alert')).toHaveTextContent(/reason is required/i)
  })

  it('UI-FIND-021 the mute request carries reason and until', async () => {
    let requestBody: unknown
    const response = {
      finding_id: 'finding-1',
      state: 'muted' as const,
      muted_until: mutedUntil,
      mute_reason: 'maintenance window',
    }
    await renderFindings([finding()], [rule()], '/findings?state=all')
    server.use(
      ok('getFindings', [finding(response)]),
      http.post('*/api/v1/findings/:findingId/mute', async ({ request }) => {
        requestBody = await request.json()
        return HttpResponse.json(response)
      }),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Mute finding' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Reason'), {
      target: { value: 'maintenance window' },
    })
    fireEvent.click(within(dialog).getByLabelText('1 hour'))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Mute finding' }))
    await settleMutation()

    expect(requestBody).toEqual({
      reason: 'maintenance window',
      until: new Date(NOW.getTime() + 60 * 60 * 1000).toISOString(),
    })
    expect(screen.getByText('Reason: maintenance window')).toBeInTheDocument()
  })

  it('UI-FIND-049 offers no explicit mute time past the server limit of ninety days', async () => {
    await renderFindings()

    fireEvent.click(screen.getByRole('button', { name: 'Mute finding' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByLabelText('Explicit timestamp'))

    // NOW is 2026-08-27T02:00 UTC, so ninety days later is 2026-11-25T02:00.
    expect(within(dialog).getByLabelText('Mute until')).toHaveAttribute('max', '2026-11-25T02:00')
  })

  it('UI-FIND-022 the dialog states that muting is not resolution', async () => {
    await renderFindings()

    fireEvent.click(screen.getByRole('button', { name: 'Mute finding' }))
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent(/muting does not resolve this finding/i)
    expect(within(dialog).getByRole('button', { name: 'Mute finding' })).not.toHaveTextContent(
      /resolve/i,
    )
  })

  it('UI-FIND-023 a failed mute leaves the finding unmuted and shows the error', async () => {
    server.use(
      http.post('*/api/v1/findings/:findingId/mute', () =>
        HttpResponse.json(
          { error: 'finding mute failed', detail: 'maintenance denied' },
          { status: 500 },
        ),
      ),
    )
    await renderFindings()

    fireEvent.click(screen.getByRole('button', { name: 'Mute finding' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'maintenance' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Mute finding' }))
    await settleMutation()

    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent(/could not mute the finding/i)
    expect(screen.queryByText(/remaining mute time/i)).not.toBeInTheDocument()
  })

  it('reports a malformed mute response without muting the finding', async () => {
    server.use(
      http.post('*/api/v1/findings/:findingId/mute', () => new HttpResponse(null, { status: 200 })),
    )
    await renderFindings()

    fireEvent.click(screen.getByRole('button', { name: 'Mute finding' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'maintenance' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Mute finding' }))
    await settleMutation()

    expect(screen.getByRole('alert')).toHaveTextContent(/could not mute the finding/i)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('reports a network failure while muting the finding', async () => {
    server.use(http.post('*/api/v1/findings/:findingId/mute', () => HttpResponse.error()))
    await renderFindings()

    fireEvent.click(screen.getByRole('button', { name: 'Mute finding' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Reason'), { target: { value: 'maintenance' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Mute finding' }))
    await settleMutation()

    expect(screen.getByRole('alert')).toHaveTextContent(/could not mute the finding/i)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('UI-FIND-024 unmute issues a DELETE and restores the previous state', async () => {
    let deleteCalled = false
    await renderFindings(
      [finding({ state: 'muted', muted_until: mutedUntil, mute_reason: 'maintenance' })],
      [rule()],
      '/findings?state=all',
    )
    server.use(
      ok('getFindings', [finding()]),
      http.delete('*/api/v1/findings/:findingId/mute', () => {
        deleteCalled = true
        return new HttpResponse(null, { status: 204 })
      }),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Unmute finding' }))
    await settleMutation()

    expect(deleteCalled).toBe(true)
    // The row repeats the state, so the card is the one to read.
    expect(
      within(screen.getByRole('article', { name: 'Replication lag' })).getByText('open'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Mute finding' })).toBeInTheDocument()
    expect(screen.queryByText('Reason: maintenance')).not.toBeInTheDocument()
  })

  it('reports an unmute failure and keeps the finding muted', async () => {
    await renderFindings(
      [finding({ state: 'muted', muted_until: mutedUntil, mute_reason: 'maintenance' })],
      [rule()],
      '/findings?state=all',
    )
    server.use(
      http.delete('*/api/v1/findings/:findingId/mute', () =>
        HttpResponse.json(
          { error: 'finding unmute failed', detail: 'maintenance denied' },
          { status: 500 },
        ),
      ),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Unmute finding' }))
    await settleMutation()

    expect(screen.getByRole('alert')).toHaveTextContent(/could not remove the mute/i)
    expect(screen.getByText('Reason: maintenance')).toBeInTheDocument()
  })

  it('reports a network failure while unmuting the finding', async () => {
    await renderFindings(
      [finding({ state: 'muted', muted_until: mutedUntil, mute_reason: 'maintenance' })],
      [rule()],
      '/findings?state=all',
    )
    server.use(http.delete('*/api/v1/findings/:findingId/mute', () => HttpResponse.error()))

    fireEvent.click(screen.getByRole('button', { name: 'Unmute finding' }))
    await settleMutation()

    expect(screen.getByRole('alert')).toHaveTextContent(/could not remove the mute/i)
    expect(screen.getByText('Reason: maintenance')).toBeInTheDocument()
  })

  it('UI-FIND-025 a muted finding shows its reason and remaining time', async () => {
    await renderFindings(
      [finding({ state: 'muted', muted_until: mutedUntil, mute_reason: 'maintenance window' })],
      [rule()],
      '/findings?state=muted',
    )

    expect(screen.getByText('Reason: maintenance window')).toBeInTheDocument()
    expect(screen.getByText(/Remaining mute time: 2 hours/)).toBeInTheDocument()
  })

  it('keeps the mute dialog accessible', async () => {
    const view = await renderFindings()
    fireEvent.click(screen.getByRole('button', { name: 'Mute finding' }))
    const dialog = screen.getByRole('dialog')
    expect(dialog).toBeInTheDocument()

    vi.useRealTimers()
    await expectNoA11yViolations(dialog)
    view.unmount()
  })

  it('UI-FIND-026 names each card by its title when the finding id holds spaces', async () => {
    await renderFindings([
      finding({
        finding_id: 'table.bloat_high/my app/public.order items',
        title: 'Table bloat on order items',
      }),
    ])

    expect(screen.getByRole('article', { name: 'Table bloat on order items' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Evidence' })).toBeInTheDocument()
  })

  it('UI-FIND-050 marks each card with its finding id when titles repeat', async () => {
    await renderFindings([
      finding({ finding_id: 'index.unused/orders_pkey', title: 'Unused index' }),
      finding({ finding_id: 'index.unused/items_pkey', title: 'Unused index' }),
    ])

    const cards = screen.getAllByRole('article', { name: 'Unused index' })
    expect(cards).toHaveLength(2)
    expect(cards[0]).toHaveAttribute('data-finding-id', 'index.unused/orders_pkey')
    expect(cards[1]).toHaveAttribute('data-finding-id', 'index.unused/items_pkey')
  })

  it('UI-FIND-027 warns when the server limit cut the findings', async () => {
    await renderFindings(
      Array.from({ length: 1000 }, (_, index) => finding({ finding_id: `finding-${index}` })),
    )

    expect(screen.getByText(/Only the first 1000 findings were returned/)).toBeInTheDocument()
  })

  it('does not warn about a limit the findings did not reach', async () => {
    await renderFindings()

    expect(screen.queryByText(/Only the first 1000 findings were returned/)).not.toBeInTheDocument()
  })

  it('has no serious or critical accessibility violations with a populated list', async () => {
    const view = await renderFindings([
      finding({ finding_id: 'one', title: 'Critical finding', severity: 'critical' }),
      finding({ finding_id: 'two', title: 'Degraded finding', state: 'degraded' }),
    ])

    expect(view.container).toBeTruthy()
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })

  it('FHD-APPLY-010 shows the findings and the rule catalogue as tabs in the page header, the default never written', async () => {
    const view = await renderFindings()

    // The tab list shares the page header line.
    const header = screen
      .getAllByRole('banner')
      .find((banner) => within(banner).queryByRole('heading', { level: 1 }))
    expect(header).toBeDefined()
    const tabs = within(header!).getByRole('tablist', { name: 'Findings views' })
    expect(within(tabs).getByRole('tab', { name: 'Findings' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('table', { name: 'Advisor findings' })).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: 'Advisor rule catalogue' })).not.toBeInTheDocument()

    fireEvent.mouseDown(within(tabs).getByRole('tab', { name: 'Rule catalogue' }))
    expect(view.router.state.location.search).toBe('?view=rules')
    expect(screen.getByRole('table', { name: 'Advisor rule catalogue' })).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: 'Advisor findings' })).not.toBeInTheDocument()
    expect(screen.getAllByRole('table')).toHaveLength(1)

    fireEvent.mouseDown(within(tabs).getByRole('tab', { name: 'Findings' }))
    expect(view.router.state.location.search).toBe('')
  })

  it('FHD-APPLY-011 opens the tab a view parameter names', async () => {
    await renderFindings([finding()], [rule()], '/findings?view=rules&state=all')
    expect(screen.getByRole('tab', { name: 'Rule catalogue' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('table', { name: 'Advisor rule catalogue' })).toBeInTheDocument()
  })

  it('FHD-APPLY-012 treats an unknown view as the findings tab', async () => {
    await renderFindings([finding()], [rule()], '/findings?view=unknown')

    expect(screen.getByRole('tab', { name: 'Findings' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('table', { name: 'Advisor findings' })).toBeInTheDocument()
  })

  it('FHD-APPLY-013 lists each finding on one line and opens its card under the row', async () => {
    server.use(
      ok('getFindings', [
        finding({ object_name: 'public.orders', datname: 'shop', title: 'Long finding title' }),
      ]),
      ok('getAdvisorRules', [rule()]),
    )
    renderWithProviders(<FindingsPage />, { route: '/findings' })
    await settlePage()

    const table = screen.getByRole('table', { name: 'Advisor findings' })
    expect(table).toHaveClass('min-[1600px]:table-fixed')
    expect(screen.queryByRole('article')).not.toBeInTheDocument()
    const title = within(table).getByTitle('Long finding title')
    expect(title).toHaveClass('min-[1600px]:truncate')
    expect(within(table).getByTitle('public.orders · database shop')).toHaveClass(
      'min-[1600px]:truncate',
    )
    expect(screen.getByRole('button', { name: 'Show details of Long finding title' })).toHaveClass(
      'min-[1600px]:size-5',
    )

    fireEvent.click(screen.getByRole('button', { name: 'Show details of Long finding title' }))
    expect(screen.getByRole('article', { name: 'Long finding title' })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Hide details of Long finding title' }),
    ).toBeInTheDocument()
  })

  it('FHD-APPLY-014 shows 20 findings per page and a new filter returns to the first page', async () => {
    server.use(
      ok(
        'getFindings',
        Array.from({ length: 25 }, (_, index) =>
          finding({ finding_id: `finding-${index}`, title: `Finding ${index}` }),
        ),
      ),
      ok('getAdvisorRules', [rule()]),
    )
    const view = renderWithProviders(<FindingsPage />, { route: '/findings?findings.page=2' })
    await settlePage()

    const rows = within(screen.getByTestId('simple-table-rows')).getAllByRole('row')
    expect(rows).toHaveLength(5)

    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: 'cluster' } })
    expect(view.router.state.location.search).toBe('?scope=cluster')
    expect(within(screen.getByTestId('simple-table-rows')).getAllByRole('row')).toHaveLength(20)
  })

  it('FHD-APPLY-015 puts the summary in the section header and the hidden counts in its truncated description', async () => {
    await renderFindings([
      finding({ finding_id: 'open', title: 'Visible open' }),
      finding({ finding_id: 'muted', state: 'muted', title: 'Hidden muted' }),
    ])

    const section = screen.getByRole('region', { name: 'Findings' })
    const summary = within(section).getByRole('group', { name: 'Finding summary' })
    // The header of the section holds the summary; the table sits in its body.
    expect(summary.compareDocumentPosition(screen.getByRole('table'))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    )
    expect(within(summary).getByRole('button', { name: /Warning\s+1/ })).toHaveClass(
      'min-[1600px]:h-6',
    )
    const description = within(section).getByText(/0 resolved findings are hidden/)
    expect(description).toHaveClass('min-[1600px]:truncate')
    expect(description).toHaveAttribute(
      'title',
      expect.stringMatching(/^1 of 2 findings shown\. By default, 1 muted/),
    )
  })

  it('FHD-APPLY-016 keeps the filters on one line, their names hidden from 1600 px', async () => {
    await renderFindings()

    const filters = screen.getByRole('group', { name: 'Finding filters' })
    for (const name of ['State', 'Severity', 'Scope', 'Cluster', 'Instance']) {
      expect(within(filters).getByLabelText(name)).toHaveClass('h-8')
      const caption = within(filters).getByText(name, { selector: 'span' })
      expect(caption).toHaveClass('min-[1600px]:hidden')
      expect(caption).toHaveAttribute('aria-hidden', 'true')
    }
  })

  it('FHD-APPLY-017 states the server limit on the filter line, truncated with the full text in its title', async () => {
    await renderFindings(
      Array.from({ length: 1000 }, (_, index) => finding({ finding_id: `finding-${index}` })),
    )

    const note = within(screen.getByRole('group', { name: 'Finding filters' })).getByText(
      /Only the first 1000 findings were returned/,
    )
    expect(note).toHaveClass('min-[1600px]:truncate')
    expect(note).toHaveAttribute('title', expect.stringMatching(/may be missing/))
    // One page of rows, each with its expand button.
    expect(screen.getAllByRole('button', { name: /^(Show|Hide) details of / })).toHaveLength(20)
  })
})

describe('FindingsPage cluster and instance filters', () => {
  it('CLNAME-UI-013 list the fleet by name, value the id, with All first', async () => {
    server.use(
      ok('getClusters', [
        {
          ...CLUSTER,
          name: 'odoo-prod',
          instances: [{ ...CLUSTER.instances[0], display_name: 'odoo-db-1' }],
        },
      ]),
      ok('getFindings', [finding()]),
      ok('getAdvisorRules', [rule()]),
    )
    const view = renderWithProviders(<FindingsPage />, { route: '/findings' })
    await flushNetwork()

    const cluster = screen.getByRole('combobox', { name: 'Cluster' })
    expect(
      within(cluster)
        .getAllByRole('option')
        .map((option) => option.textContent),
    ).toEqual(['All clusters', 'odoo-prod'])
    const instance = screen.getByRole('combobox', { name: 'Instance' })
    expect(
      within(instance)
        .getAllByRole('option')
        .map((option) => option.textContent),
    ).toEqual(['All instances', 'odoo-db-1'])
    fireEvent.change(instance, { target: { value: INSTANCE_ID } })
    expect(view.router.state.location.search).toBe(`?instance_id=${INSTANCE_ID}`)
    fireEvent.change(instance, { target: { value: '' } })
    expect(view.router.state.location.search).toBe('')
  })
})
