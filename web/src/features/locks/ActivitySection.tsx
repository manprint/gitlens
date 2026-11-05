import { useMemo } from 'react'
import { Link, useInRouterContext } from 'react-router-dom'

import type { PermTier } from '@/api/types'
import { InfoHint } from '@/components/layout/InfoHint'
import { MetricTile } from '@/components/layout/MetricTile'
import { Section } from '@/components/layout/Section'
import { Disabled, Stale, Unknown } from '@/components/state'
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
  XMIN_HOLDER_KIND_LABELS,
  XMIN_IDENTITY_LABELS,
  XMIN_NONE_TEXT,
  XMIN_PENDING_TEXT,
  xminHorizonState,
  xminIdentityText,
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
    return new Intl.NumberFormat('en-US', { maximumSignificantDigits: 2 }).format(rate)
  }
  return rate.toFixed(2)
}

function BlockHeading({ captionId, title }: { captionId: CaptionId; title: string }) {
  return (
    <div className="flex items-center gap-1" data-caption-id={captionId}>
      <h3 className="font-medium">{title}</h3>
      <InfoHint id={captionId} label={title} />
    </div>
  )
}

function plural(count: number, noun: string): string {
  return `${formatCount(count)} ${noun}${count === 1 ? '' : 's'}`
}

/** The origin split with a noun for each number (D31): `2 application connections`. */
function originDetail(connections: ConnectionsSummary): string {
  const rows = connectionGroup(connections, 'by_origin')
  return `${plural(countOf(rows, 'application'), 'application connection')} · ${plural(countOf(rows, 'pglens'), 'pglens connection')}`
}

