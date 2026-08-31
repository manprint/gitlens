import { useEffect, useState } from 'react'

import { REFRESH, type RefreshSurface } from '@/api/policy'
import { formatDuration } from '@/lib/format'

export interface Freshness {
  age: number
  isStale: boolean
  label: string
}

/**
 * Derive a live age from the query's last successful update.
 *
 * A failed refetch does not change `dataUpdatedAt`, so the existing data keeps
 * aging and eventually becomes stale instead of appearing freshly fetched.
 */
export function useFreshness(dataUpdatedAt: number, policy: RefreshSurface): Freshness {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1_000)
    return () => window.clearInterval(timer)
  }, [dataUpdatedAt])

  const hasData = Number.isFinite(dataUpdatedAt) && dataUpdatedAt > 0
  const age = hasData ? Math.max(0, now - dataUpdatedAt) : 0
  const staleAfter = REFRESH[policy].staleAfter
  const isStale = hasData && staleAfter > 0 && age >= staleAfter
  const label = hasData ? `Updated ${formatDuration(Math.floor(age / 1_000))} ago` : 'No data yet'

  return { age, isStale, label }
}
