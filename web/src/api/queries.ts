import {
  keepPreviousData,
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryOptions,
  type UseQueryResult,
} from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

import { client, type ApiFailure, toApiFailure } from './client'
import type { AlertRule } from './alerts'
import type { components, paths } from './generated'
import { qk } from './keys'
import { REFRESH, type RefreshPolicy } from './policy'
import { useRefreshTiming } from '@/hooks/usePreferences'
import { resolveQueryWindow, type QueryWindow } from '@/lib/timerange'

export { useCommand } from './commands'

type GetPath = keyof paths
type GetOperation<Path extends GetPath> = NonNullable<paths[Path]['get']>
type OperationParameters<Path extends GetPath> = GetOperation<Path>['parameters']
type QueryParameters<Path extends GetPath> = NonNullable<OperationParameters<Path>['query']>
// The window comes from the caller as a QueryWindow and is resolved at request time.
type WindowlessParameters<Path extends GetPath> = Omit<QueryParameters<Path>, 'from' | 'to'>

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

async function getApiResponse<T>(
  run: () => Promise<ClientResponse<T>>,
): Promise<{ data: T; response: Response }> {
  try {
    const { data, error, response } = await run()
    if (error !== undefined) {
      throw new ApiQueryError(toApiFailure(response, error))
    }
    if (data === undefined) {
      throw new ApiQueryError({ kind: 'malformed', message: 'API response did not contain data' })
    }
    return { data, response }
  } catch (error) {
    if (error instanceof ApiQueryError) {
      throw error
    }
    throw new ApiQueryError(toApiFailure(error, null))
  }
}

async function getApi<T>(run: () => Promise<ClientResponse<T>>): Promise<T> {
  return (await getApiResponse(run)).data
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
  queryFn: (signal: AbortSignal) => Promise<T>
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
  // The `refreshInterval` preference may replace the policy interval (R-11.0 P1).
  const { interval } = useRefreshTiming(policy)
  // `off` stopped a polling surface: no refetch on focus or on return to the tab either.
  const stoppedByPreference = policy.interval > 0 && interval === 0
  const queryClient = useQueryClient()
  const query = useQuery<T, ApiFailure>({
    enabled,
    // Forwarding react-query's own AbortSignal is what makes an abandoned
    // page actually stop costing the server. Every read here polls on an
    // interval, and the expensive ones (ASH over a wide range, statements,
    // metrics/query) hold one of the server's bounded pool connections for
    // the whole query: without the signal, navigating away or unmounting
    // left those requests running to completion with nobody to receive
    // them. net/http cancels the request context when the client
    // disconnects, so the connection goes back to the pool immediately.
    queryFn: ({ signal }) => queryFn(signal),
    queryKey,
    refetchInterval: paused || interval === 0 ? false : interval,
    ...(stoppedByPreference ? { refetchOnWindowFocus: false } : {}),
    retry: shouldRetry,
    retryDelay: 0,
    ...(keepPrevious ? { placeholderData: keepPreviousData } : {}),
  })
  const wasPaused = useRef(paused)
  const { refetch } = query

  useEffect(() => {
    if (wasPaused.current && !paused && !stoppedByPreference) {
      void refetch()
    }
    wasPaused.current = paused
  }, [paused, refetch, stoppedByPreference])

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

/**
 * The cluster list as the cache holds it, without a request of its own: the
 * shell (AppShell) polls it on every page. Undefined until the first answer.
 */
export function useCachedClusters(): Awaited<ReturnType<typeof fetchClusters>> | undefined {
  return useQuery<Awaited<ReturnType<typeof fetchClusters>>, ApiFailure>({
    enabled: false,
    queryFn: ({ signal }) => fetchClusters(signal),
    queryKey: qk.clusters(),
  }).data
}

async function fetchClusters(signal: AbortSignal) {
  return getApi(() => client.GET('/api/v1/clusters', { signal }))
}

export function useClusterTopology(id: string) {
  return useApiQuery({
    policy: REFRESH.cluster,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/clusters/{id}/topology', { params: { path: { id } }, signal }),
      ),
    queryKey: qk.clusterTopology(id),
  })
}

