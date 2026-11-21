import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/api/types'
import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { base as replicationBase } from '@/test/fixtures/getClusterReplication'
import { base as slotsBase } from '@/test/fixtures/getClusterSlots'
import { base as topologyBase } from '@/test/fixtures/getClusterTopology'
import { CLUSTER } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { ClusterPage } from './ClusterPage'

vi.mock('./TopologyGraph', () => ({ TopologyGraph: () => null }))

const cluster = CLUSTER as unknown as Cluster

describe('cluster i18n (plan 006 F3)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    server.use(
      ok('getClusters', [cluster]),
      ok('getClusterTopology', topologyBase),
      ok('getClusterReplication', replicationBase),
      ok('getClusterSettingsDrift', []),
      ok('getClusterSlots', slotsBase),
    )
  })

  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-031 cluster page reads in Italian when server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<ClusterPage clusterId={cluster.cluster_id} />, { route: '/' })

    expect(await screen.findByRole('tablist', { name: 'Sezioni del cluster' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Ritardo di replica' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Eventi' })).toBeInTheDocument()
    expect(screen.getByText(/Identità del cluster:/)).toBeInTheDocument()
  })
})
