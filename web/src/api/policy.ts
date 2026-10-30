import type { RefreshIntervalPreference } from '@/lib/preferences'

export const REFRESH = {
  fleet: { interval: 15_000, staleAfter: 45_000 },
  cluster: { interval: 15_000, staleAfter: 45_000 },
  instance: { interval: 15_000, staleAfter: 45_000 },
  locks: { interval: 5_000, staleAfter: 20_000 },
  activity: { interval: 5_000, staleAfter: 20_000 },
  ash: { interval: 30_000, staleAfter: 90_000 },
  statements: { interval: 60_000, staleAfter: 180_000 },
  findings: { interval: 60_000, staleAfter: 300_000 },
  catalog: { interval: 60_000, staleAfter: 300_000 },
  alerts: { interval: 15_000, staleAfter: 60_000 },
  // Per-instance inventory read on the settings page. The page fans out to every
  // instance, so these polls run at a slower cadence than the instance page.
  inventory: { interval: 60_000, staleAfter: 180_000 },
  command: { interval: 1_000, staleAfter: 0 },
  static: { interval: 0, staleAfter: 0 },
} as const

export type RefreshSurface = keyof typeof REFRESH
export type RefreshPolicy = (typeof REFRESH)[RefreshSurface]

/** A polling interval and the age at which its data is shown as stale (0: never). */
export interface RefreshTiming {
  readonly interval: number
  readonly staleAfter: number
}

/** The intervals a `refreshInterval` preference can choose. */
const PREFERRED_INTERVAL_MS: Record<'15s' | '30s' | '60s', number> = {
  '15s': 15_000,
  '30s': 30_000,
  '60s': 60_000,
}

/**
 * Surfaces that poll faster than this keep their policy whatever the
 * preference: `command` (1 s), `locks` and `activity` (5 s), and `static`
 * (no polling).
 */
export const PREFERENCE_MIN_POLICY_INTERVAL = 15_000

/**
 * The timing a surface uses under the `refreshInterval` preference
 * (R-11.0 P1). `auto` keeps the policy. A chosen interval replaces the policy
 * interval of the surfaces polling every 15 s or slower and scales their
 * `staleAfter` by the same ratio; `off` stops them and turns their stale
 * badge off (`staleAfter` 0). Faster surfaces always keep their policy.
 */
export function effectiveRefresh(
  policy: RefreshTiming,
  preference: RefreshIntervalPreference,
): RefreshTiming {
  if (preference === 'auto' || policy.interval < PREFERENCE_MIN_POLICY_INTERVAL) return policy
  if (preference === 'off') return { interval: 0, staleAfter: 0 }
  const interval = PREFERRED_INTERVAL_MS[preference]
  return { interval, staleAfter: Math.round((policy.staleAfter * interval) / policy.interval) }
}