export function useClusterReplication(
  id: string,
  window: QueryWindow,
  options: { enabled?: boolean } = {},
) {
  return useApiQuery({
    enabled: options.enabled ?? true,
    keepPrevious: true,
    policy: REFRESH.cluster,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/clusters/{id}/replication', {
          params: { path: { id }, query: resolveQueryWindow(window) },
          signal,
        }),
      ),
    queryKey: qk.clusterReplication(id, window),
  })
}

export function useClusterSettingsDrift(id: string) {
  return useApiQuery({
    policy: REFRESH.cluster,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/clusters/{id}/settings-drift', { params: { path: { id } }, signal }),
      ),
    queryKey: qk.clusterSettingsDrift(id),
  })
}

export function useClusterSlots(id: string) {
  return useApiQuery({
    policy: REFRESH.cluster,
    queryFn: (signal) =>
      getApi(() => client.GET('/api/v1/clusters/{id}/slots', { params: { path: { id } }, signal })),
    queryKey: qk.clusterSlots(id),
  })
}

export function useInstances(): ApiQueryResult<Awaited<ReturnType<typeof fetchInstances>>> {
  return useApiQuery({
    policy: REFRESH.fleet,
    queryFn: fetchInstances,
    queryKey: qk.instances(),
  })
}

async function fetchInstances(signal: AbortSignal) {
  return getApi(() => client.GET('/api/v1/instances', { signal }))
}

export function useInstance(id: string) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: (signal) =>
      getApi(() => client.GET('/api/v1/instances/{id}', { params: { path: { id } }, signal })),
    queryKey: qk.instance(id),
  })
}

export function useInstanceActivity(id: string) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/activity', { params: { path: { id } }, signal }),
      ),
    queryKey: qk.instanceActivity(id),
  })
}

export function useInstanceDatabases(id: string, policy: RefreshPolicy = REFRESH.instance) {
  return useApiQuery({
    policy,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/databases', { params: { path: { id } }, signal }),
      ),
    queryKey: qk.instanceDatabases(id),
  })
}

export function useInstanceHost(id: string, policy: RefreshPolicy = REFRESH.instance) {
  return useApiQuery({
    policy,
    queryFn: (signal) =>
      getApi(() => client.GET('/api/v1/instances/{id}/host', { params: { path: { id } }, signal })),
    queryKey: qk.instanceHost(id),
  })
}

export function useInstanceSettings(
  id: string,
  params: QueryParameters<'/api/v1/instances/{id}/settings'> = {},
) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/settings', {
          params: { path: { id }, query: params },
          signal,
        }),
      ),
    queryKey: qk.instanceSettings(id, params.changed_since),
  })
}

export function useInstanceTriggers(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/triggers', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instanceTriggers(id),
  })
}

export function useInstanceUnindexedForeignKeys(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/unindexed-foreign-keys', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instanceUnindexedForeignKeys(id),
  })
}

export function useInstanceExtensions(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/extensions', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instanceExtensions(id),
  })
}

/**
 * `GET /api/v1/instances/{id}/coverage` (getCoverage): every check the
 * instance's agent schedules and whether it has reported since the agent
 * started. Polled like the instance page, so a check's report shows within one
 * interval.
 */
export function useCoverage(id: string) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/coverage', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.coverage(id),
  })
}

/**
 * `GET /api/v1/instances/{id}/readiness` (getReadiness): what stops the
 * instance from being monitored in full, with the fixes (14.1). The server
 * caches it for 30 s, so the instance poll interval costs nothing extra.
 */
export function useReadiness(id: string, options: { enabled?: boolean } = {}) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/readiness', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.readiness(id),
    ...options,
  })
}

/** `GET /api/v1/readiness/summary` (getReadinessSummary): the level of every instance, for the fleet cards. */
export function useReadinessSummary() {
  return useApiQuery({
    policy: REFRESH.fleet,
    queryFn: (signal) => getApi(() => client.GET('/api/v1/readiness/summary', { signal })),
    queryKey: qk.readinessSummary(),
  })
}

