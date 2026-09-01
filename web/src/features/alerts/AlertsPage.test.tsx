import { act, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { AlertsPage } from './AlertsPage'

type Alert = Schemas['Alert']
type Silence = Schemas['Silence']

const instant = '2030-01-01T00:00:00Z'

function alert(overrides: Partial<Alert> = {}): Alert {
  return {
    alert_key: 'replication-lag',
    rule_id: 'replication-lag',
    severity: 'warning',
    state: 'firing',
    cluster_id: '9007199254740993',
    instance_id: '00000000-0000-4000-8000-000000000010',
    datname: 'app',
    labels: { team: 'database' },
    value: 12,
    summary: 'Replication lag is above the threshold',
    started_at: instant,
    last_eval_at: instant,
    resolved_at: null,
    suppressed: false,
    ...overrides,
  }
}

function silence(overrides: Partial<Silence> = {}): Silence {
  return {
    silence_id: '00000000-0000-4000-8000-000000000001',
    matchers: [{ name: 'rule_id', value: 'replication-lag' }],
    reason: 'maintenance window',
    starts_at: '2020-01-01T00:00:00Z',
    ends_at: '2090-01-01T00:00:00Z',
    ...overrides,
  }
}

async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

async function renderAlerts(
  alerts: Alert[] = [alert()],
  silences: Silence[] = [],
  route = '/alerts',
) {
  server.use(ok('getAlerts', alerts), ok('getSilences', silences))
  const view = renderWithProviders(<AlertsPage />, { route })
  await settle()
  return view
}

describe('AlertsPage', () => {
  afterEach(() => server.resetHandlers())

  it('UI-ALERT-010 the summary shows suppressed separately from firing', async () => {
    await renderAlerts([
      alert({ severity: 'critical' }),
      alert({ alert_key: 'suppressed', suppressed: true }),
      alert({ alert_key: 'resolved', state: 'resolved', severity: 'info' }),
    ])

    expect(screen.getByText('Critical firing: 1')).toBeInTheDocument()
    expect(screen.getByText('Suppressed: 1')).toBeInTheDocument()
    expect(screen.getByText('Resolved: 1')).toBeInTheDocument()
  })

  it('UI-ALERT-011 a suppressed alert is listed with its silence and remaining time', async () => {
    await renderAlerts([alert({ suppressed: true })], [silence()])

    expect(screen.getByText('maintenance window')).toBeInTheDocument()
    expect(screen.getByText(/remaining/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'maintenance window' })).toHaveAttribute(
      'href',
      '/alerts?silence_id=00000000-0000-4000-8000-000000000001',
    )
  })

  it('UI-ALERT-012 filters round-trip through the URL', async () => {
    const view = await renderAlerts([alert()], [], '/alerts')

    fireEvent.change(screen.getByLabelText('State'), { target: { value: 'resolved' } })
    fireEvent.change(screen.getByLabelText('Severity'), { target: { value: 'critical' } })
    fireEvent.change(screen.getByLabelText('Cluster'), {
      target: { value: '9007199254740993' },
    })
    fireEvent.change(screen.getByLabelText('Suppression'), {
      target: { value: 'suppressed' },
    })
    await settle()

    expect(view.router.state.location.search).toBe(
      '?state=resolved&severity=critical&cluster_id=9007199254740993&suppressed=suppressed',
    )
  })

  it('UI-ALERT-013 the cluster link uses the exact string cluster id', async () => {
    await renderAlerts([alert()])

    expect(screen.getByRole('link', { name: '9007199254740993' })).toHaveAttribute(
      'href',
      '/clusters/9007199254740993',
    )
  })

  it('UI-ALERT-014 an agent_down alert links to troubleshooting', async () => {
    await renderAlerts([alert({ rule_id: 'agent_down', alert_key: 'agent-down' })])

    expect(screen.getByRole('link', { name: 'View troubleshooting checklist' })).toHaveAttribute(
      'href',
      '/README.md#troubleshooting',
    )
  })

  it('opens selected alert detail through getAlert', async () => {
    const selected = alert({ labels: { component: 'replication' } })
    server.use(ok('getAlerts', [selected]), ok('getSilences', []), ok('getAlert', selected))
    renderWithProviders(<AlertsPage />, { route: '/alerts' })
    await settle()

    fireEvent.click(screen.getByRole('button', { name: selected.alert_key }))
    await settle()

    expect(screen.getByTestId('alert-detail')).toHaveTextContent('replication')
    expect(screen.getByText('Labels')).toBeInTheDocument()
  })

  it('has no serious or critical accessibility violations on a populated list', async () => {
    vi.useRealTimers()
    server.use(ok('getAlerts', [alert()]), ok('getSilences', []))
    const view = renderWithProviders(<AlertsPage />, { route: '/alerts' })
    await screen.findByRole('button', { name: 'replication-lag' })

    await expect(expectNoA11yViolations(view.container)).resolves.toBeUndefined()
  })
})
