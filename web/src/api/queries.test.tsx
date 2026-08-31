import { act, fireEvent, screen } from '@testing-library/react'
import { delay, http, HttpResponse } from 'msw'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { REFRESH } from './policy'
import {
  useAdvisorRules,
  useAlert,
  useAlertRules,
  useAlerts,
  useAsh,
  useAshTop,
  useClusterReplication,
  useClusterSettingsDrift,
  useClusters,
  useClusterTopology,
  useCommand,
  useEvents,
  useFinding,
  useFindings,
  useHealthz,
  useInstance,
  useInstanceActivity,
  useInstanceBloat,
  useInstanceCommandAudit,
  useInstanceDatabases,
  useInstanceHost,
  useInstanceIndexes,
  useInstanceSettings,
  useInstanceTables,
  useInstances,
  useLocks,
  useMetrics,
  usePlans,
  useQueryMetrics,
  useReadyz,
  useSession,
  useSilences,
  useStatements,
} from './queries'
import { renderWithProviders } from '@/test/render'
import { base as clusters, empty as emptyClusters } from '@/test/fixtures/getClusters'
import { base as topology } from '@/test/fixtures/getClusterTopology'
import { server } from '@/test/msw/server'
import { sequence, status } from '@/test/msw/handlers'

function ClustersProbe() {
  const query = useClusters()
  return (
    <output data-testid="clusters-probe">
      {query.data?.length ?? query.error?.kind ?? 'loading'}
    </output>
  )
}

function TopologyProbe({ id }: { id: string }) {
  const query = useClusterTopology(id)
  return (
    <output data-testid="topology-probe">
      {query.data?.cluster_id ?? query.error?.kind ?? 'loading'}
    </output>
  )
}

function TopologySwitcher() {
  const [id, setId] = useState('cluster-a')
  return (
    <>
      <TopologyProbe id={id} />
      <button type="button" onClick={() => setId('cluster-b')}>
        switch cluster
      </button>
    </>
  )
}

function AgeProbe() {
  const query = useClusters()
  const [, setTick] = useState(0)
  return (
    <>
      <output data-testid="age-probe">{query.dataAge}</output>
      <button type="button" onClick={() => setTick((tick) => tick + 1)}>
        tick clock
      </button>
    </>
  )
}

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    value: state,
  })
  document.dispatchEvent(new Event('visibilitychange'))
}

async function settleInitialQuery() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
  })
}

async function flushQueryNotification() {
  await act(async () => {
    await vi.runOnlyPendingTimersAsync()
  })
}

describe('refresh policies', () => {
  it('keeps staleAfter at least three intervals for every polling surface', () => {
    for (const policy of Object.values(REFRESH)) {
      if (policy.interval > 0 && policy.staleAfter > 0) {
        expect(policy.staleAfter).toBeGreaterThanOrEqual(policy.interval * 3)
      }
    }
  })
})