/** The shortest search term the server accepts, in characters once trimmed (D20). */
export const SEARCH_MIN_LENGTH = 2

/** The trimmed term, or undefined when it is too short to search (no request, no key). */
function searchTerm(q: string | undefined): string | undefined {
  const term = q?.trim() ?? ''
  return [...term].length >= SEARCH_MIN_LENGTH ? term : undefined
}

/** The parameters with q trimmed, or without q when it is too short to search. */
function withSearchTerm<T extends { q?: string }>(params: T): T {
  const { q, ...rest } = params
  const term = searchTerm(q)
  return (term === undefined ? rest : { ...rest, q: term }) as T
}

/**
 * Full search over what pglens collected for an instance (D20): statements,
 * indexes, tables and columns, each group with its total. Nothing is asked
 * while the trimmed term is shorter than the server accepts.
 */
export function useInstanceSearch(
  instanceId: string,
  params: QueryParameters<'/api/v1/instances/{id}/search'>,
) {
  const q = searchTerm(params.q)
  return useApiQuery({
    enabled: instanceId !== '' && q !== undefined,
    keepPrevious: true,
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/search', {
          params: { path: { id: instanceId }, query: { ...params, q: q ?? '' } },
          signal,
        }),
      ),
    queryKey: qk.instanceSearch(
      instanceId,
      q,
      params.kinds,
      params.database,
      params.from,
      params.to,
      params.limit,
    ),
  })
}

export function useInstanceSpatialColumns(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/spatial-columns', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instanceSpatialColumns(id),
  })
}

export function useInstanceSequences(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/sequences', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instanceSequences(id),
  })
}

export function useInstanceLogicalReplication(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/logical-replication', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instanceLogicalReplication(id),
  })
}

export function useInstanceInternals(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/internals', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instanceInternals(id),
  })
}

export function useInstanceFunctions(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/functions', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instanceFunctions(id),
  })
}

export function useInstanceSecurityDefinerFunctions(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/security-definer-functions', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instanceSecurityDefinerFunctions(id),
  })
}

export function useInstancePartitionedTables(id: string) {
  return useApiQuery({
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/partitioned-tables', {
          params: { path: { id } },
          signal,
        }),
      ),
    queryKey: qk.instancePartitionedTables(id),
  })
}

export function useInstanceTables(
  id: string,
  params: QueryParameters<'/api/v1/instances/{id}/tables'> = {},
) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/tables', {
          params: { path: { id }, query: params },
          signal,
        }),
      ),
    queryKey: qk.instanceTables(id, params.limit, params.order_by),
  })
}

export function useInstanceTableHistory(
  id: string,
  params: WindowlessParameters<'/api/v1/instances/{id}/tables/history'>,
  window: QueryWindow,
) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.instance,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/tables/history', {
          params: { path: { id }, query: { ...params, ...resolveQueryWindow(window) } },
          signal,
        }),
      ),
    queryKey: qk.instanceTableHistory(id, params.database, window, params.limit),
  })
}

/** Whitelisted series of the tracked statements, tables or indexes (one source per call). */
export function useWorkloadSeries(
  id: string,
  params: WindowlessParameters<'/api/v1/instances/{id}/workload/series'>,
  window: QueryWindow,
) {
  return useApiQuery({
    keepPrevious: true,
    // The sources are scraped every 60 s (statements) or 5 min (tables, indexes).
    policy: REFRESH.statements,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/workload/series', {
          params: { path: { id }, query: { ...params, ...resolveQueryWindow(window) } },
          signal,
        }),
      ),
    queryKey: qk.workloadSeries(
      id,
      params.source,
      params.series,
      window,
      params.step,
      params.database,
    ),
  })
}

