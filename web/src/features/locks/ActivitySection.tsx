import { useMemo } from 'react'

import { MetricTile } from '@/components/layout/MetricTile'
import { Section } from '@/components/layout/Section'
import { Disabled, Stale, Unknown } from '@/components/state'
import {
  WRAPAROUND_RISK_THRESHOLD,
  breakdown,
  connectionSaturation,
  deadlockRate,
  hasReported,
  latestSampleAt,
  latestValue,
  oldestStateAge,
  type ActivityMetrics,
} from '@/lib/activity'
import { formatCount, formatDuration, formatPercent, formatRelative } from '@/lib/format'

export interface ActivityResponseLike {
  stale: boolean
  metrics: ActivityMetrics
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
  now = new Date(),
}: {
  response: ActivityResponseLike
  now?: Date
}) {
  const sampledAt = useMemo(() => latestSampleAt(response.metrics), [response.metrics])
  const age = ageSince(sampledAt, now)
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
  const maxIdleTransactionAge =
    latestValue(response.metrics, 'pg_max_idle_in_transaction_seconds') ??
    latestValue(response.metrics, 'pg_max_idle_in_txn_seconds')
  const maxStateAge = oldestStateAge(response.metrics)
  const preparedTransactions = latestValue(response.metrics, 'pg_prepared_xacts')
  const oldestPreparedTransaction = latestValue(response.metrics, 'pg_oldest_prepared_xact_seconds')
  const frozenXidAge = latestValue(response.metrics, 'pg_max_datfrozenxid_age')
  const rate = deadlockRate(response.metrics)

  return (
    <Section
      title="Activity"
      description={
        <>
          Activity is sampled every {SAMPLE_INTERVAL_SECONDS} seconds. Breakdowns are available per
          state and per database; application counts appear only when the agent opts in.
          {sampledAt ? <> Latest activity sample: {formatRelative(sampledAt, now)}.</> : null}
        </>
      }
    >
      {response.stale && age !== null ? (
        <Stale age={age} threshold={STALE_THRESHOLD_SECONDS}>
          <span>Activity data is older than the freshness threshold.</span>
        </Stale>
      ) : null}

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <MetricTile
          label="Connections used / max_connections"
          value={
            used !== null && limit !== null ? (
              `${formatCount(used)} / ${formatCount(limit)}`
            ) : (
              <Unknown />
            )
          }
          unit={
            saturation.ratio === null
              ? undefined
              : (formatPercent(saturation.ratio, 1) ?? undefined)
          }
        />
        <MetricTile label="Longest transaction" value={durationOrUnknown(maxTransactionAge)} />
        <MetricTile
          label="Longest idle-in-transaction"
          value={durationOrUnknown(maxIdleTransactionAge)}
        />
        <MetricTile label="Oldest state age" value={durationOrUnknown(maxStateAge)} />
      </div>

      <div className="mt-6 grid gap-6 lg:grid-cols-3">
        <div>
          <h3 className="font-medium">Connection saturation</h3>
          {saturation.ratio === null ? (
            <p className="text-muted-foreground mt-2 text-sm">No connection limit sample.</p>
          ) : (
            <p role="status" className="text-muted-foreground mt-2 text-sm">
              {saturation.saturated ? 'Near max_connections.' : 'Below the saturation threshold.'}{' '}
              The conn.near_max advisor threshold is: Connections above 80 percent of
              max_connections.
            </p>
          )}
        </div>
        <div>
          <h3 className="font-medium">Connections by database</h3>
          <BreakdownList label="Connections by database" rows={databaseRows} />
        </div>
        <div>
          <h3 className="font-medium">Backends by state</h3>
          <BreakdownList label="Backends by state" rows={stateRows} />
        </div>
      </div>

      <div className="mt-6 grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <MetricTile label="Prepared transactions" value={valueOrUnknown(preparedTransactions)} />
        <MetricTile
          label="Oldest prepared transaction"
          value={durationOrUnknown(oldestPreparedTransaction)}
        />
        <MetricTile label="Frozen-XID age" value={valueOrUnknown(frozenXidAge)} />
        <MetricTile
          label="Deadlock rate"
          value={rate === null ? <Unknown /> : rate.toFixed(2)}
          unit="per second"
        />
      </div>
      <p className="text-muted-foreground mt-3 text-sm">
        Frozen-XID age is shown with the table.wraparound_risk framing: maintenance is urgent as the
        age approaches {formatCount(WRAPAROUND_RISK_THRESHOLD)} transaction IDs. Deadlock rate is
        derived from counter samples; identifying involved statements requires PostgreSQL log
        analysis.
      </p>

      <div className="mt-6">
        <h3 className="font-medium">Connections by application</h3>
        {hasReported(response.metrics, 'pg_connections_by_application') ? (
          <BreakdownList label="Connections by application" rows={applicationRows} />
        ) : (
          <>
            <Disabled
              feature="Per-application activity counts"
              configKey="checks.activity.by_application"
            />
            <p className="text-muted-foreground mt-2 text-sm">
              This breakdown is opt-in and was not reported by the agent.
            </p>
          </>
        )}
      </div>
    </Section>
  )
}
