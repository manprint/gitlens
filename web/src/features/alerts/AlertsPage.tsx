import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAlert, useAlerts, useSilences } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import { matchSilence, rankAlerts, summariseAlerts, type Alert, type Silence } from '@/lib/alerts'

import { AlertRow } from './AlertRow'

type StateFilter = 'all' | Alert['state']
type SeverityFilter = 'all' | Alert['severity']
type SuppressedFilter = 'all' | 'suppressed' | 'unsuppressed'

const stateFilters: readonly StateFilter[] = ['firing', 'all', 'pending', 'resolved']
const severityFilters: readonly SeverityFilter[] = ['all', 'critical', 'warning', 'info']
const suppressionFilters: readonly SuppressedFilter[] = ['all', 'suppressed', 'unsuppressed']
const EMPTY_ALERTS: Alert[] = []
const EMPTY_SILENCES: Silence[] = []

function readFilter<T extends string>(value: string | null, allowed: readonly T[], fallback: T): T {
  return allowed.includes(value as T) ? (value as T) : fallback
}

function updateSearchParams(
  searchParams: URLSearchParams,
  setSearchParams: (next: URLSearchParams) => void,
  updates: Record<string, string>,
) {
  const next = new URLSearchParams(searchParams)
  for (const [key, value] of Object.entries(updates)) {
    if (value) next.set(key, value)
    else next.delete(key)
  }
  setSearchParams(next)
}

function matchesFilter(
  alert: Alert,
  state: StateFilter,
  severity: SeverityFilter,
  clusterId: string,
  suppressed: SuppressedFilter,
): boolean {
  if (state !== 'all' && alert.state !== state) return false
  if (severity !== 'all' && alert.severity !== severity) return false
  if (clusterId && String(alert.cluster_id ?? '') !== clusterId) return false
  if (suppressed === 'suppressed' && !alert.suppressed) return false
  if (suppressed === 'unsuppressed' && alert.suppressed) return false
  return true
}

function Detail({ alertKey }: { alertKey: string }) {
  const detailQuery = useAlert(alertKey)

  if (detailQuery.isPending) {
    return (
      <div aria-busy="true" aria-label="Loading alert detail" role="status">
        Loading alert detail…
      </div>
    )
  }
  if (detailQuery.error) {
    return (
      <ErrorState
        endpoint="alert detail"
        failure={detailQuery.error}
        onRetry={() => void detailQuery.refetch()}
      />
    )
  }
  if (!detailQuery.data) return null

  const alert = detailQuery.data
  return (
    <div className="space-y-3" data-testid="alert-detail">
      <dl className="grid gap-2 text-sm sm:grid-cols-2">
        <div>
          <dt className="font-medium">Rule</dt>
          <dd>{alert.rule_id}</dd>
        </div>
        <div>
          <dt className="font-medium">State</dt>
          <dd>{alert.state}</dd>
        </div>
        <div>
          <dt className="font-medium">Labels</dt>
          <dd>
            <code>{JSON.stringify(alert.labels)}</code>
          </dd>
        </div>
        <div>
          <dt className="font-medium">History</dt>
          <dd>
            Started {alert.started_at}; last evaluated {alert.last_eval_at}; resolved{' '}
            {alert.resolved_at ?? 'not yet'}.
          </dd>
        </div>
      </dl>
    </div>
  )
}