export function useIndexSuggestions(
  id: string,
  params: QueryParameters<'/api/v1/instances/{id}/index-suggestions'> = {},
) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.catalog,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/index-suggestions', {
          params: { path: { id }, query: params },
          signal,
        }),
      ),
    queryKey: qk.indexSuggestions(id, params.window, params.database, params.limit),
  })
}

export function useInstanceIndexes(
  id: string,
  params: QueryParameters<'/api/v1/instances/{id}/indexes'> = {},
) {
  return useApiQuery({
    policy: REFRESH.instance,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/indexes', {
          params: { path: { id }, query: params },
          signal,
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
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/bloat', {
          params: { path: { id }, query: params },
          signal,
        }),
      ),
    queryKey: qk.instanceBloat(id, params.limit, params.object_kind),
  })
}

export function useInstanceCommandAudit(id: string, policy: RefreshPolicy = REFRESH.instance) {
  return useApiQuery({
    policy,
    // The server writes the queryid in the args of an audit entry as a
    // string, so JSON parsing keeps all of its digits.
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/instances/{id}/command-audit', { params: { path: { id } }, signal }),
      ),
    queryKey: qk.instanceCommandAudit(id),
  })
}

export function useLocks(instanceId: string) {
  return useApiQuery({
    policy: REFRESH.locks,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/locks', { params: { query: { instance_id: instanceId } }, signal }),
      ),
    queryKey: qk.locks(instanceId),
  })
}

type QueryMetricsParameters = WindowlessParameters<'/api/v1/metrics/query'>

function queryMetricsRequest(params: QueryMetricsParameters, window: QueryWindow) {
  return {
    queryFn: (signal: AbortSignal) =>
      getApi(() =>
        client.GET('/api/v1/metrics/query', {
          params: { query: { ...params, ...resolveQueryWindow(window) } },
          signal,
        }),
      ),
    queryKey: qk.queryMetrics(
      params.metric,
      params.instance_id,
      window,
      params.step,
      params.database,
    ),
  }
}

export function useQueryMetrics(params: QueryMetricsParameters, window: QueryWindow) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.activity,
    ...queryMetricsRequest(params, window),
  })
}

type QueryMetricsData = Awaited<ReturnType<ReturnType<typeof queryMetricsRequest>['queryFn']>>

export type QueryMetricsResult = UseQueryResult<QueryMetricsData, ApiFailure>

/**
 * `useQueryMetrics` for a list of metrics whose length is fixed by the
 * caller (one chart's lines). Same cache keys, polling and retries, so a
 * metric already read elsewhere on the page is fetched once.
 */
export function useQueryMetricsList(
  paramsList: readonly QueryMetricsParameters[],
  window: QueryWindow,
): QueryMetricsResult[] {
  const paused = useAutoRefreshPaused()
  const queries = useQueries({
    queries: paramsList.map((params): UseQueryOptions<QueryMetricsData, ApiFailure> => {
      const { queryFn, queryKey } = queryMetricsRequest(params, window)
      return {
        placeholderData: keepPreviousData,
        queryFn: ({ signal }) => queryFn(signal),
        queryKey,
        refetchInterval: paused ? false : REFRESH.activity.interval,
        retry: shouldRetry,
        retryDelay: 0,
      }
    }),
  })
  const wasPaused = useRef(paused)

  // `queries` is a new array on every render; the ref keeps this to one
  // refetch per return from a hidden tab.
  useEffect(() => {
    if (wasPaused.current && !paused) {
      queries.forEach((query) => void query.refetch())
    }
    wasPaused.current = paused
  }, [paused, queries])

  return queries
}

export function useEvents(params: WindowlessParameters<'/api/v1/events'>, window: QueryWindow) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.activity,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/events', {
          params: { query: { ...params, ...resolveQueryWindow(window) } },
          signal,
        }),
      ),
    queryKey: qk.events(params.cluster_id, params.type, window, params.limit),
  })
}

