import { act, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/api/types'
import { REFRESH } from '@/api/policy'
import { qk } from '@/api/keys'
import { expectNoA11yViolations } from '@/test/a11y'
import { CLUSTER, CLUSTER_ID, INSTANCE_ID } from '@/test/fixture-helpers'
import { base as topologyBase } from '@/test/fixtures/getClusterTopology'
import { base as replicationBase } from '@/test/fixtures/getClusterReplication'
import { renderWithProviders } from '@/test/render'
import { ok, sequence, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'

import { ClusterPage } from './ClusterPage'

vi.mock('./TopologyGraph', () => ({
  TopologyGraph: ({
    instances,
    topology,
  }: {
    instances: readonly unknown[]
    topology: readonly unknown[]
  }) => (
    <section aria-label="Replication topology graph" data-testid="cluster-topology-graph">
      <h2>Replication topology</h2>
      <p>
        {instances.length} instance{instances.length === 1 ? '' : 's'}, {topology.length}{' '}
        replication edge{topology.length === 1 ? '' : 's'}.
      </p>
    </section>
  ),
}))

const cluster = CLUSTER as unknown as Cluster

function configurePage() {
  server.use(
    ok('getClusters', [cluster]),
    ok('getClusterTopology', topologyBase),
    ok('getClusterReplication', replicationBase),
    ok('getClusterSettingsDrift', []),
  )
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function renderPage() {
  return renderWithProviders(<ClusterPage clusterId={CLUSTER_ID} />, {
    route: `/clusters/${CLUSTER_ID}`,
  })
}

describe('ClusterPage', () => {
  it('UI-CLUS-040 renders not found with a link back for an unknown cluster id', async () => {
    configurePage()
    server.use(status('getClusterTopology', 404))
    renderPage()
    await settle()

    expect(screen.getByRole('heading', { name: 'Cluster not found' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Back to fleet overview' })).toHaveAttribute(
      'href',
      '/',
    )
  })

  it('UI-CLUS-041 renders one node and explicit no-replication text for a standalone cluster', async () => {
    configurePage()
    renderPage()
    await settle()

    expect(screen.getByTestId('cluster-topology-graph')).toHaveTextContent(
      '1 instance, 0 replication edges',
    )
    expect(screen.getByText('No replication observed for this cluster.')).toBeInTheDocument()
    vi.useRealTimers()
    await expectNoA11yViolations(document.body)
  })

  it('UI-CLUS-042 keeps the graph and marks lag charts unavailable after a replication failure', async () => {
    configurePage()
    server.use(
      status('getClusterReplication', 500, { error: 'internal_error', detail: 'lag unavailable' }),
    )
    renderPage()
    await settle()

    expect(screen.getByTestId('cluster-topology-graph')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load replication lag charts')
    vi.useRealTimers()
    await expectNoA11yViolations(document.body)
  })

  it('UI-CLUS-043 renders Stale in the header when the last snapshot ages past the threshold', async () => {
    configurePage()
    const view = renderPage()
    await settle()

    view.queryClient.setQueryData(qk.clusterTopology(CLUSTER_ID), topologyBase, {
      updatedAt: Date.now() - REFRESH.cluster.staleAfter,
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })

    expect(screen.getByRole('status', { name: /Stale data:/i })).toBeInTheDocument()
  })

  it('UI-CLUS-044 navigates a 401 to login exactly once', async () => {
    configurePage()
    server.use(status('getClusterTopology', 401))
    const view = renderPage()
    await settle()

    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe(`?next=%2Fclusters%2F${CLUSTER_ID}`)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.cluster.interval * 2)
    })
    expect(view.router.state.location.pathname).toBe('/login')
  })

  it('UI-CLUS-045 polls at the cluster interval', async () => {
    configurePage()
    const edge = {
      confidence: 'high' as const,
      from: INSTANCE_ID,
      sync_state: 'sync',
      to: '00000000-0000-4000-8000-000000000002',
      type: 'streaming',
    }
    server.use(sequence('getClusterTopology', topologyBase, { ...topologyBase, topology: [edge] }))
    renderPage()
    await settle()

    expect(screen.getByTestId('cluster-topology-graph')).toHaveTextContent('0 replication edges')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.cluster.interval)
      await vi.runOnlyPendingTimersAsync()
    })
    await settle()
    expect(screen.getByTestId('cluster-topology-graph')).toHaveTextContent('1 replication edge')
  })
})
