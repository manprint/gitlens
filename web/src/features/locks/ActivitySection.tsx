import { useMemo, type ReactNode } from 'react'
import { Link, useInRouterContext } from 'react-router-dom'

import type { PermTier } from '@/api/types'
import { HelpLink } from '@/components/layout/HelpLink'
import { InfoHint } from '@/components/layout/InfoHint'
import { MetricTile } from '@/components/layout/MetricTile'
import { Section } from '@/components/layout/Section'
import { Disabled, Stale, Unknown } from '@/components/state'
import { useT, type Translate } from '@/i18n/react'
import { activeIntl } from '@/i18n/store'
import type { CaptionId } from '@/lib/captions'
import {
  WRAPAROUND_RISK_THRESHOLD,
  breakdown,
  connectionSaturation,
  deadlockRate,
  latestSampleAt,
  latestValue,
  oldestStateAge,
  type ActivityMetrics,
} from '@/lib/activity'
import { formatCount, formatDuration, formatPercent, formatRelative } from '@/lib/format'
import { sessionKey, type LockNode } from '@/lib/locks'
import {
  connectionGroup,
  countOf,
  hasOrigin,
  splitByOrigin,
  usesOriginTables,
  type ConnectionsSummary,
} from '@/lib/sessions'
import {
  xminHorizonState,
  xminHolderKindLabel,
  xminIdentityLabel,
  xminIdentityText,
  xminNoneText,
  xminPendingText,
  type XminHorizon,
} from '@/lib/xmin'
import { ConnectionSummary } from './ConnectionSummary'
import { SessionsTable } from './SessionsTable'
import { SignalActions } from './SignalActions'

export interface ActivityResponseLike {
  stale: boolean
  metrics: ActivityMetrics
  xmin_horizon?: XminHorizon | null
}

function sessionValue(value: unknown): string {
  if (typeof value === 'string' && value.trim() !== '') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return '—'
}

function sessionQuery(value: unknown): string | null {
  if (typeof value !== 'string' || value.trim() === '') return null
  return value.split(/\r?\n/u, 1)[0]?.trim() ?? null
}

// Two decimals would show a steady trickle of deadlocks as 0.00. Below 0.01
// per second the rate keeps two significant digits so it stays visible.
function deadlockRateText(rate: number): string {
  if (rate > 0 && rate < 0.01) {
    return new Intl.NumberFormat(activeIntl(), { maximumSignificantDigits: 2 }).format(rate)
  }
  return new Intl.NumberFormat(activeIntl(), {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
    useGrouping: false,
  }).format(rate)
}

function BlockHeading({
  captionId,
  help,
  title,
}: {
  captionId: CaptionId
  help?: ReactNode
  title: string
}) {
  return (
    <div className="flex items-center gap-1" data-caption-id={captionId}>
      <h3 className="font-medium">{title}</h3>
      <InfoHint id={captionId} label={title} />
      {help}
    </div>
  )
}

/** The origin split with a noun for each number (D31): `2 application connections`. */
function originDetail(connections: ConnectionsSummary, t: Translate): string {
  const rows = connectionGroup(connections, 'by_origin')
  return `${t('locks.activity.applicationConnections', { count: countOf(rows, 'application') })} · ${t('locks.activity.pglensConnections', { count: countOf(rows, 'pglens') })}`
}

function ReadinessLink({ instanceId, item }: { instanceId: string | undefined; item: string }) {
  const t = useT()
  const inRouter = useInRouterContext()
  if (instanceId === undefined || !inRouter) return null
  return (
    <>
      {' '}
      <Link
        className="underline"
        to={`/instances/${encodeURIComponent(instanceId)}?section=readiness`}
      >
        {t('locks.activity.readinessItem', { item })}
      </Link>{' '}
      {t('locks.activity.readinessFix')}
    </>
  )
}