export function useStatements(
  rawParams: WindowlessParameters<'/api/v1/statements'>,
  window: QueryWindow,
) {
  const params = withSearchTerm(rawParams)
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.statements,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/statements', {
          params: { query: { ...params, ...resolveQueryWindow(window) } },
          signal,
        }),
      ),
    queryKey: qk.statements(
      params.instance_id,
      params.database,
      window,
      params.order_by,
      params.limit,
      params.q,
    ),
  })
}

export function useAsh(params: WindowlessParameters<'/api/v1/ash'>, window: QueryWindow) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.ash,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/ash', {
          params: { query: { ...params, ...resolveQueryWindow(window) } },
          signal,
        }),
      ),
    queryKey: qk.ash(
      params.instance_id,
      window,
      params.group_by,
      params.database,
      params.limit,
      params.wait_event_type,
      params.wait_event,
    ),
  })
}

export function useAshTop(params: WindowlessParameters<'/api/v1/ash/top'>, window: QueryWindow) {
  return useApiQuery({
    keepPrevious: true,
    policy: REFRESH.ash,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/ash/top', {
          params: { query: { ...params, ...resolveQueryWindow(window) } },
          signal,
        }),
      ),
    queryKey: qk.ashTop(
      params.instance_id,
      window,
      params.database,
      params.limit,
      params.wait_event_type,
      params.wait_event,
    ),
  })
}

export function usePlans(params: QueryParameters<'/api/v1/plans'>) {
  return useApiQuery({
    policy: REFRESH.statements,
    queryFn: (signal) =>
      getApi(() => client.GET('/api/v1/plans', { params: { query: params }, signal })),
    queryKey: qk.plans(params.queryid, params.instance_id, params.datname, params.limit),
  })
}

// The alert list carries its truncation flag in a header, which getApi drops.
async function fetchAlerts(params: QueryParameters<'/api/v1/alerts'>, signal: AbortSignal) {
  const { data, response } = await getApiResponse(() =>
    client.GET('/api/v1/alerts', { params: { query: params }, signal }),
  )
  return { alerts: data, truncated: response.headers.get('X-Result-Truncated') === 'true' }
}

/**
 * The alerts, and whether the server cut the list short. A cut list means any
 * count or listing built from it is a lower bound.
 */
export function useAlerts(params: QueryParameters<'/api/v1/alerts'> = {}) {
  const query = useApiQuery({
    policy: REFRESH.alerts,
    queryFn: (signal) => fetchAlerts(params, signal),
    queryKey: qk.alerts(
      params.state,
      params.severity,
      params.rule_id,
      params.instance_id,
      params.cluster_id,
    ),
  })
  return {
    ...query,
    data: query.data?.alerts,
    truncated: query.data?.truncated ?? false,
  }
}

export function useAlert(alertKey: string) {
  return useApiQuery({
    enabled: Boolean(alertKey),
    policy: REFRESH.alerts,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/alerts/{alert_key}', {
          params: { path: { alert_key: alertKey } },
          signal,
        }),
      ),
    queryKey: qk.alert(alertKey),
  })
}

export function useAlertRules() {
  return useApiQuery<AlertRule[]>({
    policy: REFRESH.findings,
    queryFn: (signal) => getApi(() => client.GET('/api/v1/alert-rules', { signal })),
    queryKey: qk.alertRules(),
  })
}

export function useSilences(params: QueryParameters<'/api/v1/silences'> = {}) {
  return useApiQuery({
    policy: REFRESH.findings,
    queryFn: (signal) =>
      getApi(() => client.GET('/api/v1/silences', { params: { query: params }, signal })),
    queryKey: qk.silences(params.all),
  })
}

/**
 * `keepPrevious` shows the findings already loaded while a changed search
 * term is asked for (D20), so a search box above them stays in place.
 */
export function useFindings(
  rawParams: QueryParameters<'/api/v1/findings'> = {},
  { keepPrevious = false }: { keepPrevious?: boolean } = {},
) {
  const params = withSearchTerm(rawParams)
  return useApiQuery({
    keepPrevious,
    policy: REFRESH.findings,
    queryFn: (signal) =>
      getApi(() => client.GET('/api/v1/findings', { params: { query: params }, signal })),
    queryKey: qk.findings(
      params.state,
      params.severity,
      params.rule_id,
      params.datname,
      params.scope,
      params.instance_id,
      params.cluster_id,
      params.limit,
      params.q,
    ),
  })
}

