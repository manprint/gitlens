import { useSyncExternalStore } from 'react'

import { effectiveRefresh, type RefreshTiming } from '@/api/policy'
import {
  DEFAULT_PREFERENCES,
  getPreferencesSnapshot,
  subscribePreferences,
  type Preferences,
} from '@/lib/preferences'

const serverSnapshot = () => DEFAULT_PREFERENCES

/** The console preferences, re-rendering on every change (this tab or another). */
export function usePreferences(): Preferences {
  return useSyncExternalStore(subscribePreferences, getPreferencesSnapshot, serverSnapshot)
}

/** A surface's polling timing under the `refreshInterval` preference (R-11.0 P1). */
export function useRefreshTiming(policy: RefreshTiming): RefreshTiming {
  return effectiveRefresh(policy, usePreferences().refreshInterval)
}