function ActiveSessions({
  sessions,
  currentTier,
  instanceId,
  sampleStale,
}: {
  sessions: readonly LockNode[]
  currentTier: PermTier
  instanceId: string
  sampleStale: boolean
}) {
  const t = useT()
  return (
    <div className="mt-6">
      <BlockHeading
        captionId="activity.sessions-active"
        title={t('locks.activity.activeSessions')}
      />
      {sessions.length > 0 && !hasOrigin(sessions) ? (
        <p className="text-muted-foreground mt-2 text-sm">{t('locks.activity.noOrigin')}</p>
      ) : null}
      {sessions.length === 0 ? (
        <p className="text-muted-foreground mt-2 text-sm">{t('locks.activity.noActiveSessions')}</p>
      ) : (
        <ul aria-label={t('locks.activity.activeSessions')} className="mt-3 space-y-3">
          {sessions.map((session) => (
            <li key={sessionKey(session)} className="rounded-md border p-3">
              <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-sm">
                <strong>
                  {t('locks.sessions.pid')} {sessionValue(session.pid)}
                </strong>
                <span>{sessionValue(session.state)}</span>
                <span className="text-muted-foreground">
                  {t('locks.activity.sessionDbUser', {
                    database: sessionValue(session.datname ?? session.database),
                    user: sessionValue(session.usename ?? session.username ?? session.user),
                  })}
                </span>
              </div>
              <p className="text-muted-foreground mt-1 truncate text-sm">
                {sessionQuery(session.query) ?? t('locks.activity.queryBlockingOnly')}
              </p>
              <SignalActions
                currentTier={currentTier}
                instanceId={instanceId}
                node={session}
                sampleStale={sampleStale}
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

const SAMPLE_INTERVAL_SECONDS = 10
const STALE_THRESHOLD_SECONDS = 20

function ageSince(sampledAt: string | null, now: Date): number | null {
  if (sampledAt === null) return null
  const timestamp = new Date(sampledAt).getTime()
  if (!Number.isFinite(timestamp)) return null
  return Math.max(0, (now.getTime() - timestamp) / 1000)
}

function valueOrUnknown(value: number | null) {
  return value === null ? <Unknown /> : formatCount(value)
}

function durationOrUnknown(value: number | null) {
  return value === null ? <Unknown /> : formatDuration(value)
}

// The oldest xmin holder names what keeps VACUUM from removing dead rows. Its
// age is in transaction IDs; the time since its start is shown only when the
// holder has one. Query text is never part of it.
function XminHorizonBlock({
  metrics,
  holder,
}: {
  metrics: ActivityMetrics
  holder: XminHorizon | null | undefined
}) {
  const t = useT()
  const xmin = xminHorizonState(metrics, holder)
  return (
    <div className="mt-6">
      <BlockHeading
        captionId="activity.xmin-holder"
        help={<HelpLink anchor="locks#xmin-holder" label={t('locks.activity.xminHolder')} />}
        title={t('locks.activity.xminHolder')}
      />
      {xmin.state === 'holder' ? (
        <dl className="mt-3 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[max-content_1fr]">
          <dt className="text-muted-foreground">{t('locks.activity.holderKind')}</dt>
          <dd>{xminHolderKindLabel(xmin.holder.holder_kind)}</dd>
          <dt className="text-muted-foreground">{xminIdentityLabel(xmin.holder.holder_kind)}</dt>
          <dd className="break-all">{xminIdentityText(xmin.holder)}</dd>
          <dt className="text-muted-foreground">{t('locks.activity.ageXids')}</dt>
          <dd className="tabular-nums">{formatCount(xmin.holder.age_xids)}</dd>
          <dt className="text-muted-foreground">{t('locks.activity.timeSinceStart')}</dt>
          <dd>
            {xmin.holder.age_seconds === null
              ? t('locks.activity.notMeasured')
              : formatDuration(xmin.holder.age_seconds)}
          </dd>
        </dl>
      ) : xmin.state === 'none' ? (
        <p className="text-muted-foreground mt-2 text-sm">{xminNoneText()}</p>
      ) : (
        <p className="text-muted-foreground mt-2 text-sm">{xminPendingText()}</p>
      )}
      <p className="text-muted-foreground mt-2 text-sm">{t('locks.activity.xminExplanation')}</p>
    </div>
  )
}

function BreakdownList({
  label,
  rows,
}: {
  label: string
  rows: readonly { label: string; value: number }[]
}) {
  const t = useT()
  if (rows.length === 0) {
    return <p className="text-muted-foreground text-sm">{t('locks.activity.noSamples')}</p>
  }

  return (
    <ul aria-label={label} className="space-y-1 text-sm">
      {rows.map((row) => (
        <li key={row.label} className="flex justify-between gap-4">
          <span className="truncate">{row.label}</span>
          <span className="tabular-nums">{formatCount(row.value)}</span>
        </li>
      ))}
    </ul>
  )
}

export function ActivitySection({
  response,
  sessions,
  sessionsStale = false,
  currentTier = 'T0',
  instanceId,
  now = new Date(),
  connections,
  part = 'all',
}: {
  response: ActivityResponseLike
  sessions?: readonly LockNode[]
  /** The connection summary of the lock response; its absence means a server older than 16.2. */
  connections?: ConnectionsSummary | undefined
  /** `tiles` and `details` split the section across two views of the page. */
  part?: 'all' | 'tiles' | 'details'
  // The sessions come from the lock sample, which can be stale while the
  // activity sample is fresh.
  sessionsStale?: boolean
  currentTier?: PermTier
  instanceId?: string
  now?: Date
}) {
  const t = useT()
  const sampledAt = useMemo(() => latestSampleAt(response.metrics), [response.metrics])
  const age = ageSince(sampledAt, now)
  const sampleStale = response.stale || (age !== null && age > STALE_THRESHOLD_SECONDS)
  const used = latestValue(response.metrics, 'pg_connections_used')
  const limit = latestValue(response.metrics, 'pg_connections_limit')
  const saturation = connectionSaturation(response.metrics)
  const stateRows = breakdown(response.metrics, 'pg_backends', 'state')
  const databaseRows = breakdown(response.metrics, 'pg_connections_by_database', 'datname')
  const applicationRows = breakdown(
    response.metrics,
    'pg_connections_by_application',
    'application_name',
  )
  const maxTransactionAge = latestValue(response.metrics, 'pg_max_xact_age_seconds')
  const maxIdleTransactionAge = latestValue(response.metrics, 'pg_max_idle_in_transaction_seconds')
  const maxStateAge = oldestStateAge(response.metrics)
  const preparedTransactions = latestValue(response.metrics, 'pg_prepared_xacts')
  const oldestPreparedTransaction = latestValue(response.metrics, 'pg_oldest_prepared_xact_seconds')
  const frozenXidAge = latestValue(response.metrics, 'pg_max_datfrozenxid_age')
  const rate = deadlockRate(response.metrics)
  const originMode = usesOriginTables(sessions ?? [], connections)
  const showTiles = part !== 'details'
  const showDetails = part !== 'tiles'

  return (
    <Section
      actions={<HelpLink anchor="locks#activity-tiles" label={t('locks.activity.tilesHelp')} />}
      captionId="activity.section"
      title={t('locks.activity.title')}
    >
      <p className="text-muted-foreground mb-3 text-sm">
        {t('locks.activity.sampleInterval', { seconds: SAMPLE_INTERVAL_SECONDS })}
        {connections ? t('locks.activity.originOn') : t('locks.activity.originOff')}
        {sampledAt ? (
          <> {t('locks.activity.latestSample', { time: formatRelative(sampledAt, now) })}</>
        ) : null}
      </p>
      {response.stale && age !== null ? (
        <Stale age={age} threshold={STALE_THRESHOLD_SECONDS}>
          <span>{t('locks.activity.staleData')}</span>
        </Stale>
      ) : null}

      {showTiles ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <MetricTile
            captionId="activity.connections-used"
            label={t('locks.activity.connectionsUsed')}
            value={used !== null ? formatCount(used) : <Unknown />}
            unit={used !== null ? 'connections' : undefined}
            detail={
              limit !== null || connections ? (
                <>
                  {limit !== null ? (
                    <span>
                      {saturation.ratio === null
                        ? t('locks.activity.limitSet', { limit: formatCount(limit) })
                        : t('locks.activity.limitOf', {
                            limit: formatCount(limit),
                            percent: formatPercent(saturation.ratio, 1) ?? '',
                          })}
                    </span>
                  ) : null}{' '}
                  {connections ? <span>{originDetail(connections, t)}</span> : null}
                </>
              ) : undefined
            }
          />
          <MetricTile
            captionId="activity.longest-transaction"
            label={t('locks.activity.longestTransaction')}
            value={durationOrUnknown(maxTransactionAge)}
          />
          <MetricTile
            captionId="activity.longest-idle-transaction"
            label={t('locks.activity.longestIdle')}
            value={durationOrUnknown(maxIdleTransactionAge)}
          />
          <MetricTile
            captionId="activity.oldest-state-age"
            label={t('locks.activity.oldestStateAge')}
            value={durationOrUnknown(maxStateAge)}
          />
        </div>
      ) : null}

      {showDetails ? (
        <div className="mt-6 grid gap-6 lg:grid-cols-3">
          <div>
            <BlockHeading captionId="activity.saturation" title={t('locks.activity.saturation')} />
            {saturation.ratio === null ? (
              <p className="text-muted-foreground mt-2 text-sm">
                {t('locks.activity.noLimitSample')}
              </p>
            ) : (
              <p role="status" className="text-muted-foreground mt-2 text-sm">
                {saturation.saturated
                  ? t('locks.activity.saturationNear')
                  : t('locks.activity.saturationBelow')}{' '}
                {t('locks.activity.saturationAdvice')}
              </p>
            )}
          </div>
          <div>
            <BlockHeading captionId="activity.by-database" title={t('locks.activity.byDatabase')} />
            <BreakdownList label={t('locks.activity.byDatabase')} rows={databaseRows} />
          </div>
          <div>
            <BlockHeading captionId="activity.by-state" title={t('locks.activity.byState')} />
            <BreakdownList label={t('locks.activity.byState')} rows={stateRows} />
          </div>
        </div>
      ) : null}

      {showTiles ? (
        <>
          <div className="mt-6 grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            <MetricTile
              captionId="activity.prepared-transactions"
              label={t('locks.activity.preparedTransactions')}
              unit={
                preparedTransactions !== null && preparedTransactions !== undefined
                  ? 'transactions'
                  : undefined
              }
              value={valueOrUnknown(preparedTransactions)}
            />
            <MetricTile
              captionId="activity.oldest-prepared-transaction"
              label={t('locks.activity.oldestPrepared')}
              value={durationOrUnknown(oldestPreparedTransaction)}
            />
            <MetricTile
              captionId="activity.frozen-xid-age"
              detail={t('locks.activity.frozenDetail')}
              label={t('locks.activity.sinceFreeze')}
              unit={
                frozenXidAge !== null && frozenXidAge !== undefined ? 'transactions' : undefined
              }
              value={valueOrUnknown(frozenXidAge)}
            />
            <MetricTile
              captionId="activity.deadlock-rate"
              label={t('locks.activity.deadlockRate')}
              value={rate === null ? <Unknown /> : deadlockRateText(rate)}
              unit="per second"
            />
          </div>
          <p className="text-muted-foreground mt-3 text-sm">
            {t('locks.activity.wraparoundNote', {
              threshold: formatCount(WRAPAROUND_RISK_THRESHOLD),
            })}
          </p>
        </>
      ) : null}

      {showDetails ? (
        <>
          <XminHorizonBlock holder={response.xmin_horizon} metrics={response.metrics} />

          <div className="mt-6">
            <BlockHeading
              captionId="activity.by-application"
              title={t('locks.activity.byApplication')}
            />
            <BreakdownList label={t('locks.activity.byApplication')} rows={applicationRows} />
            {connections?.by_application_enabled === false ? (
              <>
                <Disabled
                  feature={t('locks.activity.disabledFeature')}
                  configKey="checks.activity.by_application"
                />
                <p className="text-muted-foreground mt-2 text-sm">
                  {t('locks.activity.optIn')}
                  <ReadinessLink instanceId={instanceId} item="RDY-ACT-01" />
                </p>
              </>
            ) : null}
          </div>

          {connections ? (
            <div className="mt-6">
              <ConnectionSummary connections={connections} />
            </div>
          ) : null}
        </>
      ) : null}

      {part === 'all' && originMode && sessions && instanceId ? (
        <div className="mt-6 space-y-6">
          {(['application', 'pglens'] as const).map((origin) => (
            <SessionsTable
              key={origin}
              connections={connections}
              currentTier={currentTier}
              instanceId={instanceId}
              origin={origin}
              rows={splitByOrigin(sessions)[origin]}
              sampleStale={sampleStale || sessionsStale}
              sessions={sessions}
            />
          ))}
        </div>
      ) : null}

      {showDetails && !originMode && sessions && instanceId ? (
        <ActiveSessions
          currentTier={currentTier}
          instanceId={instanceId}
          sampleStale={sampleStale || sessionsStale}
          sessions={sessions}
        />
      ) : null}
    </Section>
  )
}