export function useFinding(findingId: string) {
  return useApiQuery({
    policy: REFRESH.findings,
    queryFn: (signal) =>
      getApi(() =>
        client.GET('/api/v1/findings/{finding-id}', {
          params: { path: { 'finding-id': findingId } },
          signal,
        }),
      ),
    queryKey: qk.finding(findingId),
  })
}

export function useAdvisorRules() {
  return useApiQuery({
    policy: REFRESH.findings,
    queryFn: (signal) => getApi(() => client.GET('/api/v1/advisor/rules', { signal })),
    queryKey: qk.advisorRules(),
  })
}

export function useSession() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: (signal) => getApi(() => client.GET('/api/v1/session', { signal })),
    queryKey: qk.session(),
  })
}

export function useHealthz() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: (signal) => getApi(() => client.GET('/healthz', { signal })),
    queryKey: qk.healthz(),
  })
}

export function useReadyz() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: (signal) => getApi(() => client.GET('/readyz', { signal })),
    queryKey: qk.readyz(),
  })
}

export function useMetrics() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: (signal) => getApi(() => client.GET('/metrics', { signal })),
    queryKey: qk.metrics(),
  })
}

export interface RenameClusterVariables {
  clusterId: string
  /** The new name; null clears it and the cluster gets its default label back. */
  name: string | null
}

export interface RenameInstanceVariables {
  instanceId: string
  /** The new display name; null clears it and the instance is labelled by address and port. */
  displayName: string | null
}

type RenamedCluster = components['schemas']['Cluster']
type RenamedInstance = components['schemas']['InstanceSummary']

/** Every read that shows a cluster or instance name refetches after a rename. */
function invalidateNameQueries(queryClient: ReturnType<typeof useQueryClient>) {
  void queryClient.invalidateQueries({ queryKey: qk.clusters() })
  void queryClient.invalidateQueries({ queryKey: qk.instances() })
}

/** `PATCH /api/v1/clusters/{id}` (renameCluster). */
export function useRenameCluster(): UseMutationResult<
  RenamedCluster,
  ApiFailure,
  RenameClusterVariables
> {
  const queryClient = useQueryClient()
  return useMutation<RenamedCluster, ApiFailure, RenameClusterVariables>({
    mutationFn: ({ clusterId, name }) =>
      getApi(() =>
        client.PATCH('/api/v1/clusters/{id}', {
          params: { path: { id: clusterId } },
          body: { name },
        }),
      ),
    onSuccess: () => invalidateNameQueries(queryClient),
    retry: false,
  })
}

/** `PATCH /api/v1/instances/{id}` (renameInstance). */
export function useRenameInstance(): UseMutationResult<
  RenamedInstance,
  ApiFailure,
  RenameInstanceVariables
> {
  const queryClient = useQueryClient()
  return useMutation<RenamedInstance, ApiFailure, RenameInstanceVariables>({
    mutationFn: ({ instanceId, displayName }) =>
      getApi(() =>
        client.PATCH('/api/v1/instances/{id}', {
          params: { path: { id: instanceId } },
          body: { display_name: displayName },
        }),
      ),
    onSuccess: (_, { instanceId }) => {
      invalidateNameQueries(queryClient)
      // The instance page reads its own detail query; its header shows the name.
      void queryClient.invalidateQueries({ queryKey: qk.instance(instanceId) })
    },
    retry: false,
  })
}

type RetentionFamily = components['schemas']['RetentionFamily']
type RetentionPreview = components['schemas']['RetentionPreview']
type RetentionFamilyName = components['schemas']['RetentionFamilyName']

/** `GET /api/v1/settings/retention` (getRetention): the live retention of each metrics family. */
export function useRetention() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: (signal) => getApi(() => client.GET('/api/v1/settings/retention', { signal })),
    queryKey: qk.retention(),
  })
}

