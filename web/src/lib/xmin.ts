import type { ActivityMetrics } from './activity'
import { hasReported } from './activity'

/**
 * The oldest holder of the xmin horizon, as the activity sample reports it. The
 * identity is a session's pid, a replication slot's name, a standby's
 * application_name or a prepared transaction's gid. age_seconds is null when the
 * holder has no start time to measure from.
 */
export interface XminHorizon {
  holder_kind: 'session' | 'slot' | 'standby' | 'prepared'
  identity: string
  age_xids: number
  age_seconds: number | null
  sampled_at: string
}

export const XMIN_HOLDER_KIND_LABELS: Record<XminHorizon['holder_kind'], string> = {
  session: 'Session',
  slot: 'Replication slot',
  standby: 'Standby',
  prepared: 'Prepared transaction',
}

// The name of the identity field, in the words of its holder kind.
export const XMIN_IDENTITY_LABELS: Record<XminHorizon['holder_kind'], string> = {
  session: 'PID',
  slot: 'Slot name',
  standby: 'application_name',
  prepared: 'Transaction gid',
}

export const XMIN_PENDING_TEXT = 'xmin holder unknown: no activity snapshot'
export const XMIN_NONE_TEXT =
  'No session, replication slot, standby or prepared transaction holds an xmin in the latest activity sample.'

export type XminHorizonState =
  { state: 'holder'; holder: XminHorizon } | { state: 'none' } | { state: 'unknown' }

/**
 * What the activity section can say about the xmin horizon. A holder is named
 * when the latest sample reported one. With no holder, the section says no holder
 * holds the horizon when the sample did report the holder gauges, and that the
 * holder is unknown when no activity sample is there at all.
 */
export function xminHorizonState(
  metrics: ActivityMetrics,
  holder: XminHorizon | null | undefined,
): XminHorizonState {
  if (holder) return { state: 'holder', holder }
  if (hasReported(metrics, 'pg_xmin_horizon_age_xids')) return { state: 'none' }
  return { state: 'unknown' }
}

// An empty identity is shown as (none), as an empty application_name is.
export function xminIdentityText(holder: XminHorizon): string {
  return holder.identity === '' ? '(none)' : holder.identity
}
