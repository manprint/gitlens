import type { MessageKey } from '@/i18n/messages/en'
import { translate } from '@/i18n/store'

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

const HOLDER_KIND_KEYS = {
  session: 'locks.xmin.kind.session',
  slot: 'locks.xmin.kind.slot',
  standby: 'locks.xmin.kind.standby',
  prepared: 'locks.xmin.kind.prepared',
} as const satisfies Record<XminHorizon['holder_kind'], MessageKey>

// The name of the identity field, in the words of its holder kind.
const IDENTITY_KEYS = {
  session: 'locks.xmin.identity.session',
  slot: 'locks.xmin.identity.slot',
  standby: 'locks.xmin.identity.standby',
  prepared: 'locks.xmin.identity.prepared',
} as const satisfies Record<XminHorizon['holder_kind'], MessageKey>

export function xminHolderKindLabel(kind: XminHorizon['holder_kind']): string {
  return translate(HOLDER_KIND_KEYS[kind])
}

export function xminIdentityLabel(kind: XminHorizon['holder_kind']): string {
  return translate(IDENTITY_KEYS[kind])
}

export function xminPendingText(): string {
  return translate('locks.xmin.pending')
}

export function xminNoneText(): string {
  return translate('locks.xmin.none')
}

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
  return holder.identity === '' ? translate('locks.xmin.emptyIdentity') : holder.identity
}
