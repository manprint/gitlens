import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/api/types'
import { qk } from '@/api/keys'
import { expectNoA11yViolations } from '@/test/a11y'
import { base as alertBase } from '@/test/fixtures/getAlert'
import { CLUSTER, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { AGENT_STALE_AFTER_SECONDS } from '@/lib/fleet'
import { REFRESH } from '@/api/policy'
import { renderWithProviders } from '@/test/render'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'

import { FleetPage } from './FleetPage'

const baseCluster = CLUSTER as unknown as Cluster

function makeCluster(overrides: Partial<Cluster>): Cluster {
  return { ...baseCluster, ...overrides }
}

async function settleInitialQuery() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

function renderFleet(clusters: Cluster[], alerts: unknown[] = [], route = '/') {
  server.use(ok('getClusters', clusters), ok('getAlerts', alerts))
  return renderWithProviders(<FleetPage />, { route })
}

describe('FleetPage', () => {
  it('UI-FLEET-010 renders one card per cluster in ranked order', async () => {
    const clusters = [
      makeCluster({ cluster_id: '9007199254740994', name: 'zulu', health: 'ok' }),
      makeCluster({ cluster_id: '9007199254740995', name: 'alpha', health: 'critical' }),
      makeCluster({ cluster_id: '9007199254740996', name: 'bravo', health: 'degraded' }),
    ]
    renderFleet(clusters)
    await settleInitialQuery()

    const cards = within(screen.getByRole('list', { name: 'Cluster cards' })).getAllByRole(
      'listitem',
    )
    expect(cards).toHaveLength(3)
    expect(cards.map((card) => within(card).getByRole('link').getAttribute('aria-label'))).toEqual([
      'alpha (9007199254740995)',
      'bravo (9007199254740996)',
      'zulu (9007199254740994)',
    ])
  })

  it('UI-FLEET-011 renders cluster_id as an exact string, not a number', async () => {
    renderFleet([CLUSTER as unknown as Cluster])
    await settleInitialQuery()

    expect(screen.getByText('9007199254740993')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /production \(9007199254740993\)/ })).toHaveAttribute(
      'href',
      '/clusters/9007199254740993',
    )
  })

  it('UI-FLEET-012 renders Unknown for null max replay lag', async () => {
    renderFleet([CLUSTER as unknown as Cluster])
    await settleInitialQuery()

    expect(screen.getByLabelText('not measured')).toBeInTheDocument()
  })

  it('UI-FLEET-013 renders a measured lag with two decimals', async () => {
    renderFleet([makeCluster({ max_replay_lag_seconds: 0.12 })])
    await settleInitialQuery()

    expect(screen.getByText('0.12 s')).toBeInTheDocument()
  })

  it('UI-FLEET-014 shows identity guidance when the source falls back to cluster_name', async () => {
    renderFleet([makeCluster({ id_source: 'cluster_name' })])
    await settleInitialQuery()

    expect(screen.getByText('cluster_name')).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: /monitoring role grant instructions/i }),
    ).toHaveAttribute('href', '/README.md#setting-up-the-monitoring-role')
  })

  it('UI-FLEET-015 exposes the health explanation through the badge description', async () => {
    renderFleet([makeCluster({ health: 'degraded', max_replay_lag_seconds: 12.5 })])
    await settleInitialQuery()

    expect(
      screen.getByRole('status', { name: /Health: degraded.*standby replay lag.*12\.5 seconds/i }),
    ).toBeInTheDocument()
  })

  it('UI-FLEET-016 writes q and health to the URL and narrows the grid', async () => {
    const view = renderFleet(
      [
        makeCluster({ cluster_id: '9007199254740997', name: 'production', health: 'ok' }),
        makeCluster({ cluster_id: '9007199254740998', name: 'staging', health: 'degraded' }),
      ],
      [],
      '/?health=unsupported',
    )
    await settleInitialQuery()

    fireEvent.change(screen.getByLabelText('Filter clusters'), { target: { value: 'staging' } })
    expect(view.router.state.location.search).toBe('?health=unsupported&q=staging')
    fireEvent.click(screen.getByRole('button', { name: /Filter degraded clusters/ }))
    expect(view.router.state.location.search).toBe('?health=degraded&q=staging')
    expect(screen.getByText('staging')).toBeInTheDocument()
    expect(screen.queryByText('production')).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Filter clusters'), { target: { value: 'missing' } })
    expect(
      screen.getByRole('heading', { name: 'No clusters match these filters' }),
    ).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Filter clusters'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: /Filter all clusters/ }))
    expect(view.router.state.location.search).toBe('')
  })

  it('uses instance and firing-alert summary counts as filters', async () => {
    const downCluster = makeCluster({
      cluster_id: '9007199254740999',
      name: 'outage',
      instances: [{ ...INSTANCE_SUMMARY, up: false }],
    })
    const alert = { ...alertBase, labels: { cluster: 'outage' } }
    renderFleet([CLUSTER as unknown as Cluster, downCluster], [alert])
    await settleInitialQuery()

    expect(screen.getByRole('button', { name: /instances down \(1\)/i })).toHaveTextContent('1')
    expect(screen.getByRole('button', { name: /firing alerts \(1\)/i })).toHaveTextContent('1')
    fireEvent.click(screen.getByRole('button', { name: /instances down \(1\)/i }))
    const clusterCards = screen.getByRole('list', { name: 'Cluster cards' })
    expect(within(clusterCards).getByText('outage')).toBeInTheDocument()
    expect(within(clusterCards).queryByText('production')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /firing alerts \(1\)/i }))
    expect(within(clusterCards).getByText('outage')).toBeInTheDocument()
  })

  it('renders the loading and API error states', async () => {
    server.use(
      status('getClusters', 500, { error: 'internal_error', detail: 'unavailable' }),
      ok('getAlerts', []),
    )
    const firstView = renderWithProviders(<FleetPage />)
    await settleInitialQuery()
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load clusters')
    firstView.unmount()

    server.resetHandlers()
    server.use(
      ok('getClusters', [CLUSTER]),
      status('getAlerts', 500, { error: 'internal_error', detail: 'unavailable' }),
    )
    const view = renderWithProviders(<FleetPage />)
    await settleInitialQuery()
    expect(screen.getByRole('alert')).toHaveTextContent('Alert counts unavailable')
    view.unmount()
  })

  it('UI-FLEET-040 an empty fleet names the agent setup step', async () => {
    renderFleet([])
    await settleInitialQuery()

    expect(screen.getByRole('heading', { name: 'No monitored clusters yet' })).toBeInTheDocument()
    expect(screen.getByText(/set up the pglens agent/i)).toBeInTheDocument()
  })

  it('UI-FLEET-041 marks stale fleet data in the header', async () => {
    const clusters = [CLUSTER as unknown as Cluster]
    const view = renderFleet(clusters)
    await settleInitialQuery()

    view.queryClient.setQueryData(qk.clusters(), clusters, {
      updatedAt: Date.now() - REFRESH.fleet.staleAfter - 1_000,
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })

    expect(screen.getByRole('status', { name: /Stale data:/i })).toHaveTextContent('Stale')
  })

  it('UI-FLEET-042 redirects a 401 to login exactly once', async () => {
    server.use(
      status('getClusters', 401, { error: 'unauthorized', detail: 'login required' }),
      ok('getAlerts', []),
    )
    const view = renderWithProviders(<FleetPage />, { route: '/fleet' })
    await settleInitialQuery()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe('?next=%2Ffleet')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe('?next=%2Ffleet')
  })

  it('UI-FLEET-043 retries a 500 and renders the cluster grid', async () => {
    const clusters = [CLUSTER as unknown as Cluster]
    server.use(
      sequence(
        'getClusters',
        { status: 500, body: { error: 'internal_error', detail: 'database unavailable' } },
        clusters,
      ),
      ok('getAlerts', []),
    )
    renderWithProviders(<FleetPage />)
    await settleInitialQuery()

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load clusters')
    fireEvent.click(screen.getByRole('button', { name: 'Retry clusters' }))
    await settleInitialQuery()
    expect(screen.getByRole('list', { name: 'Cluster cards' })).toBeInTheDocument()
  })

  it('UI-FLEET-044 keeps the grid and marks alert counts unavailable', async () => {
    server.use(
      ok('getClusters', [CLUSTER as unknown as Cluster]),
      status('getAlerts', 500, { error: 'internal_error', detail: 'alerts unavailable' }),
    )
    renderWithProviders(<FleetPage />)
    await settleInitialQuery()

    expect(screen.getByRole('list', { name: 'Cluster cards' })).toBeInTheDocument()
    expect(screen.getByText('Alert counts unavailable.')).toBeInTheDocument()
    const alertFilter = screen.getByRole('button', {
      name: /Filter clusters with firing alerts \(unavailable\)/i,
    })
    expect(alertFilter).toHaveTextContent('Unavailable')
    expect(alertFilter).not.toHaveTextContent('0')
    expect(screen.getByRole('button', { name: 'Retry alerts' })).toBeInTheDocument()
  })

  it('UI-FLEET-045 polls the fleet interval', async () => {
    const first = makeCluster({ name: 'first snapshot' })
    const second = makeCluster({ name: 'second snapshot' })
    server.use(sequence('getClusters', [first], [second]), ok('getAlerts', []))
    renderWithProviders(<FleetPage />)
    await settleInitialQuery()

    expect(screen.getByText('first snapshot')).toBeInTheDocument()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.fleet.interval)
      await vi.runOnlyPendingTimersAsync()
    })
    expect(screen.getByText('second snapshot')).toBeInTheDocument()
  })

  it('UI-FLEET-020 omits the agent health strip when every agent reports', async () => {
    renderFleet([CLUSTER as unknown as Cluster])
    await settleInitialQuery()

    expect(
      screen.queryByRole('heading', { name: 'Agents requiring attention' }),
    ).not.toBeInTheDocument()
  })

  it('UI-FLEET-021 lists an instance with a firing agent_down alert', async () => {
    const alert = {
      ...alertBase,
      alert_key: `agent_down/${INSTANCE_SUMMARY.instance_id}`,
      rule_id: 'agent_down',
      instance_id: INSTANCE_SUMMARY.instance_id,
      labels: { cause: 'agent stopped reporting' },
      summary: 'Agent is down',
    }
    renderFleet([CLUSTER as unknown as Cluster], [alert])
    await settleInitialQuery()

    expect(screen.getByRole('heading', { name: 'Agents requiring attention' })).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: /Open instance postgres\.example\.test \(production\)/i }),
    ).toHaveAttribute('href', `/instances/${INSTANCE_SUMMARY.instance_id}`)
    expect(screen.getByText('cause: agent stopped reporting')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'View agent troubleshooting' })).toHaveAttribute(
      'href',
      '/README.md#troubleshooting',
    )
  })

  it('UI-FLEET-022 lists an instance older than three push intervals', async () => {
    const staleLastSeen = new Date(
      Date.now() - (AGENT_STALE_AFTER_SECONDS + 1) * 1000,
    ).toISOString()
    const staleCluster = makeCluster({
      instances: [{ ...INSTANCE_SUMMARY, last_seen: staleLastSeen }],
    })
    renderFleet([staleCluster])
    await settleInitialQuery()

    expect(screen.getByRole('heading', { name: 'Agents requiring attention' })).toBeInTheDocument()
    expect(screen.getByText('The agent has not reported recently.')).toBeInTheDocument()
  })

  it('UI-FLEET-023 marks cluster health and lag as stale for a down agent', async () => {
    const alert = {
      ...alertBase,
      alert_key: `instance_unreachable/${INSTANCE_SUMMARY.instance_id}`,
      rule_id: 'instance_unreachable',
      instance_id: INSTANCE_SUMMARY.instance_id,
      labels: { cause: 'network timeout' },
    }
    renderFleet([CLUSTER as unknown as Cluster], [alert])
    await settleInitialQuery()

    expect(screen.getAllByRole('status', { name: /Stale data:/i })).toHaveLength(2)
    expect(screen.getAllByText('Stale — 45s old (threshold 45s)')).toHaveLength(2)
  })

  it('has no serious or critical accessibility violations for a populated grid', async () => {
    vi.useRealTimers()
    const { container } = renderFleet([
      CLUSTER as unknown as Cluster,
      makeCluster({ cluster_id: '9007199254741000', name: 'degraded', health: 'degraded' }),
    ])
    await waitFor(() =>
      expect(screen.getByRole('list', { name: 'Cluster cards' })).toBeInTheDocument(),
    )

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('has no serious or critical accessibility violations for an empty fleet', async () => {
    vi.useRealTimers()
    const { container } = renderFleet([])
    await waitFor(() =>
      expect(
        screen.getByRole('heading', { name: 'No monitored clusters yet' }),
      ).toBeInTheDocument(),
    )

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('has no serious or critical accessibility violations for an API error', async () => {
    vi.useRealTimers()
    server.use(
      status('getClusters', 500, { error: 'internal_error', detail: 'unavailable' }),
      ok('getAlerts', []),
    )
    const { container } = renderWithProviders(<FleetPage />)
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('Could not load clusters'),
    )

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })

  it('has no serious or critical accessibility violations with agent health present', async () => {
    vi.useRealTimers()
    const alert = {
      ...alertBase,
      alert_key: `agent_down/${INSTANCE_SUMMARY.instance_id}`,
      rule_id: 'agent_down',
      instance_id: INSTANCE_SUMMARY.instance_id,
      labels: { cause: 'agent stopped reporting' },
    }
    const { container } = renderFleet([CLUSTER as unknown as Cluster], [alert])
    await waitFor(() =>
      expect(
        screen.getByRole('heading', { name: 'Agents requiring attention' }),
      ).toBeInTheDocument(),
    )

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
