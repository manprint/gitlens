import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { qk } from '@/api/keys'
import { REFRESH } from '@/api/policy'
import { NOW } from '@/test/time'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { expectNoA11yViolations } from '@/test/a11y'
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

async function renderPage(route = '/findings') {
  const view = renderWithProviders(<FindingsPage />, { route })
  await settlePage()
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

async function settleMutation() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
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
      [finding({ state: 'degraded', severity: 'critical', scope: 'instance', cluster_id: '42', instance_id: '00000000-0000-0000-0000-000000000007' })],
      [rule()],
      '/findings?state=degraded&severity=critical&scope=instance&cluster_id=42&instance_id=00000000-0000-0000-0000-000000000007',
    )

    expect(screen.getByLabelText('State')).toHaveValue('degraded')
    expect(screen.getByLabelText('Severity')).toHaveValue('critical')
    expect(screen.getByLabelText('Scope')).toHaveValue('instance')
    expect(screen.getByLabelText('Cluster')).toHaveValue('42')
    expect(screen.getByLabelText('Instance')).toHaveValue('00000000-0000-0000-0000-000000000007')

    fireEvent.change(screen.getByLabelText('State'), { target: { value: 'all' } })
    fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'warning' } })
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: 'cluster' } })
    fireEvent.change(screen.getByLabelText('Cluster'), { target: { value: 'cluster-9' } })
    fireEvent.change(screen.getByLabelText('Instance'), { target: { value: 'instance-2' } })

    expect(view.router.state.location.search).toBe(
      '?state=all&severity=warning&scope=cluster&cluster_id=cluster-9&instance_id=instance-2',
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
    await renderFindings([
      finding({ evidence: { lag_seconds: 42, threshold: 10, primary: true } }),
    ])

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
      http.post(
        '*/api/v1/findings/:findingId/mute',
        () => HttpResponse.json({ error: 'finding mute failed', detail: 'maintenance denied' }, { status: 500 }),
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

  it('UI-FIND-024 unmute issues a DELETE and restores the previous state', async () => {
    let deleteCalled = false
    await renderFindings([finding({ state: 'muted', muted_until: mutedUntil, mute_reason: 'maintenance' })], [rule()], '/findings?state=all')
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
    expect(screen.getByText('open')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Mute finding' })).toBeInTheDocument()
    expect(screen.queryByText('Reason: maintenance')).not.toBeInTheDocument()
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

  it('has no serious or critical accessibility violations with a populated list', async () => {
    const view = await renderFindings([
      finding({ finding_id: 'one', title: 'Critical finding', severity: 'critical' }),
      finding({ finding_id: 'two', title: 'Degraded finding', state: 'degraded' }),
    ])

    expect(view.container).toBeTruthy()
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })
})
