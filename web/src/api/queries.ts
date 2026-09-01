import {
  keepPreviousData,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

import { client, type ApiFailure, toApiFailure } from './client'
import type { AlertRule } from './alerts'
import type { paths } from './generated'
import { qk } from './keys'
import { REFRESH, type RefreshPolicy } from './policy'

export { useCommand } from './commands'

type GetPath = keyof paths
type GetOperation<Path extends GetPath> = NonNullable<paths[Path]['get']>
type OperationParameters<Path extends GetPath> = GetOperation<Path>['parameters']
type QueryParameters<Path extends GetPath> = NonNullable<OperationParameters<Path>['query']>

interface ClientResponse<T> {
  data?: T
  error?: unknown
  response: Response
}

export type ApiQueryResult<T> = UseQueryResult<T, ApiFailure> & { dataAge: number }

class ApiQueryError extends Error {
  readonly kind: ApiFailure['kind']
  readonly status?: number
  readonly code?: string
  readonly detail?: string

  constructor(failure: ApiFailure) {
    super('message' in failure ? failure.message : `API request failed: ${failure.kind}`)
    this.name = 'ApiQueryError'
    this.kind = failure.kind
    Object.assign(this, failure)
  }
}

function isApiFailure(error: unknown): error is ApiFailure {
  return (
    typeof error === 'object' && error !== null && 'kind' in error && typeof error.kind === 'string'
  )
}

async function getApi<T>(run: () => Promise<ClientResponse<T>>): Promise<T> {
  try {
    const { data, error, response } = await run()
    if (error !== undefined) {
      throw new ApiQueryError(toApiFailure(response, error))
    }
    if (data === undefined) {
      throw new ApiQueryError({ kind: 'malformed', message: 'API response did not contain data' })
    }
    return data
  } catch (error) {
    if (error instanceof ApiQueryError) {
      throw error
    }
    throw new ApiQueryError(toApiFailure(error, null))
  }
}

function shouldRetry(failureCount: number, error: unknown): boolean {
  return isApiFailure(error) && error.kind === 'network' && failureCount < 2
}

/**
 * Keep polling out of hidden tabs. A visibility transition back to visible
 * performs one immediate refetch; the normal interval then resumes.
 */
export function useAutoRefreshPaused(): boolean {
  const [paused, setPaused] = useState(
    () => typeof document !== 'undefined' && document.visibilityState === 'hidden',
  )

  useEffect(() => {
    const handleVisibilityChange = () => {
      setPaused(document.visibilityState === 'hidden')
    }
    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [])

  return paused
}

interface UseApiQueryOptions<T> {
  enabled?: boolean
  keepPrevious?: boolean
  policy: RefreshPolicy
  queryFn: () => Promise<T>
  queryKey: readonly unknown[]
}

function useApiQuery<T>({
  enabled = true,
  keepPrevious = false,
  policy,
  queryFn,
  queryKey,
}: UseApiQueryOptions<T>): ApiQueryResult<T> {
  const paused = useAutoRefreshPaused()
  const queryClient = useQueryClient()
  const query = useQuery<T, ApiFailure>({
    enabled,
    queryFn,
    queryKey,
    refetchInterval: paused || policy.interval === 0 ? false : policy.interval,
    retry: shouldRetry,
    retryDelay: 0,
    ...(keepPrevious ? { placeholderData: keepPreviousData } : {}),
  })
  const wasPaused = useRef(paused)
  const { refetch } = query

  useEffect(() => {
    if (wasPaused.current && !paused) {
      void refetch()
    }
    wasPaused.current = paused
  }, [paused, refetch])

  const dataUpdatedAt = queryClient.getQueryState(queryKey)?.dataUpdatedAt ?? query.dataUpdatedAt
  // The age is intentionally evaluated from the wall clock on each render.
  // eslint-disable-next-line react-hooks/purity
  const dataAge = dataUpdatedAt > 0 ? Math.max(0, Date.now() - dataUpdatedAt) : 0
  return { ...query, dataAge }
}

export function useClusters(): ApiQueryResult<Awaited<ReturnType<typeof fetchClusters>>> {
  return useApiQuery({
    policy: REFRESH.fleet,
    queryFn: fetchClusters,
    queryKey: qk.clusters(),
  })
}

async function fetchClusters() {
  return getApi(() => client.GET('/api/v1/clusters'))
}

export function useClusterTopology(id: string) {
  return useApiQuery({
    policy: REFRESH.cluster,
    queryFn: () =>
      getApi(() => client.GET('/api/v1/clusters/{id}/topology', { params: { path: { id } } })),
    queryKey: qk.clusterTopology(id),
  })
}

export function useClusterReplication(
  id: string,
  params: QueryParameters<'/api/v1/clusters/{id}/replication'>,
) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.cluster,
    queryFn: () =>
      getApi(() =>
        client.GET('/api/v1/clusters/{id}/replication', {
          params: { path: { id }, query: params },
        }),
      ),
    queryKey: qk.clusterReplication(id, params.from, params.to),
  })
}

