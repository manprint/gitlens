import { useSyncExternalStore } from 'react'

import { displayTimeZone, subscribeTimeZone } from '@/lib/timezone'

const serverSnapshot = () => 'UTC'

/** The zone console times are written in, re-rendering when it changes. */
export function useDisplayTimeZone(): string {
  return useSyncExternalStore(subscribeTimeZone, displayTimeZone, serverSnapshot)
}