describe('query hooks', () => {
  it('UI-API-010 polls at its policy interval', async () => {
    server.use(sequence('getClusters', clusters, emptyClusters))
    renderWithProviders(<ClustersProbe />)
    await settleInitialQuery()

    expect(screen.getByTestId('clusters-probe')).toHaveTextContent('1')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.fleet.interval)
    })
    await flushQueryNotification()
    expect(screen.getByTestId('clusters-probe')).toHaveTextContent('0')
  })

  it('UI-API-011 stops polling while hidden and refetches on return', async () => {
    server.use(sequence('getClusters', clusters, emptyClusters))
    setVisibility('hidden')
    const view = renderWithProviders(<ClustersProbe />)
    await settleInitialQuery()

    expect(screen.getByTestId('clusters-probe')).toHaveTextContent('1')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.fleet.interval)
    })
    expect(screen.getByTestId('clusters-probe')).toHaveTextContent('1')

    await act(async () => {
      setVisibility('visible')
      await vi.advanceTimersByTimeAsync(0)
    })
    await flushQueryNotification()
    expect(screen.getByTestId('clusters-probe')).toHaveTextContent('0')
    view.unmount()
    setVisibility('visible')
  })

  it('UI-API-012 does not retry a 500', async () => {
    server.use(
      sequence(
        'getClusters',
        { status: 500, body: { error: 'internal_error', detail: 'database unavailable' } },
        clusters,
      ),
    )
    renderWithProviders(<ClustersProbe />)
    await settleInitialQuery()

    expect(screen.getByTestId('clusters-probe')).toHaveTextContent('server')
  })

  it('UI-API-013 retries a network error twice then surfaces it', async () => {
    let attempts = 0
    server.use(
      http.get('*/api/v1/clusters', () => {
        attempts += 1
        return HttpResponse.error()
      }),
    )
    renderWithProviders(<ClustersProbe />)
    await settleInitialQuery()
    await flushQueryNotification()

    expect(screen.getByTestId('clusters-probe')).toHaveTextContent('network')
    expect(attempts).toBe(3)
  })

  it('UI-API-014 does not keep identity-scoped data under a new cluster id', async () => {
    let calls = 0
    server.use(
      http.get('*/api/v1/clusters/:id/topology', async () => {
        calls += 1
        if (calls > 1) {
          await delay(100)
        }
        return HttpResponse.json({
          ...topology,
          cluster_id: calls === 1 ? 'cluster-a' : 'cluster-b',
        })
      }),
    )
    renderWithProviders(<TopologySwitcher />)
    await settleInitialQuery()

    expect(screen.getByTestId('topology-probe')).toHaveTextContent('cluster-a')
    fireEvent.click(screen.getByRole('button', { name: 'switch cluster' }))
    expect(screen.getByTestId('topology-probe')).not.toHaveTextContent('cluster-a')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(100)
    })
    await flushQueryNotification()
    expect(screen.getByTestId('topology-probe')).toHaveTextContent('cluster-b')
  })

  it('UI-API-015 derives dataAge from the query client timestamp', async () => {
    server.use(sequence('getClusters', clusters))
    renderWithProviders(<AgeProbe />)
    await settleInitialQuery()

    expect(screen.getByTestId('age-probe')).toHaveTextContent('0')
    await act(async () => {
      vi.advanceTimersByTime(1_234)
      await Promise.resolve()
    })
    fireEvent.click(screen.getByRole('button', { name: 'tick clock' }))
    expect(screen.getByTestId('age-probe')).toHaveTextContent('1234')
  })

  it('exercises every exported GET hook through the typed client', async () => {
    const operations = [
      'getClusters',
      'getClusterTopology',
      'getClusterReplication',
      'getClusterSettingsDrift',
      'getInstances',
      'getInstance',
      'getInstanceActivity',
      'getInstanceDatabases',
      'getInstanceHost',
      'getInstanceSettings',
      'getInstanceTables',
      'getInstanceIndexes',
      'getInstanceBloat',
      'getInstanceCommandAudit',
      'getLocks',
      'queryMetrics',
      'getEvents',
      'getStatements',
      'getAsh',
      'getAshTop',
      'getPlans',
      'getAlerts',
      'getAlert',
      'getAlertRules',
      'getSilences',
      'getFindings',
      'getFinding',
      'getAdvisorRules',
      'getCommand',
      'getSession',
      'healthz',
      'readyz',
      'getMetrics',
    ] as const
    server.use(...operations.map((operation) => status(operation, 500)))

    renderWithProviders(
      <>
        <HookProbe label="clusters" useHook={useClustersForCoverage} />
        <HookProbe label="cluster-topology" useHook={useClusterTopologyForCoverage} />
        <HookProbe label="cluster-replication" useHook={useClusterReplicationForCoverage} />
        <HookProbe label="cluster-drift" useHook={useClusterSettingsDriftForCoverage} />
        <HookProbe label="instances" useHook={useInstancesForCoverage} />
        <HookProbe label="instance" useHook={useInstanceForCoverage} />
        <HookProbe label="instance-activity" useHook={useInstanceActivityForCoverage} />
        <HookProbe label="instance-databases" useHook={useInstanceDatabasesForCoverage} />
        <HookProbe label="instance-host" useHook={useInstanceHostForCoverage} />
        <HookProbe label="instance-settings" useHook={useInstanceSettingsForCoverage} />
        <HookProbe label="instance-tables" useHook={useInstanceTablesForCoverage} />
        <HookProbe label="instance-indexes" useHook={useInstanceIndexesForCoverage} />
        <HookProbe label="instance-bloat" useHook={useInstanceBloatForCoverage} />
        <HookProbe label="instance-command-audit" useHook={useInstanceCommandAuditForCoverage} />
        <HookProbe label="locks" useHook={useLocksForCoverage} />
        <HookProbe label="query-metrics" useHook={useQueryMetricsForCoverage} />
        <HookProbe label="events" useHook={useEventsForCoverage} />
        <HookProbe label="statements" useHook={useStatementsForCoverage} />
        <HookProbe label="ash" useHook={useAshForCoverage} />
        <HookProbe label="ash-top" useHook={useAshTopForCoverage} />
        <HookProbe label="plans" useHook={usePlansForCoverage} />
        <HookProbe label="alerts" useHook={useAlertsForCoverage} />
        <HookProbe label="alert" useHook={useAlertForCoverage} />
        <HookProbe label="alert-rules" useHook={useAlertRulesForCoverage} />
        <HookProbe label="silences" useHook={useSilencesForCoverage} />
        <HookProbe label="findings" useHook={useFindingsForCoverage} />
        <HookProbe label="finding" useHook={useFindingForCoverage} />
        <HookProbe label="advisor-rules" useHook={useAdvisorRulesForCoverage} />
        <HookProbe label="command" useHook={useCommandForCoverage} />
        <HookProbe label="session" useHook={useSessionForCoverage} />
        <HookProbe label="healthz" useHook={useHealthzForCoverage} />
        <HookProbe label="readyz" useHook={useReadyzForCoverage} />
        <HookProbe label="metrics" useHook={useMetricsForCoverage} />
      </>,
    )
    await settleInitialQuery()

    expect(screen.getAllByText('error')).toHaveLength(operations.length)
  })
})