export function AlertsPage() {
  const alertsQuery = useAlerts({ state: 'all' })
  const silencesQuery = useSilences({ all: true })
  const location = useLocation()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [selectedAlertKey, setSelectedAlertKey] = useState<string | null>(null)
  const redirectedForUnauthorized = useRef(false)

  const state = readFilter(searchParams.get('state'), stateFilters, 'firing')
  const severity = readFilter(searchParams.get('severity'), severityFilters, 'all')
  const clusterId = searchParams.get('cluster_id') ?? ''
  const suppressed = readFilter(searchParams.get('suppressed'), suppressionFilters, 'all')
  const alerts = alertsQuery.data ?? EMPTY_ALERTS
  const silences = silencesQuery.data ?? EMPTY_SILENCES
  const summary = useMemo(() => summariseAlerts(alerts), [alerts])
  const visibleAlerts = useMemo(
    () =>
      rankAlerts(
        alerts.filter((alert) => matchesFilter(alert, state, severity, clusterId, suppressed)),
      ),
    [alerts, clusterId, severity, state, suppressed],
  )
  const activeSilences = useMemo(
    () =>
      silences.filter((silence) => {
        const starts = Date.parse(silence.starts_at)
        const ends = Date.parse(silence.ends_at)
        // eslint-disable-next-line react-hooks/purity -- the active window is a render-time snapshot.
        const now = Date.now()
        return Number.isFinite(starts) && Number.isFinite(ends) && now >= starts && now < ends
      }),
    [silences],
  )

  function updateFilter(key: string, value: string, defaultValue = '') {
    updateSearchParams(searchParams, setSearchParams, {
      [key]: value === defaultValue ? '' : value,
    })
  }

  const unauthorized =
    alertsQuery.error?.kind === 'unauthorized' || silencesQuery.error?.kind === 'unauthorized'

  useEffect(() => {
    if (!unauthorized || redirectedForUnauthorized.current) return
    redirectedForUnauthorized.current = true
    const next = `${location.pathname}${location.search}`
    void navigate(`/login?next=${encodeURIComponent(next)}`, { replace: true })
  }, [location.pathname, location.search, navigate, unauthorized])

  if (alertsQuery.error && !alertsQuery.data) {
    return (
      <ErrorState
        endpoint="alerts"
        failure={alertsQuery.error}
        onRetry={() => void alertsQuery.refetch()}
      />
    )
  }
  if (alertsQuery.isPending || silencesQuery.isPending) {
    return (
      <section aria-busy="true" aria-label="Loading alerts" role="status">
        Loading alerts…
      </section>
    )
  }

  return (
    <div className="space-y-8">
      <PageHeader
        title="Alerts and events"
        subtitle="Current and historical alert state, with suppression kept visible for on-call context."
        freshness={<FreshnessBadge dataUpdatedAt={alertsQuery.dataUpdatedAt} policy="alerts" />}
      />

      {alertsQuery.error ? (
        <ErrorState
          endpoint="alerts"
          failure={alertsQuery.error}
          onRetry={() => void alertsQuery.refetch()}
        />
      ) : null}
      {silencesQuery.error ? (
        <div className="border-warning/40 bg-warning/10 p-3 text-sm" role="alert">
          Suppression details are unavailable; alerts remain visible without silence context.
          <button
            className="ml-2 underline"
            onClick={() => void silencesQuery.refetch()}
            type="button"
          >
            Retry silences
          </button>
        </div>
      ) : null}

      <Section
        title="Summary"
        description="Firing alerts are counted by severity; suppressed alerts are shown separately."
      >
        <div aria-label="Alert summary" className="flex flex-wrap gap-4 text-sm" role="group">
          <span>Critical firing: {summary.firing.critical}</span>
          <span>Warning firing: {summary.firing.warning}</span>
          <span>Info firing: {summary.firing.info}</span>
          <span>Suppressed: {summary.suppressed}</span>
          <span>Resolved: {summary.resolved}</span>
        </div>
      </Section>

      <Section
        title="Filters"
        description="Filters are kept in the URL so an alert view can be shared."
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <label className="space-y-1 text-sm" htmlFor="alerts-state">
            <span className="font-medium">State</span>
            <select
              className="border-input bg-background h-9 w-full rounded-md border px-3"
              id="alerts-state"
              onChange={(event) => updateFilter('state', event.target.value, 'firing')}
              value={state}
            >
              <option value="firing">Firing</option>
              <option value="all">All states</option>
              <option value="pending">Pending</option>
              <option value="resolved">Resolved</option>
            </select>
          </label>
          <label className="space-y-1 text-sm" htmlFor="alerts-severity">
            <span className="font-medium">Severity</span>
            <select
              className="border-input bg-background h-9 w-full rounded-md border px-3"
              id="alerts-severity"
              onChange={(event) => updateFilter('severity', event.target.value)}
              value={severity}
            >
              <option value="all">All severities</option>
              <option value="critical">Critical</option>
              <option value="warning">Warning</option>
              <option value="info">Info</option>
            </select>
          </label>
          <label className="space-y-1 text-sm" htmlFor="alerts-cluster">
            <span className="font-medium">Cluster</span>
            <Input
              id="alerts-cluster"
              onChange={(event) => updateFilter('cluster_id', event.target.value)}
              placeholder="Cluster ID"
              value={clusterId}
            />
          </label>
          <label className="space-y-1 text-sm" htmlFor="alerts-suppressed">
            <span className="font-medium">Suppression</span>
            <select
              className="border-input bg-background h-9 w-full rounded-md border px-3"
              id="alerts-suppressed"
              onChange={(event) => updateFilter('suppressed', event.target.value)}
              value={suppressed}
            >
              <option value="all">All alerts</option>
              <option value="suppressed">Suppressed</option>
              <option value="unsuppressed">Not suppressed</option>
            </select>
          </label>
        </div>
      </Section>

      {selectedAlertKey ? (
        <Section title={`Alert detail: ${selectedAlertKey}`}>
          <button
            className="mb-3 underline"
            onClick={() => setSelectedAlertKey(null)}
            type="button"
          >
            Close detail
          </button>
          <Detail alertKey={selectedAlertKey} />
        </Section>
      ) : null}

      <Section
        title="Alerts"
        description={`${visibleAlerts.length} of ${alerts.length} alerts shown`}
      >
        {visibleAlerts.length > 0 ? (
          <div aria-label="Alerts" className="grid gap-4" role="list">
            {visibleAlerts.map((alert) => {
              const matchingSilence = alert.suppressed
                ? activeSilences.find((silence) => matchSilence(alert, silence))
                : undefined
              return (
                <div key={alert.alert_key} role="listitem">
                  <AlertRow
                    alert={alert}
                    matchingSilence={matchingSilence}
                    onSelect={setSelectedAlertKey}
                  />
                </div>
              )
            })}
          </div>
        ) : (
          <EmptyState
            description={
              alerts.length === 0
                ? 'No alerts have been emitted by the monitored fleet.'
                : 'Try clearing one or more filters or choose a different state.'
            }
            title={alerts.length === 0 ? 'No alerts' : 'No alerts match these filters'}
          />
        )}
      </Section>
    </div>
  )
}
