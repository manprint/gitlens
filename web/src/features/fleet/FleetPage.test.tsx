import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/api/types'
import { expectNoA11yViolations } from '@/test/a11y'
import { base as alertBase } from '@/test/fixtures/getAlert'
import { CLUSTER, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'
import { ok, status } from '@/test/msw/handlers'
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
    expect(screen.getByText('outage')).toBeInTheDocument()
    expect(screen.queryByText('production')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /firing alerts \(1\)/i }))
    expect(screen.getByText('outage')).toBeInTheDocument()
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
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load alerts')
    view.unmount()
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
})