export function useClusterSettingsDrift(id: string) {
  return useApiQuery({
    policy: REFRESH.cluster,
    queryFn: () =>
      getApi(() =>
        client.GET('/api/v1/clusters/{id}/settings-drift', { params: { path: { id } } }),
      ),
    queryKey: qk.clusterSettingsDrift(id),
  })
}

export function useInstances(): ApiQueryResult<Awaited<ReturnType<typeof fetchInstances>>> {
  return useApiQuery({
    policy: REFRESH.fleet,
    queryFn: fetchInstances,
    queryKey: qk.instances(),
  })
}

async function fetchInstances() {
  return getApi(() => client.GET('/api/v1/instances'))
}

export function useInstance(id: string) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: () => getApi(() => client.GET('/api/v1/instances/{id}', { params: { path: { id } } })),
    queryKey: qk.instance(id),
  })
}

export function useInstanceActivity(id: string) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: () =>
      getApi(() => client.GET('/api/v1/instances/{id}/activity', { params: { path: { id } } })),
    queryKey: qk.instanceActivity(id),
  })
}

export function useInstanceDatabases(id: string) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: () =>
      getApi(() => client.GET('/api/v1/instances/{id}/databases', { params: { path: { id } } })),
    queryKey: qk.instanceDatabases(id),
  })
}

export function useInstanceHost(id: string) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: () =>
      getApi(() => client.GET('/api/v1/instances/{id}/host', { params: { path: { id } } })),
    queryKey: qk.instanceHost(id),
  })
}

export function useInstanceSettings(
  id: string,
  params: QueryParameters<'/api/v1/instances/{id}/settings'> = {},
) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: () =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/settings', {
          params: { path: { id }, query: params },
        }),
      ),
    queryKey: qk.instanceSettings(id, params.changed_since),
  })
}

export function useInstanceTables(
  id: string,
  params: QueryParameters<'/api/v1/instances/{id}/tables'> = {},
) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: () =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/tables', {
          params: { path: { id }, query: params },
        }),
      ),
    queryKey: qk.instanceTables(id, params.limit),
  })
}

export function useInstanceIndexes(
  id: string,
  params: QueryParameters<'/api/v1/instances/{id}/indexes'> = {},
) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: () =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/indexes', {
          params: { path: { id }, query: params },
        }),
      ),
    queryKey: qk.instanceIndexes(id, params.limit),
  })
}

export function useInstanceBloat(
  id: string,
  params: QueryParameters<'/api/v1/instances/{id}/bloat'> = {},
) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: () =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/bloat', {
          params: { path: { id }, query: params },
        }),
      ),
    queryKey: qk.instanceBloat(id, params.limit),
  })
}

export function useInstanceCommandAudit(id: string) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: () =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/command-audit', { params: { path: { id } } }),
      ),
    queryKey: qk.instanceCommandAudit(id),
  })
}

export function useLocks(instanceId: string) {
  return useApiQuery({
    policy: REFRESH.locks,
    queryFn: () =>
      getApi(() => client.GET('/api/v1/locks', { params: { query: { instance_id: instanceId } } })),
    queryKey: qk.locks(instanceId),
  })
}

export function useQueryMetrics(params: QueryParameters<'/api/v1/metrics/query'>) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.activity,
    queryFn: () => getApi(() => client.GET('/api/v1/metrics/query', { params: { query: params } })),
    queryKey: qk.queryMetrics(
      params.metric,
      params.instance_id,
      params.from,
      params.to,
      params.step,
      params.database,
    ),
  })
}

