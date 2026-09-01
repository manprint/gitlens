import { act, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ok } from '@/test/msw/handlers'
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

function finding(overrides: Partial<FindingRecord> = {}): FindingRecord {
  return { ...baseFinding, ...overrides }
}

function rule(overrides: Partial<AdvisorRule> = {}): AdvisorRule {
  return { ...baseRule, ...overrides }
}

async function renderFindings(
  findings: FindingRecord[] = [finding()],
  rules: AdvisorRule[] = [rule()],
  route = '/findings',
) {
  server.use(ok('getFindings', findings), ok('getAdvisorRules', rules))
  const view = renderWithProviders(<FindingsPage />, { route })
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
  return view
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

    expect(screen.getByRole('status')).toHaveTextContent(/seven days of history/)
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
