import { act, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { ok, status } from '@/test/msw/handlers'
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

    expect(screen.getByLabelText('Alert summary')).toHaveTextContent(/Critical firing\s*1(?!\d)/)
    expect(screen.getByLabelText('Alert summary')).toHaveTextContent(/Suppressed\s*1(?!\d)/)
    expect(screen.getByLabelText('Alert summary')).toHaveTextContent(/Resolved\s*1(?!\d)/)
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

  it('UI-ALERT-015 a silence link narrows the list to the alerts that silence matches', async () => {
    await renderAlerts(
      [alert({ suppressed: true }), alert({ alert_key: 'disk-full', rule_id: 'disk-full' })],
      [silence()],
      `/alerts?silence_id=${silence().silence_id}`,
    )

    expect(screen.getByRole('button', { name: 'replication-lag' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'disk-full' })).not.toBeInTheDocument()
    expect(screen.getByText(/Showing alerts matched by silence/)).toBeInTheDocument()
  })

  it('UI-ALERT-016 a silence link to a silence that is not listed shows no alerts', async () => {
    await renderAlerts(
      [alert({ suppressed: true })],
      [],
      '/alerts?silence_id=00000000-0000-4000-8000-0000000000ff',
    )

    expect(screen.queryByRole('button', { name: 'replication-lag' })).not.toBeInTheDocument()
    expect(screen.getByText(/is not in the current silences list/)).toBeInTheDocument()
  })

  it('UI-ALERT-017 a silence link whose silences failed to load does not claim the silence is missing', async () => {
    server.use(
      ok('getAlerts', [alert({ suppressed: true })]),
      status('getSilences', 500, { error: 'internal_error', detail: 'silences unavailable' }),
    )
    renderWithProviders(<AlertsPage />, {
      route: '/alerts?silence_id=00000000-0000-4000-8000-0000000000ff',
    })
    await settle()

    expect(screen.queryByRole('button', { name: 'replication-lag' })).not.toBeInTheDocument()
    expect(screen.getByText(/could not be checked, so no alerts are shown/)).toBeInTheDocument()
    expect(screen.queryByText(/is not in the current silences list/)).not.toBeInTheDocument()
    expect(screen.getByText(/Suppression details are unavailable/)).toBeInTheDocument()
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

  it('UI-ALERT-040 names only Slack, generic webhook and Google Chat as delivery channels', async () => {
    // Delivery is its own tab of the alerts page (15.3).
    await renderAlerts([alert()], [], '/alerts?section=delivery')

    const channels = screen.getByRole('list', { name: 'Supported notification channels' })
    expect(channels).toHaveTextContent('Slack')
    expect(channels).toHaveTextContent('Generic webhook')
    expect(channels).toHaveTextContent('Google Chat')
    expect(within(channels).getAllByRole('heading', { level: 3 })).toHaveLength(3)
  })

  it('UI-ALERT-041 explains that unconfigured channels still persist alerts', async () => {
    // Delivery is its own tab of the alerts page (15.3).
    await renderAlerts([alert()], [], '/alerts?section=delivery')

    expect(screen.getByRole('note')).toHaveTextContent('alerts are persisted but not delivered')
  })

  it('UI-ALERT-061 says the list is a lower bound when the server cut it at the limit', async () => {
    server.use(ok('getSilences', []))
    server.use(ok('getAlerts', [alert()], { headers: { 'X-Result-Truncated': 'true' } }))
    renderWithProviders(<AlertsPage />, { route: '/alerts' })
    await settle()

    expect(
      screen.getByText('Only the 500 most recently evaluated alerts are loaded.'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/any count or list built from alerts is a lower bound/),
    ).toBeInTheDocument()
  })

  it('UI-ALERT-042 does not offer a webhook URL editor', async () => {
    // Delivery is its own tab of the alerts page (15.3).
    await renderAlerts([alert()], [], '/alerts?section=delivery')

    expect(screen.queryByRole('textbox', { name: /webhook url/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /save webhook/i })).not.toBeInTheDocument()
  })

  it('has no serious or critical accessibility violations on a populated list', async () => {
    vi.useRealTimers()
    server.use(ok('getAlerts', [alert()]), ok('getSilences', []))
    const view = renderWithProviders(<AlertsPage />, { route: '/alerts' })
    await screen.findByRole('button', { name: 'replication-lag' })

    await expect(expectNoA11yViolations(view.container)).resolves.toBeUndefined()
  })
})

describe('AlertsPage Full HD layout (D26)', () => {
  afterEach(() => server.resetHandlers())

  it('FHD-APPLY-020 shows the alert list on the default tab, without writing it to the URL', async () => {
    const view = await renderAlerts()

    expect(screen.getByRole('tablist', { name: 'Alert sections' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Alerts' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getAllByRole('table')).toHaveLength(1)
    expect(screen.getByRole('table', { name: 'Alerts' })).toBeInTheDocument()
    // Only the active tab is mounted.
    expect(
      screen.queryByRole('list', { name: 'Supported notification channels' }),
    ).not.toBeInTheDocument()
    expect(view.router.state.location.search).toBe('')
  })

  it('FHD-APPLY-021 keeps the delivery tab in ?section= and falls back on an unknown value', async () => {
    const view = await renderAlerts([alert()], [], '/alerts?section=delivery&state=all')

    expect(screen.getByRole('tab', { name: 'Notification delivery' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Alerts' }))
    await settle()
    // The default tab is never written; the filters stay.
    expect(view.router.state.location.search).toBe('?state=all')
    expect(screen.getByRole('table', { name: 'Alerts' })).toBeInTheDocument()

    view.unmount()
    await renderAlerts([alert()], [], '/alerts?section=unknown')
    expect(screen.getByRole('tab', { name: 'Alerts' })).toHaveAttribute('aria-selected', 'true')
  })

  it('FHD-APPLY-022 puts the filters in the list header and the summary beside the tab list', async () => {
    await renderAlerts()

    const list = screen.getByRole('region', { name: 'Alerts' })
    const table = screen.getByRole('table', { name: 'Alerts' })
    for (const label of ['State', 'Severity', 'Cluster', 'Suppression']) {
      const control = within(list).getByLabelText(label)
      expect(control).toHaveClass('min-[1600px]:h-8')
      expect(control.compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    }
    const summary = screen.getByLabelText('Alert summary')
    expect(summary).toHaveClass('min-[1600px]:flex')
    const row = screen.getByTestId('alerts-tab-row')
    expect(row).toContainElement(summary)
    expect(row).toContainElement(screen.getByRole('tablist', { name: 'Alert sections' }))
  })

  it('FHD-APPLY-023 lists 20 alerts per page, one line each, with the full text in a title', async () => {
    await renderAlerts(
      Array.from({ length: 25 }, (_, index) =>
        alert({ alert_key: `alert-${String(index + 1).padStart(2, '0')}` }),
      ),
    )

    const table = screen.getByRole('table', { name: 'Alerts' })
    expect(table).toHaveClass('min-[1600px]:table-fixed')
    expect(within(table).getAllByRole('row')).toHaveLength(21)
    expect(screen.getByRole('navigation', { name: 'Alerts pagination' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'alert-01' })).toHaveClass('min-[1600px]:truncate')
    expect(within(table).getAllByTitle('Replication lag is above the threshold')[0]).toHaveClass(
      'min-[1600px]:truncate',
    )

    // A filter change sends the list back to its first page (D23).
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    expect(screen.getByText('Page 2 of 2')).toBeInTheDocument()
    fireEvent.change(screen.getByRole('combobox', { name: 'State' }), {
      target: { value: 'all' },
    })
    expect(screen.getByText('Page 1 of 2')).toBeInTheDocument()
  })
})
