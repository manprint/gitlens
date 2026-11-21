import { Bell } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'

import { useAlert, useAlerts, useCachedClusters, useSilences } from '@/api/queries'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { HelpLink } from '@/components/layout/HelpLink'
import { PageHeader } from '@/components/layout/PageHeader'
import { Section } from '@/components/layout/Section'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable } from '@/components/layout/SimpleTable'
import { AlertListTruncated, EmptyState, ErrorState } from '@/components/state'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useT } from '@/i18n/react'
import { matchSilence, rankAlerts, summariseAlerts, type Alert, type Silence } from '@/lib/alerts'
import { nameLookup } from '@/lib/names'

import {
  ALERTS_SECTIONS,
  DEFAULT_ALERTS_SECTION,
  readAlertsSection,
  withSection,
} from './alertingSections'
import { alertColumns, stateText } from './AlertRow'
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
  const t = useT()
  const detailQuery = useAlert(alertKey)

  if (detailQuery.isPending) {
    return (
      <div aria-busy="true" aria-label={t('alerts.detail.loadingLabel')} role="status">
        {t('alerts.detail.loading')}
      </div>
    )
  }
  if (detailQuery.error) {
    return (
      <ErrorState
        endpoint={t('alerts.endpoint.alertDetail')}
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
          <dt className="font-medium">{t('alerts.detail.rule')}</dt>
          <dd>{alert.rule_id}</dd>
        </div>
        <div>
          <dt className="font-medium">{t('alerts.column.state')}</dt>
          <dd>{stateText(alert.state, t)}</dd>
        </div>
        <div>
          <dt className="font-medium">{t('alerts.detail.labels')}</dt>
          <dd>
            <code>{JSON.stringify(alert.labels)}</code>
          </dd>
        </div>
        <div>
          <dt className="font-medium">{t('alerts.detail.history')}</dt>
          <dd>
            {t('alerts.detail.historyText', {
              started: alert.started_at,
              evaluated: alert.last_eval_at,
              resolved: alert.resolved_at ?? t('alerts.detail.notYet'),
            })}
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
  const t = useT()
  return (
    <div className="rounded-lg border px-4 py-3 min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-2 min-[1600px]:px-3 min-[1600px]:py-1">
      <dt className="text-muted-foreground text-xs font-medium tracking-wide uppercase">{label}</dt>
      <dd
        className={`mt-1 text-2xl font-semibold tabular-nums min-[1600px]:mt-0 min-[1600px]:text-base ${value > 0 && tone ? TILE_TONE[tone] : ''}`}
      >
        {value}
        <span className="text-muted-foreground ml-1 text-xs font-normal">
          {t('alerts.tile.unit', { count: value })}
        </span>
      </dd>
    </div>
  )
}

export function AlertsPage() {
  const t = useT()
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
    ? t('alerts.silence.unchecked')
    : t('alerts.silence.missing')
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
        t,
        silenceFor: (alert) =>
          alert.suppressed
            ? activeSilences.find((silence) => matchSilence(alert, silence))
            : undefined,
      }),
    [activeSilences, names, t],
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
        endpoint={t('alerts.endpoint.alerts')}
        failure={alertsQuery.error}
        onRetry={() => void alertsQuery.refetch()}
      />
    )
  }
  if (alertsQuery.isPending || silencesQuery.isPending) {
    return (
      <section aria-busy="true" aria-label={t('alerts.loading.label')} role="status">
        {t('alerts.loading.text')}
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
          <span className="font-medium">{t('alerts.column.state')}</span>
          <NativeSelect
            className={filterControl}
            id="alerts-state"
            onChange={(event) => updateFilter('state', event.target.value, 'firing')}
            value={state}
          >
            <option value="firing">{t('alerts.filter.firing')}</option>
            <option value="all">{t('alerts.filter.allStates')}</option>
            <option value="pending">{t('alerts.filter.pending')}</option>
            <option value="resolved">{t('alerts.status.resolved')}</option>
          </NativeSelect>
        </label>
        <InfoHint id="alerts.filter-state" label={t('alerts.filter.stateHint')} />
      </span>
      <span className="flex items-center gap-1">
        <label className={filterLabel} htmlFor="alerts-severity">
          <span className="font-medium">{t('alerts.column.severity')}</span>
          <NativeSelect
            className={filterControl}
            id="alerts-severity"
            onChange={(event) => updateFilter('severity', event.target.value)}
            value={severity}
          >
            <option value="all">{t('alerts.filter.allSeverities')}</option>
            <option value="critical">{t('alerts.filter.critical')}</option>
            <option value="warning">{t('alerts.filter.warning')}</option>
            <option value="info">{t('alerts.filter.info')}</option>
          </NativeSelect>
        </label>
        <InfoHint id="alerts.filter-severity" label={t('alerts.filter.severityHint')} />
      </span>
      <span className="flex items-center gap-1">
        <label className={filterLabel} htmlFor="alerts-cluster">
          <span className="font-medium">{t('alerts.column.cluster')}</span>
          <Input
            className={`w-44 ${filterControl}`}
            id="alerts-cluster"
            onChange={(event) => updateFilter('cluster_id', event.target.value)}
            placeholder={t('alerts.filter.clusterPlaceholder')}
            value={clusterId}
          />
        </label>
        <InfoHint id="alerts.filter-cluster" label={t('alerts.filter.clusterHint')} />
      </span>
      <span className="flex items-center gap-1">
        <label className={filterLabel} htmlFor="alerts-suppressed">
          <span className="font-medium">{t('alerts.filter.suppression')}</span>
          <NativeSelect
            className={filterControl}
            id="alerts-suppressed"
            onChange={(event) => updateFilter('suppressed', event.target.value)}
            value={suppressed}
          >
            <option value="all">{t('alerts.filter.allAlerts')}</option>
            <option value="suppressed">{t('alerts.status.suppressed')}</option>
            <option value="unsuppressed">{t('alerts.filter.notSuppressed')}</option>
          </NativeSelect>
        </label>
        <InfoHint id="alerts.filter-suppression" label={t('alerts.filter.suppressionHint')} />
      </span>
    </>
  )

  return (
    <div className="space-y-8 min-[1600px]:space-y-4">
      <PageHeader
        help={<HelpLink anchor="alerts" label={t('alerts.page.helpLabel')} />}
        icon={Bell}
        eyebrow={t('alerts.page.eyebrow')}
        title={t('alerts.page.title')}
        subtitle={t('alerts.page.subtitle')}
        freshness={<FreshnessBadge dataUpdatedAt={alertsQuery.dataUpdatedAt} policy="alerts" />}
      />

      {alertsQuery.error ? (
        <ErrorState
          endpoint={t('alerts.endpoint.alerts')}
          failure={alertsQuery.error}
          onRetry={() => void alertsQuery.refetch()}
        />
      ) : null}
      {silencesQuery.error ? (
        <Callout variant="warning">
          {t('alerts.callout.silencesUnavailable')}
          <button
            className="ml-2 underline"
            onClick={() => void silencesQuery.refetch()}
            type="button"
          >
            {t('alerts.callout.retry')}
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
          <TabsList aria-label={t('alerts.tabs.label')}>
            {ALERTS_SECTIONS.map((entry) => (
              <TabsTrigger key={entry.id} value={entry.id}>
                {t(entry.labelKey)}
              </TabsTrigger>
            ))}
          </TabsList>
          <div className="flex items-center gap-1">
            {/* Firing alerts are counted by severity; suppressed alerts are shown separately. */}
            <dl
              aria-label={t('alerts.summary.title')}
              className="grid grid-cols-2 gap-3 min-[1600px]:flex min-[1600px]:gap-2 sm:grid-cols-3 lg:grid-cols-5"
            >
              <SummaryTile
                label={t('alerts.summary.criticalFiring')}
                tone="critical"
                value={summary.firing.critical}
              />
              <SummaryTile
                label={t('alerts.summary.warningFiring')}
                tone="warning"
                value={summary.firing.warning}
              />
              <SummaryTile
                label={t('alerts.summary.infoFiring')}
                tone="info"
                value={summary.firing.info}
              />
              <SummaryTile label={t('alerts.status.suppressed')} value={summary.suppressed} />
              <SummaryTile label={t('alerts.status.resolved')} value={summary.resolved} />
            </dl>
            <InfoHint id="alerts.summary" label={t('alerts.summary.countsHint')} />
            <HelpLink anchor="alerts#summary-tiles" label={t('alerts.summary.title')} />
          </div>
        </div>
        <TabsContent className="space-y-8 min-[1600px]:space-y-4" value="alerts">
          {selectedAlertKey ? (
            <Section
              actions={
                <HelpLink anchor="alerts#alert-detail" label={t('alerts.detail.helpLabel')} />
              }
              captionId="alerts.detail"
              title={t('alerts.detail.title', { key: selectedAlertKey })}
            >
              <button
                className="mb-3 underline"
                onClick={() => setSelectedAlertKey(null)}
                type="button"
              >
                {t('alerts.detail.close')}
              </button>
              <Detail alertKey={selectedAlertKey} />
            </Section>
          ) : null}

          <Section
            actions={
              <>
                {filters}
                <HelpLink anchor="alerts#alert-table" label={t('alerts.list.tableHelp')} />
              </>
            }
            captionId="alerts.list"
            title={t('alerts.section.alerts')}
          >
            <p aria-live="polite" className="text-muted-foreground mb-3 text-sm">
              {t('alerts.list.shown', { shown: visibleAlerts.length, total: alerts.length })}
            </p>
            {silenceId ? (
              <p className="mb-3 text-sm" role="status">
                {silenceFilter
                  ? t('alerts.list.silenceMatched', { reason: silenceFilter.reason })
                  : silenceNote}{' '}
                <button
                  className="text-foreground underline"
                  onClick={() => updateFilter('silence_id', '')}
                  type="button"
                >
                  {t('alerts.list.showAll')}
                </button>
              </p>
            ) : null}
            <SimpleTable
              ariaLabel={t('alerts.section.alerts')}
              captionId="alerts.table"
              columns={columns}
              emptyState={
                <EmptyState
                  description={
                    alerts.length === 0
                      ? t('alerts.empty.noneEmitted')
                      : t('alerts.empty.tryFilters')
                  }
                  title={alerts.length === 0 ? t('alerts.empty.none') : t('alerts.empty.noMatch')}
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
          <Section
            actions={<HelpLink anchor="alerts#delivery" label={t('alerts.section.delivery')} />}
            captionId="alerts.delivery"
            title={t('alerts.section.delivery')}
          >
            <DeliverySection />
          </Section>
        </TabsContent>
      </Tabs>
    </div>
  )
}
