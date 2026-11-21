import { useEffect, useState } from 'react'

import { REFRESH, type RefreshSurface } from '@/api/policy'
import { useT } from '@/i18n/react'
import { formatDuration } from '@/lib/format'

import { useRefreshTiming } from './usePreferences'

export interface Freshness {
  age: number
  /** The age alone (`12s`), empty before the first update. */
  ageText: string
  isStale: boolean
  label: string
  /** The stale threshold in force (ms; 0: never stale), after the refresh preference. */
  staleAfter: number
}

/**
 * Derive a live age from the query's last successful update.
 *
 * A failed refetch does not change `dataUpdatedAt`, so the existing data keeps
 * aging and eventually becomes stale instead of appearing freshly fetched.
 */
export function useFreshness(dataUpdatedAt: number, policy: RefreshSurface): Freshness {
  const t = useT()
  const [now, setNow] = useState(() => Date.now())
  // A chosen refresh interval scales the threshold; `off` disables it (R-11.0 P1).
  const { staleAfter } = useRefreshTiming(REFRESH[policy])

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1_000)
    return () => window.clearInterval(timer)
  }, [dataUpdatedAt])

  const hasData = Number.isFinite(dataUpdatedAt) && dataUpdatedAt > 0
  const age = hasData ? Math.max(0, now - dataUpdatedAt) : 0
  const isStale = hasData && staleAfter > 0 && age >= staleAfter
  const ageText = hasData ? formatDuration(Math.floor(age / 1_000)) : ''
  const label = hasData ? t('shell.fresh.updated', { age: ageText }) : t('shell.fresh.noData')

  return { age, ageText, isStale, label, staleAfter }
}
