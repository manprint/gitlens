import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'

import type { Alert, Silence } from '@/lib/alerts'
import { expectNoA11yViolations } from '@/test/a11y'
import { assertMatchesContract } from '@/test/contract'
import { NOW } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'

import { SilencesPage } from './SilencesPage'

function makeAlert(overrides: Partial<Alert> = {}): Alert {
  return {
    alert_key: 'replication-lag',
    datname: 'app',
    labels: { cluster: 'production' },
    last_eval_at: NOW,
    resolved_at: null,
    rule_id: 'replication-lag',
    severity: 'warning',
    started_at: NOW,
    state: 'firing',
    summary: 'Replication lag is above the threshold',
    suppressed: false,
    value: 12.5,
    ...overrides,
  }
}

function makeSilence(overrides: Partial<Silence> = {}): Silence {
  return {
    ends_at: '2026-08-27T03:00:00.000Z',
    matchers: [{ name: 'severity', value: 'warning' }],
    reason: 'maintenance window',
    silence_id: '00000000-0000-4000-8000-000000000003',
    starts_at: NOW,
    ...overrides,
  }
}

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

async function renderSilences(silences: Silence[] = [], alerts: Alert[] = []) {
  server.use(ok('getSilences', silences), ok('getAlerts', alerts))
  const view = renderWithProviders(<SilencesPage />, { route: '/alerts/silences' })
  await settle()
  return view
}

function openEditor() {
  fireEvent.click(screen.getByRole('button', { name: 'Create silence' }))
  screen.getByRole('heading', { name: 'Create silence' })
}

function fillMatcherAndReason(reason = 'planned maintenance') {
  fireEvent.change(screen.getByLabelText('Matcher 1 name'), { target: { value: 'severity' } })
  fireEvent.change(screen.getByLabelText('Matcher 1 value'), { target: { value: 'warning' } })
  fireEvent.change(screen.getByLabelText('Reason'), { target: { value: reason } })
}

describe('SilencesPage', () => {
  it('UI-ALERT-030 previews exactly which firing alerts match the draft', async () => {
    const matching = makeAlert({ alert_key: 'warning-alert', severity: 'warning' })
    const other = makeAlert({ alert_key: 'critical-alert', severity: 'critical' })
    await renderSilences([], [matching, other])

    openEditor()
    fireEvent.change(screen.getByLabelText('Matcher 1 name'), { target: { value: 'severity' } })
    fireEvent.change(screen.getByLabelText('Matcher 1 value'), { target: { value: 'warning' } })

    expect(
      screen.getByText('Would suppress notifications for 1 currently firing alert(s):'),
    ).toBeInTheDocument()
    expect(screen.getByText('warning-alert')).toBeInTheDocument()
    expect(screen.queryByText('critical-alert')).not.toBeInTheDocument()
  })

  it('UI-ALERT-031 requires explicit confirmation when every firing alert matches', async () => {
    const alerts = [
      makeAlert({ alert_key: 'first-alert' }),
      makeAlert({ alert_key: 'second-alert' }),
    ]
    await renderSilences([], alerts)

    openEditor()
    fillMatcherAndReason()
    expect(
      screen.getByText(
        'This silence would suppress notifications for all 2 currently firing alerts. Confirm to continue.',
      ),
    ).toBeInTheDocument()
    expect(
      screen.getByLabelText(
        'I understand this will suppress notifications for all 2 currently firing alerts',
      ),
    ).not.toBeChecked()

    fireEvent.click(screen.getByRole('button', { name: 'Create silence' }))
    expect(
      screen.getByText('Confirm that this silence will suppress all 2 currently firing alerts.'),
    ).toBeInTheDocument()
  })

  it('UI-ALERT-032 rejects a missing reason without sending the request', async () => {
    let requestCount = 0
    server.use(
      ok('getSilences', []),
      ok('getAlerts', []),
      http.post('*/api/v1/silences', () => {
        requestCount += 1
        return HttpResponse.json({})
      }),
    )
    renderWithProviders(<SilencesPage />, { route: '/alerts/silences' })
    await settle()

    openEditor()
    fireEvent.change(screen.getByLabelText('Matcher 1 name'), { target: { value: 'severity' } })
    fireEvent.change(screen.getByLabelText('Matcher 1 value'), { target: { value: 'warning' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create silence' }))

    expect(screen.getByText('Reason is required.')).toBeInTheDocument()
    expect(requestCount).toBe(0)
  })

  it('UI-ALERT-033 rejects an end time before the start time', async () => {
    server.use(ok('getSilences', []), ok('getAlerts', []))
    renderWithProviders(<SilencesPage />, { route: '/alerts/silences' })
    await settle()

    openEditor()
    fillMatcherAndReason()
    fireEvent.change(screen.getByLabelText('Start'), { target: { value: '2026-08-27T04:00' } })
    fireEvent.change(screen.getByLabelText('End'), { target: { value: '2026-08-27T03:00' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create silence' }))

    expect(screen.getByText('End must be after start.')).toBeInTheDocument()
  })

  it('UI-ALERT-034 sends the contract fields when creating a silence', async () => {
    let requestBody: unknown
    const response = makeSilence({
      ends_at: '2026-08-27T03:00:00.000Z',
      matchers: [{ name: 'severity', value: 'warning' }],
      starts_at: NOW,
    })
    server.use(
      ok('getSilences', []),
      ok('getAlerts', []),
      http.post('*/api/v1/silences', async ({ request }) => {
        requestBody = await request.json()
        assertMatchesContract('createSilence', 201, response)
        return HttpResponse.json(response, { status: 201 })
      }),
    )
    renderWithProviders(<SilencesPage />, { route: '/alerts/silences' })
    await settle()

    openEditor()
    fillMatcherAndReason('deploy window')
    fireEvent.change(screen.getByLabelText('Start'), { target: { value: '2026-08-27T02:00' } })
    fireEvent.change(screen.getByLabelText('End'), { target: { value: '2026-08-27T03:00' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create silence' }))
    await settle()

    expect(requestBody).toEqual({
      ends_at: '2026-08-27T03:00:00.000Z',
      matchers: [{ name: 'severity', value: 'warning' }],
      reason: 'deploy window',
      starts_at: NOW,
    })
    expect(screen.getByText('Silence created.')).toBeInTheDocument()
  })

  it('UI-ALERT-035 confirms deletion and refreshes the list', async () => {
    const silence = makeSilence()
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    server.use(
      sequence('getSilences', [silence], []),
      ok('getAlerts', []),
      status('deleteSilence', 204),
    )
    renderWithProviders(<SilencesPage />, { route: '/alerts/silences' })
    await settle()

    fireEvent.click(screen.getByRole('button', { name: `End silence ${silence.silence_id}` }))
    await settle()

    expect(confirmSpy).toHaveBeenCalledWith(
      `End silence ${silence.silence_id}? Notifications will no longer be suppressed.`,
    )
    expect(screen.getByText(`Ended silence ${silence.silence_id}.`)).toBeInTheDocument()
    expect(screen.getByText('No stored silences')).toBeInTheDocument()
  })

  it('UI-ALERT-036 makes clear that suppression does not resolve an alert', async () => {
    await renderSilences()
    openEditor()

    expect(
      screen.getByText(
        /A silence suppresses notifications only; the alert remains visible and continues to be evaluated/i,
      ),
    ).toBeInTheDocument()
  })

  it('UI-ALERT-037 has no accessibility violations in the editor', async () => {
    const view = await renderSilences([], [makeAlert()])
    openEditor()

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
    expect(view.container).toBeTruthy()
  })
})