function HookProbe({ label, useHook }: { label: string; useHook: () => { error: unknown } }) {
  const query = useHook()
  return <output data-testid={`hook-${label}`}>{query.error ? 'error' : 'loading'}</output>
}

function useClustersForCoverage() {
  return useClusters()
}

function useClusterTopologyForCoverage() {
  return useClusterTopology('cluster-a')
}

function useClusterReplicationForCoverage() {
  return useClusterReplication('cluster-a', {
    from: '2026-08-27T00:00:00Z',
    to: '2026-08-27T01:00:00Z',
  })
}

function useClusterSettingsDriftForCoverage() {
  return useClusterSettingsDrift('cluster-a')
}

function useInstancesForCoverage() {
  return useInstances()
}

function useInstanceForCoverage() {
  return useInstance('instance-a')
}

function useInstanceActivityForCoverage() {
  return useInstanceActivity('instance-a')
}

function useInstanceDatabasesForCoverage() {
  return useInstanceDatabases('instance-a')
}

function useInstanceHostForCoverage() {
  return useInstanceHost('instance-a')
}

function useInstanceSettingsForCoverage() {
  return useInstanceSettings('instance-a', { changed_since: '2026-08-27T00:00:00Z' })
}

function useInstanceTablesForCoverage() {
  return useInstanceTables('instance-a')
}

function useInstanceIndexesForCoverage() {
  return useInstanceIndexes('instance-a')
}

function useInstanceBloatForCoverage() {
  return useInstanceBloat('instance-a')
}

function useInstanceCommandAuditForCoverage() {
  return useInstanceCommandAudit('instance-a')
}

function useLocksForCoverage() {
  return useLocks('instance-a')
}

function useQueryMetricsForCoverage() {
  return useQueryMetrics({
    metric: 'cpu_usage',
    instance_id: 'instance-a',
    from: '2026-08-27T00:00:00Z',
    to: '2026-08-27T01:00:00Z',
    step: '1m',
  })
}

function useEventsForCoverage() {
  return useEvents()
}

function useStatementsForCoverage() {
  return useStatements({ instance_id: 'instance-a' })
}

function useAshForCoverage() {
  return useAsh({ instance_id: 'instance-a' })
}

function useAshTopForCoverage() {
  return useAshTop({ instance_id: 'instance-a' })
}

function usePlansForCoverage() {
  return usePlans({ queryid: 42 })
}

function useAlertsForCoverage() {
  return useAlerts()
}

function useAlertForCoverage() {
  return useAlert('alert-a')
}

function useAlertRulesForCoverage() {
  return useAlertRules()
}

function useSilencesForCoverage() {
  return useSilences()
}

function useFindingsForCoverage() {
  return useFindings()
}

function useFindingForCoverage() {
  return useFinding('finding-a')
}

function useAdvisorRulesForCoverage() {
  return useAdvisorRules()
}

function useCommandForCoverage() {
  return useCommand('command-a')
}

function useSessionForCoverage() {
  return useSession()
}

function useHealthzForCoverage() {
  return useHealthz()
}

function useReadyzForCoverage() {
  return useReadyz()
}

function useMetricsForCoverage() {
  return useMetrics()
}