export interface RetentionPreviewVariables {
  family: RetentionFamilyName
  days: number
}

/**
 * `GET /api/v1/settings/retention/preview` (previewRetention), on demand: what
 * a retention of `days` would delete. Nothing is changed on the server.
 */
export function useRetentionPreview(): UseMutationResult<
  RetentionPreview,
  ApiFailure,
  RetentionPreviewVariables
> {
  return useMutation<RetentionPreview, ApiFailure, RetentionPreviewVariables>({
    mutationFn: ({ family, days }) =>
      getApi(() =>
        client.GET('/api/v1/settings/retention/preview', {
          params: { query: { family, days } },
        }),
      ),
    retry: false,
  })
}

export interface SetRetentionVariables {
  family: RetentionFamilyName
  days: number
  /** Sent only after the destructive confirmation of a shortening. */
  confirm?: true
}

/** `PUT /api/v1/settings/retention/{family}` (setRetention). */
export function useSetRetention(): UseMutationResult<
  RetentionFamily,
  ApiFailure,
  SetRetentionVariables
> {
  const queryClient = useQueryClient()
  return useMutation<RetentionFamily, ApiFailure, SetRetentionVariables>({
    mutationFn: ({ family, days, confirm }) =>
      getApi(() =>
        client.PUT('/api/v1/settings/retention/{family}', {
          params: { path: { family } },
          body: confirm ? { days, confirm } : { days },
        }),
      ),
    // A refused or failed write may mean the live policy moved under the
    // console (409): either way the listing is read again.
    onSettled: () => void queryClient.invalidateQueries({ queryKey: qk.retention() }),
    retry: false,
  })
}

type AdvisorRule = components['schemas']['AdvisorRule']
type AdvisorWindow = components['schemas']['AdvisorWindow']
type AdvisorWindowsUpdate = components['schemas']['AdvisorWindowsUpdate']

export interface SetAdvisorRuleOverrideVariables {
  ruleId: string
  enabled: boolean
}

/**
 * `PUT /api/v1/advisor/rules/{id}/override` (setAdvisorRuleOverride): disables
 * a rule, or enables it again. The catalogue is read again either way, so a
 * failed write shows the live state (I-11).
 */
export function useSetAdvisorRuleOverride(): UseMutationResult<
  AdvisorRule,
  ApiFailure,
  SetAdvisorRuleOverrideVariables
> {
  const queryClient = useQueryClient()
  return useMutation<AdvisorRule, ApiFailure, SetAdvisorRuleOverrideVariables>({
    mutationFn: ({ ruleId, enabled }) =>
      getApi(() =>
        client.PUT('/api/v1/advisor/rules/{id}/override', {
          params: { path: { id: ruleId } },
          body: { enabled },
        }),
      ),
    onSuccess: (rule) =>
      queryClient.setQueryData<AdvisorRule[]>(qk.advisorRules(), (rules) =>
        rules?.map((entry) => (entry.id === rule.id ? rule : entry)),
      ),
    onSettled: () => void queryClient.invalidateQueries({ queryKey: qk.advisorRules() }),
    retry: false,
  })
}

/** `GET /api/v1/settings/advisor-windows` (getAdvisorWindows): both observation windows. */
export function useAdvisorWindows() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: (signal) => getApi(() => client.GET('/api/v1/settings/advisor-windows', { signal })),
    queryKey: qk.advisorWindows(),
  })
}

/**
 * `PUT /api/v1/settings/advisor-windows` (setAdvisorWindows): seconds set a
 * window, `null` resets it to the default.
 */
export function useSetAdvisorWindows(): UseMutationResult<
  AdvisorWindow[],
  ApiFailure,
  AdvisorWindowsUpdate
> {
  const queryClient = useQueryClient()
  return useMutation<AdvisorWindow[], ApiFailure, AdvisorWindowsUpdate>({
    mutationFn: (body) => getApi(() => client.PUT('/api/v1/settings/advisor-windows', { body })),
    onSuccess: (windows) => queryClient.setQueryData(qk.advisorWindows(), windows),
    onSettled: () => void queryClient.invalidateQueries({ queryKey: qk.advisorWindows() }),
    retry: false,
  })
}

