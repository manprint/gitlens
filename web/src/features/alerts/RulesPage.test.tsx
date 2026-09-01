import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { http, HttpResponse } from 'msw'

import type { AlertRule } from '@/api/alerts'
import { expectNoA11yViolations } from '@/test/a11y'
import { assertMatchesContract } from '@/test/contract'
import { renderWithProviders } from '@/test/render'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'

import { RulesPage } from './RulesPage'

function makeRule(overrides: Partial<AlertRule> = {}): AlertRule {
  return {
    enabled: true,
    for_seconds: 60,
    id: 'replication-lag',
    min_tier: 'T1',
    needs: ['metric'],
    scope: 'instance',
    severity: 'warning',
    threshold: 10,
    ...overrides,
  }
}

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

async function renderRules(rules: AlertRule[]) {
  server.use(ok('getAlertRules', rules))
  const view = renderWithProviders(<RulesPage />, { route: '/alerts/rules' })
  await settle()
  return view
}

describe('RulesPage', () => {
  it('UI-ALERT-020 keeps tier 0 rules non-editable and explains why', async () => {
    await renderRules([makeRule({ id: 'staleness', min_tier: 'T0' })])

    expect(screen.getByRole('table', { name: 'Alert rules' })).toBeInTheDocument()
    expect(screen.getByText('Always on by design')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /edit staleness/i })).not.toBeInTheDocument()
  })

  it('UI-ALERT-021 opens a tier 1 rule with its current values', async () => {
    await renderRules([makeRule()])

    screen.getByRole('table', { name: 'Alert rules' })
    fireEvent.click(screen.getByRole('button', { name: 'Edit replication-lag' }))

    expect(
      screen.getByRole('heading', { name: 'Edit alert rule: replication-lag' }),
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Threshold')).toHaveValue(10)
    expect(screen.getByLabelText('Duration (seconds)')).toHaveValue(60)
    expect(screen.getByLabelText('Severity')).toHaveValue('warning')
    expect(screen.getByLabelText('Enabled')).toBeChecked()
  })

  it('UI-ALERT-022 saves changed fields with a PUT', async () => {
    const current = makeRule()
    let requestBody: unknown
    const response = {
      enabled: false,
      for_seconds: 120,
      rule_id: current.id,
      severity: 'critical',
      threshold: 25,
    }
    server.use(
      ok('getAlertRules', [current]),
      http.put('*/api/v1/alert-rules/:rule_id', async ({ request }) => {
        requestBody = await request.json()
        assertMatchesContract('updateAlertRule', 200, response)
        return HttpResponse.json(response)
      }),
    )
    renderWithProviders(<RulesPage />, { route: '/alerts/rules' })
    await settle()

    screen.getByRole('table', { name: 'Alert rules' })
    fireEvent.click(screen.getByRole('button', { name: 'Edit replication-lag' }))
    fireEvent.change(screen.getByLabelText('Threshold'), { target: { value: '25' } })
    fireEvent.change(screen.getByLabelText('Duration (seconds)'), { target: { value: '120' } })
    fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'critical' } })
    fireEvent.click(screen.getByLabelText('Enabled'))
    fireEvent.click(screen.getByRole('button', { name: 'Save rule' }))

    await settle()
    expect(screen.getByText(/Updated replication-lag/)).toBeInTheDocument()
    expect(requestBody).toEqual({
      enabled: false,
      for_seconds: 120,
      severity: 'critical',
      threshold: 25,
    })
  })

  it('UI-ALERT-023 shows validation failures without closing the editor', async () => {
    let requestCount = 0
    const rule = makeRule()
    server.use(
      ok('getAlertRules', [rule]),
      http.put('*/api/v1/alert-rules/:rule_id', () => {
        requestCount += 1
        return HttpResponse.json({})
      }),
    )
    renderWithProviders(<RulesPage />, { route: '/alerts/rules' })
    await settle()

    screen.getByRole('table', { name: 'Alert rules' })
    fireEvent.click(screen.getByRole('button', { name: 'Edit replication-lag' }))
    fireEvent.change(screen.getByLabelText('Threshold'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Duration (seconds)'), { target: { value: '-1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save rule' }))

    expect(screen.getByText('Threshold must be a finite number.')).toBeInTheDocument()
    expect(
      screen.getByText('Duration must be an integer from 0 to 2147483647 seconds.'),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('heading', { name: 'Edit alert rule: replication-lag' }),
    ).toBeInTheDocument()
    expect(requestCount).toBe(0)
  })

  it('UI-ALERT-024 keeps entered values and shows server detail on failure', async () => {
    const rule = makeRule()
    server.use(
      ok('getAlertRules', [rule]),
      status('updateAlertRule', 503, {
        detail: 'storage unavailable',
        error: 'rule update failed',
      }),
    )
    renderWithProviders(<RulesPage />, { route: '/alerts/rules' })
    await settle()

    screen.getByRole('table', { name: 'Alert rules' })
    fireEvent.click(screen.getByRole('button', { name: 'Edit replication-lag' }))
    fireEvent.change(screen.getByLabelText('Threshold'), { target: { value: '25' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save rule' }))

    await settle()
    expect(screen.getByText('rule update failed: storage unavailable')).toBeInTheDocument()
    expect(screen.getByLabelText('Threshold')).toHaveValue(25)
    expect(screen.getByLabelText('Duration (seconds)')).toHaveValue(60)
    expect(
      screen.getByRole('heading', { name: 'Edit alert rule: replication-lag' }),
    ).toBeInTheDocument()
  })

  it('UI-ALERT-025 invalidates the rules query after a successful save', async () => {
    const current = makeRule()
    const updated = makeRule({ threshold: 42 })
    server.use(
      sequence('getAlertRules', [current], [updated]),
      ok('updateAlertRule', {
        enabled: true,
        for_seconds: 60,
        rule_id: current.id,
        severity: 'warning',
        threshold: 42,
      }),
    )
    renderWithProviders(<RulesPage />, { route: '/alerts/rules' })
    await settle()

    screen.getByRole('table', { name: 'Alert rules' })
    fireEvent.click(screen.getByRole('button', { name: 'Edit replication-lag' }))
    fireEvent.change(screen.getByLabelText('Threshold'), { target: { value: '42' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save rule' }))

    await settle()
    expect(screen.getByText(/Updated replication-lag/)).toBeInTheDocument()
    await settle()
    expect(screen.getByRole('cell', { name: '42' })).toBeInTheDocument()
  })

  it('UI-ALERT-026 has no accessibility violations in the editor', async () => {
    const view = await renderRules([makeRule()])

    screen.getByRole('table', { name: 'Alert rules' })
    fireEvent.click(screen.getByRole('button', { name: 'Edit replication-lag' }))
    screen.getByRole('heading', { name: 'Edit alert rule: replication-lag' })

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
    expect(view.container).toBeTruthy()
  })
})