export function useEvents(params: QueryParameters<'/api/v1/events'> = {}) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.activity,
    queryFn: () => getApi(() => client.GET('/api/v1/events', { params: { query: params } })),
    queryKey: qk.events(params.cluster_id, params.type, params.from, params.to, params.limit),
  })
}

export function useStatements(params: QueryParameters<'/api/v1/statements'>) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.statements,
    queryFn: () => getApi(() => client.GET('/api/v1/statements', { params: { query: params } })),
    queryKey: qk.statements(
      params.instance_id,
      params.database,
      params.from,
      params.to,
      params.order_by,
      params.limit,
    ),
  })
}

export function useAsh(params: QueryParameters<'/api/v1/ash'>) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.ash,
    queryFn: () => getApi(() => client.GET('/api/v1/ash', { params: { query: params } })),
    queryKey: qk.ash(
      params.instance_id,
      params.from,
      params.to,
      params.group_by,
      params.database,
      params.limit,
    ),
  })
}

export function useAshTop(params: QueryParameters<'/api/v1/ash/top'>) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.ash,
    queryFn: () => getApi(() => client.GET('/api/v1/ash/top', { params: { query: params } })),
    queryKey: qk.ashTop(params.instance_id, params.from, params.to, params.database, params.limit),
  })
}

export function usePlans(params: QueryParameters<'/api/v1/plans'>) {
  return useApiQuery({
    policy: REFRESH.statements,
    queryFn: () => getApi(() => client.GET('/api/v1/plans', { params: { query: params } })),
    queryKey: qk.plans(params.queryid, params.instance_id, params.datname, params.limit),
  })
}

export function useAlerts(params: QueryParameters<'/api/v1/alerts'> = {}) {
  return useApiQuery({
    policy: REFRESH.alerts,
    queryFn: () => getApi(() => client.GET('/api/v1/alerts', { params: { query: params } })),
    queryKey: qk.alerts(
      params.state,
      params.severity,
      params.rule_id,
      params.instance_id,
      params.cluster_id,
    ),
  })
}

export function useAlert(alertKey: string) {
  return useApiQuery({
    enabled: Boolean(alertKey),
    policy: REFRESH.alerts,
    queryFn: () =>
      getApi(() =>
        client.GET('/api/v1/alerts/{alert_key}', { params: { path: { alert_key: alertKey } } }),
      ),
    queryKey: qk.alert(alertKey),
  })
}

export function useAlertRules() {
  return useApiQuery<AlertRule[]>({
    policy: REFRESH.findings,
    queryFn: () => getApi(() => client.GET('/api/v1/alert-rules')),
    queryKey: qk.alertRules(),
  })
}

export function useSilences(params: QueryParameters<'/api/v1/silences'> = {}) {
  return useApiQuery({
    policy: REFRESH.findings,
    queryFn: () => getApi(() => client.GET('/api/v1/silences', { params: { query: params } })),
    queryKey: qk.silences(params.all),
  })
}

export function useFindings(params: QueryParameters<'/api/v1/findings'> = {}) {
  return useApiQuery({
    policy: REFRESH.findings,
    queryFn: () => getApi(() => client.GET('/api/v1/findings', { params: { query: params } })),
    queryKey: qk.findings(
      params.state,
      params.severity,
      params.rule_id,
      params.datname,
      params.scope,
      params.instance_id,
      params.cluster_id,
      params.limit,
    ),
  })
}

export function useFinding(findingId: string) {
  return useApiQuery({
    policy: REFRESH.findings,
    queryFn: () =>
      getApi(() =>
        client.GET('/api/v1/findings/{finding-id}', {
          params: { path: { 'finding-id': findingId } },
        }),
      ),
    queryKey: qk.finding(findingId),
  })
}

export function useAdvisorRules() {
  return useApiQuery({
    policy: REFRESH.findings,
    queryFn: () => getApi(() => client.GET('/api/v1/advisor/rules')),
    queryKey: qk.advisorRules(),
  })
}

export function useSession() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: () => getApi(() => client.GET('/api/v1/session')),
    queryKey: qk.session(),
  })
}

export function useHealthz() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: () => getApi(() => client.GET('/healthz')),
    queryKey: qk.healthz(),
  })
}

export function useReadyz() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: () => getApi(() => client.GET('/readyz')),
    queryKey: qk.readyz(),
  })
}

export function useMetrics() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: () => getApi(() => client.GET('/metrics')),
    queryKey: qk.metrics(),
  })
}