type NotificationChannelKind = components['schemas']['NotificationChannelKind']
type NotificationTestResult = components['schemas']['NotificationTestResult']

/**
 * `GET /api/v1/notification-channels` (getNotificationChannels): whether the
 * server environment configures each channel, and the host of its address.
 */
export function useNotificationChannels() {
  return useApiQuery({
    policy: REFRESH.static,
    queryFn: (signal) => getApi(() => client.GET('/api/v1/notification-channels', { signal })),
    queryKey: qk.notificationChannels(),
  })
}

/**
 * `POST /api/v1/notification-channels/{kind}/test` (testNotificationChannel):
 * sends one test message. Never retried: each call posts a message.
 */
export function useTestNotificationChannel(): UseMutationResult<
  NotificationTestResult,
  ApiFailure,
  NotificationChannelKind
> {
  return useMutation<NotificationTestResult, ApiFailure, NotificationChannelKind>({
    mutationFn: (kind) =>
      getApi(() =>
        client.POST('/api/v1/notification-channels/{kind}/test', {
          params: { path: { kind } },
        }),
      ),
    retry: false,
  })
}

type NotificationChannelRow = components['schemas']['NotificationChannel']

/** Puts a channel's new status row into the cached listing. */
function replaceNotificationChannel(
  rows: NotificationChannelRow[] | undefined,
  row: NotificationChannelRow,
): NotificationChannelRow[] | undefined {
  return rows?.map((current) => (current.kind === row.kind ? row : current))
}

/**
 * `PUT /api/v1/notification-channels/{kind}` (setNotificationChannel): stores
 * a webhook address from the console. The address is write-only: the answer
 * is the channel's status row. The mutation's variables hold the address, so
 * the caller calls `reset()` once it settles: that detaches the observer, and
 * gcTime 0 then drops the mutation from the mutation cache at once (with an
 * observer attached, gcTime alone does not drop it).
 */
export function useSetNotificationChannel(): UseMutationResult<
  NotificationChannelRow,
  ApiFailure,
  { kind: NotificationChannelKind; url: string }
> {
  const queryClient = useQueryClient()
  return useMutation<
    NotificationChannelRow,
    ApiFailure,
    { kind: NotificationChannelKind; url: string }
  >({
    mutationFn: ({ kind, url }) =>
      getApi(() =>
        client.PUT('/api/v1/notification-channels/{kind}', {
          params: { path: { kind } },
          body: { url },
        }),
      ),
    onSuccess: (row) =>
      queryClient.setQueryData<NotificationChannelRow[]>(qk.notificationChannels(), (rows) =>
        replaceNotificationChannel(rows, row),
      ),
    onSettled: () => void queryClient.invalidateQueries({ queryKey: qk.notificationChannels() }),
    gcTime: 0,
    retry: false,
  })
}

/**
 * `DELETE /api/v1/notification-channels/{kind}` (deleteNotificationChannel):
 * removes the address stored from the console; the channel goes back to the
 * server environment.
 */
export function useDeleteNotificationChannel(): UseMutationResult<
  NotificationChannelRow,
  ApiFailure,
  NotificationChannelKind
> {
  const queryClient = useQueryClient()
  return useMutation<NotificationChannelRow, ApiFailure, NotificationChannelKind>({
    mutationFn: (kind) =>
      getApi(() =>
        client.DELETE('/api/v1/notification-channels/{kind}', {
          params: { path: { kind } },
        }),
      ),
    onSuccess: (row) =>
      queryClient.setQueryData<NotificationChannelRow[]>(qk.notificationChannels(), (rows) =>
        replaceNotificationChannel(rows, row),
      ),
    onSettled: () => void queryClient.invalidateQueries({ queryKey: qk.notificationChannels() }),
    retry: false,
  })
}
