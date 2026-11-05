import { Bell } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAlert, useAlerts, useCachedClusters, useSilences } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable } from '@/components/layout/SimpleTable'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { matchSilence, rankAlerts, summariseAlerts, type Alert, type Silence } from '@/lib/alerts'
import { nameLookup } from '@/lib/names'

import {
  ALERTS_SECTIONS,
  DEFAULT_ALERTS_SECTION,
  readAlertsSection,
  withSection,
} from './alertingSections'
import { alertColumns } from './AlertRow'
import { DeliverySection } from './DeliverySection'
import { NativeSelect } from '@/components/ui/native-select'
import { Callout } from '@/components/ui/callout'

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
  // A narrower list starts on its first page (D23).
  next.delete('alerts.page')
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

const TILE_TONE = {
  critical: 'text-destructive',
  warning: 'text-warning-foreground',
  info: 'text-primary',
} as const

// From 1600 px a tile is one compact line beside the tab list (D26).
function SummaryTile({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone?: keyof typeof TILE_TONE
}) {
  return (
    <div className="rounded-lg border px-4 py-3 min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-2 min-[1600px]:px-3 min-[1600px]:py-1">
      <dt className="text-muted-foreground text-xs font-medium tracking-wide uppercase">{label}</dt>
      <dd
        className={`mt-1 text-2xl font-semibold tabular-nums min-[1600px]:mt-0 min-[1600px]:text-base ${value > 0 && tone ? TILE_TONE[tone] : ''}`}
      >
        {value}
      </dd>
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
  // The silence links from AlertRow name the silence they were built from. The
  // list then shows the alerts that silence matches. A silence that is not in
  // the list shows nothing rather than every alert, and the page says so.
  const silenceId = searchParams.get('silence_id') ?? ''
  const alerts = alertsQuery.data ?? EMPTY_ALERTS
  const silences = silencesQuery.data ?? EMPTY_SILENCES
  const silenceFilter = silenceId
    ? silences.find((silence) => silence.silence_id === silenceId)
    : undefined
  // A silence that could not be loaded is not known to be absent, so the page
  // must not say it is missing from the list.
  const silenceNote = silencesQuery.error
    ? 'The silence named in this link could not be checked, so no alerts are shown.'
    : 'The silence named in this link is not in the current silences list.'
  const summary = useMemo(() => summariseAlerts(alerts), [alerts])
  const visibleAlerts = useMemo(
    () =>
      rankAlerts(
        alerts.filter(
          (alert) =>
            matchesFilter(alert, state, severity, clusterId, suppressed) &&
            (silenceId === '' ||
              (silenceFilter !== undefined && matchSilence(alert, silenceFilter))),
        ),
      ),
    [alerts, clusterId, severity, silenceFilter, silenceId, state, suppressed],
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

  // Cluster and instance names come from the list the shell polls (D14, D15).
  const cachedClusters = useCachedClusters()
  const names = useMemo(() => nameLookup(cachedClusters), [cachedClusters])
  const columns = useMemo(
    () =>
      alertColumns({
        names,
        onSelect: setSelectedAlertKey,
        silenceFor: (alert) =>
          alert.suppressed
            ? activeSilences.find((silence) => matchSilence(alert, silence))
            : undefined,
      }),
    [activeSilences, names],
  )
  const section = readAlertsSection(searchParams.get('section'))

  function showSection(next: string) {
    setSearchParams((current) => withSection(current, next, DEFAULT_ALERTS_SECTION), {
      replace: true,
    })
  }

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

  const filterLabel = 'flex items-center gap-2 text-sm'
  // From 1600 px the filters sit in the list header at the toolbar height (D26).
  const filterControl = 'min-[1600px]:h-8'
  const filters = (
    <>
      <span className="flex items-center gap-1">
        <label className={filterLabel} htmlFor="alerts-state">
          <span className="font-medium">State</span>
          <NativeSelect
            className={filterControl}
            id="alerts-state"
            onChange={(event) => updateFilter('state', event.target.value, 'firing')}
            value={state}
          >
            <option value="firing">Firing</option>
            <option value="all">All states</option>
            <option value="pending">Pending</option>
            <option value="resolved">Resolved</option>
          </NativeSelect>
        </label>
        <InfoHint id="alerts.filter-state" label="State filter" />
      </span>
      <span className="flex items-center gap-1">
        <label className={filterLabel} htmlFor="alerts-severity">
          <span className="font-medium">Severity</span>
          <NativeSelect
            className={filterControl}
            id="alerts-severity"
            onChange={(event) => updateFilter('severity', event.target.value)}
            value={severity}
          >
            <option value="all">All severities</option>
            <option value="critical">Critical</option>
            <option value="warning">Warning</option>
            <option value="info">Info</option>
          </NativeSelect>
        </label>
        <InfoHint id="alerts.filter-severity" label="Severity filter" />
      </span>
      <span className="flex items-center gap-1">
        <label className={filterLabel} htmlFor="alerts-cluster">
          <span className="font-medium">Cluster</span>
          <Input
            className={`w-44 ${filterControl}`}
            id="alerts-cluster"
            onChange={(event) => updateFilter('cluster_id', event.target.value)}
            placeholder="Cluster ID"
            value={clusterId}
          />
        </label>
        <InfoHint id="alerts.filter-cluster" label="Cluster filter" />
      </span>
      <span className="flex items-center gap-1">
        <label className={filterLabel} htmlFor="alerts-suppressed">
          <span className="font-medium">Suppression</span>
          <NativeSelect
            className={filterControl}
            id="alerts-suppressed"
            onChange={(event) => updateFilter('suppressed', event.target.value)}
            value={suppressed}
          >
            <option value="all">All alerts</option>
            <option value="suppressed">Suppressed</option>
            <option value="unsuppressed">Not suppressed</option>
          </NativeSelect>
        </label>
        <InfoHint id="alerts.filter-suppression" label="Suppression filter" />
      </span>
    </>
  )

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        icon={Bell}
        eyebrow="Alerting"
        title="Alerts and events"
        subtitle="Alerts raised by the alert rules: what is firing now, what was resolved, and what is silenced. Open an alert for its labels, history and delivery."
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
        <Callout variant="warning">
          Suppression details are unavailable; alerts remain visible without silence context.
          <button
            className="ml-2 underline"
            onClick={() => void silencesQuery.refetch()}
            type="button"
          >
            Retry silences
          </button>
        </Callout>
      ) : null}

      <AlertListTruncated truncated={alertsQuery.truncated} />
      {/* One view per tab (15.3): the alert list, or the delivery settings. */}
      <Tabs onValueChange={showSection} value={section}>
        <div
          className="flex flex-col gap-4 min-[1600px]:flex-row min-[1600px]:items-center min-[1600px]:justify-between"
          data-testid="alerts-tab-row"
        >
          <TabsList aria-label="Alert sections">
            {ALERTS_SECTIONS.map((entry) => (
              <TabsTrigger key={entry.id} value={entry.id}>
                {entry.label}
              </TabsTrigger>
            ))}
          </TabsList>
          <div className="flex items-center gap-1">
            {/* Firing alerts are counted by severity; suppressed alerts are shown separately. */}
            <dl
              aria-label="Alert summary"
              className="grid grid-cols-2 gap-3 min-[1600px]:flex min-[1600px]:gap-2 sm:grid-cols-3 lg:grid-cols-5"
            >
              <SummaryTile
                label="Critical firing"
                tone="critical"
                value={summary.firing.critical}
              />
              <SummaryTile label="Warning firing" tone="warning" value={summary.firing.warning} />
              <SummaryTile label="Info firing" tone="info" value={summary.firing.info} />
              <SummaryTile label="Suppressed" value={summary.suppressed} />
              <SummaryTile label="Resolved" value={summary.resolved} />
            </dl>
            <InfoHint id="alerts.summary" label="Alert summary counts" />
          </div>
        </div>
        <TabsContent className="space-y-8 min-[1600px]:space-y-4" value="alerts">
          {selectedAlertKey ? (
            <Section captionId="alerts.detail" title={`Alert detail: ${selectedAlertKey}`}>
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

          <Section actions={filters} captionId="alerts.list" title="Alerts">
            <p aria-live="polite" className="text-muted-foreground mb-3 text-sm">
              {`${visibleAlerts.length} of ${alerts.length} alerts shown; filters are kept in the URL so the view can be shared.`}
            </p>
            {silenceId ? (
              <p className="mb-3 text-sm" role="status">
                {silenceFilter
                  ? `Showing alerts matched by silence “${silenceFilter.reason}”.`
                  : silenceNote}{' '}
                <button
                  className="text-foreground underline"
                  onClick={() => updateFilter('silence_id', '')}
                  type="button"
                >
                  Show all alerts
                </button>
              </p>
            ) : null}
            <SimpleTable
              ariaLabel="Alerts"
              columns={columns}
              emptyState={
                <EmptyState
                  description={
                    alerts.length === 0
                      ? 'No alerts have been emitted by the monitored fleet.'
                      : 'Try clearing one or more filters or choose a different state.'
                  }
                  title={alerts.length === 0 ? 'No alerts' : 'No alerts match these filters'}
                />
              }
              rowKey={(alert) => alert.alert_key}
              rows={visibleAlerts}
              scope="alerts"
              tableId="alerts"
            />
          </Section>
        </TabsContent>
        <TabsContent value="delivery">
          <Section captionId="alerts.delivery" title="Notification delivery">
            <DeliverySection />
          </Section>
        </TabsContent>
      </Tabs>
    </div>
  )
}