function ReadinessLink({ instanceId, item }: { instanceId: string | undefined; item: string }) {
  const inRouter = useInRouterContext()
  if (instanceId === undefined || !inRouter) return null
  return (
    <>
      {' '}
      <Link
        className="underline"
        to={`/instances/${encodeURIComponent(instanceId)}?section=readiness`}
      >
        Readiness item {item}
      </Link>{' '}
      has the fix.
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
  return (
    <div className="mt-6">
      <BlockHeading captionId="activity.sessions-active" title="Active sessions" />
      {sessions.length > 0 && !hasOrigin(sessions) ? (
        <p className="text-muted-foreground mt-2 text-sm">
          This server does not report the origin of a session, so application and pglens sessions
          are not told apart.
        </p>
      ) : null}
      {sessions.length === 0 ? (
        <p className="text-muted-foreground mt-2 text-sm">
          No active sessions in the latest sample.
        </p>
      ) : (
        <ul aria-label="Active sessions" className="mt-3 space-y-3">
          {sessions.map((session) => (
            <li key={sessionKey(session)} className="rounded-md border p-3">
              <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-sm">
                <strong>PID {sessionValue(session.pid)}</strong>
                <span>{sessionValue(session.state)}</span>
                <span className="text-muted-foreground">
                  database {sessionValue(session.datname ?? session.database)}; user{' '}
                  {sessionValue(session.usename ?? session.username ?? session.user)}
                </span>
              </div>
              <p className="text-muted-foreground mt-1 truncate text-sm">
                {sessionQuery(session.query) ??
                  'Query text shown only for sessions in a blocking tree.'}
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
  const xmin = xminHorizonState(metrics, holder)
  return (
    <div className="mt-6">
      <BlockHeading captionId="activity.xmin-holder" title="Oldest xmin holder" />
      {xmin.state === 'holder' ? (
        <dl className="mt-3 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[max-content_1fr]">
          <dt className="text-muted-foreground">Holder kind</dt>
          <dd>{XMIN_HOLDER_KIND_LABELS[xmin.holder.holder_kind]}</dd>
          <dt className="text-muted-foreground">{XMIN_IDENTITY_LABELS[xmin.holder.holder_kind]}</dt>
          <dd className="break-all">{xminIdentityText(xmin.holder)}</dd>
          <dt className="text-muted-foreground">Age in transaction IDs</dt>
          <dd className="tabular-nums">{formatCount(xmin.holder.age_xids)}</dd>
          <dt className="text-muted-foreground">Time since start</dt>
          <dd>
            {xmin.holder.age_seconds === null
              ? 'not measured for this holder kind'
              : formatDuration(xmin.holder.age_seconds)}
          </dd>
        </dl>
      ) : xmin.state === 'none' ? (
        <p className="text-muted-foreground mt-2 text-sm">{XMIN_NONE_TEXT}</p>
      ) : (
        <p className="text-muted-foreground mt-2 text-sm">{XMIN_PENDING_TEXT}</p>
      )}
      <p className="text-muted-foreground mt-2 text-sm">
        The xmin horizon is the oldest transaction ID VACUUM must keep. Time since start is the
        start of the session's transaction, or the prepare time of a prepared transaction.
      </p>
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
  if (rows.length === 0) {
    return <p className="text-muted-foreground text-sm">No samples reported.</p>
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
    <Section captionId="activity.section" title="Activity">
      <p className="text-muted-foreground mb-3 text-sm">
        Activity is sampled every {SAMPLE_INTERVAL_SECONDS} seconds. Breakdowns are available per
        state and per database
        {connections ? (
          <>
            ; application counts are on by default and can be turned off in the agent. Connections
            are split by origin: application or pglens.
          </>
        ) : (
          <>; application counts appear only when the agent opts in.</>
        )}
        {sampledAt ? <> Latest activity sample: {formatRelative(sampledAt, now)}.</> : null}
      </p>
      {response.stale && age !== null ? (
        <Stale age={age} threshold={STALE_THRESHOLD_SECONDS}>
          <span>Activity data is older than the freshness threshold.</span>
        </Stale>
      ) : null}

      {showTiles ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <MetricTile
            captionId="activity.connections-used"
            label="Connections used / max_connections"
            value={used !== null ? formatCount(used) : <Unknown />}
            unit={used !== null ? 'connections' : undefined}
            detail={
              limit !== null || connections ? (
                <>
                  {limit !== null ? (
                    <span>
                      {saturation.ratio === null
                        ? `Limit set by max_connections: ${formatCount(limit)} connections.`
                        : `Of ${formatCount(limit)} allowed by max_connections (${formatPercent(saturation.ratio, 1) ?? ''} used).`}
                    </span>
                  ) : null}{' '}
                  {connections ? <span>{originDetail(connections)}</span> : null}
                </>
              ) : undefined
            }
          />
          <MetricTile
            captionId="activity.longest-transaction"
            label="Longest transaction"
            value={durationOrUnknown(maxTransactionAge)}
          />
          <MetricTile
            captionId="activity.longest-idle-transaction"
            label="Longest idle-in-transaction"
            value={durationOrUnknown(maxIdleTransactionAge)}
          />
          <MetricTile
            captionId="activity.oldest-state-age"
            label="Oldest state age"
            value={durationOrUnknown(maxStateAge)}
          />
        </div>
      ) : null}

      {showDetails ? (
        <div className="mt-6 grid gap-6 lg:grid-cols-3">
          <div>
            <BlockHeading captionId="activity.saturation" title="Connection saturation" />
            {saturation.ratio === null ? (
              <p className="text-muted-foreground mt-2 text-sm">No connection limit sample.</p>
            ) : (
              <p role="status" className="text-muted-foreground mt-2 text-sm">
                {saturation.saturated ? 'Near max_connections.' : 'Below the saturation threshold.'}{' '}
                The conn.saturation advisor threshold is: Connections above 80 percent of
                max_connections.
              </p>
            )}
          </div>
          <div>
            <BlockHeading captionId="activity.by-database" title="Connections by database" />
            <BreakdownList label="Connections by database" rows={databaseRows} />
          </div>
          <div>
            <BlockHeading captionId="activity.by-state" title="Backends by state" />
            <BreakdownList label="Backends by state" rows={stateRows} />
          </div>
        </div>
      ) : null}

      {showTiles ? (
        <>
          <div className="mt-6 grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            <MetricTile
              captionId="activity.prepared-transactions"
              label="Prepared transactions"
              value={valueOrUnknown(preparedTransactions)}
            />
            <MetricTile
              captionId="activity.oldest-prepared-transaction"
              label="Oldest prepared transaction"
              value={durationOrUnknown(oldestPreparedTransaction)}
            />
            <MetricTile
              captionId="activity.frozen-xid-age"
              detail="Transaction IDs used since the oldest database was last frozen."
              label="Frozen-XID age"
              value={valueOrUnknown(frozenXidAge)}
            />
            <MetricTile
              captionId="activity.deadlock-rate"
              label="Deadlock rate"
              value={rate === null ? <Unknown /> : deadlockRateText(rate)}
              unit="per second"
            />
          </div>
          <p className="text-muted-foreground mt-3 text-sm">
            Frozen-XID age is shown with the txn.wraparound_risk framing: maintenance is urgent as
            the age approaches {formatCount(WRAPAROUND_RISK_THRESHOLD)} transaction IDs. Deadlock
            rate is the sum of each database's latest rate; identifying involved statements requires
            PostgreSQL log analysis.
          </p>
        </>
      ) : null}

      {showDetails ? (
        <>
          <XminHorizonBlock holder={response.xmin_horizon} metrics={response.metrics} />

          <div className="mt-6">
            <BlockHeading captionId="activity.by-application" title="Connections by application" />
            <BreakdownList label="Connections by application" rows={applicationRows} />
            {connections?.by_application_enabled === false ? (
              <>
                <Disabled
                  feature="Per-application activity counts"
                  configKey="checks.activity.by_application"
                />
                <p className="text-muted-foreground mt-2 text-sm">
                  This breakdown is opt-in and was not reported by the agent.
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
