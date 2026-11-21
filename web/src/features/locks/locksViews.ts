/**
 * The tabs of the Locks page once the server sends session origins (16.3,
 * D26): one primary table per view. The registry is used by the page, the
 * tests and the full-HD route list.
 */

import type { MessageKey } from '@/i18n/messages/en'

/** `label` names the view in tests and route lists; the tab shows `labelKey`. */
export const LOCKS_VIEWS = [
  { id: 'overview', label: 'Blocking', labelKey: 'locks.view.overview' },
  { id: 'application', label: 'Application sessions', labelKey: 'locks.view.application' },
  { id: 'pglens', label: 'pglens sessions', labelKey: 'locks.view.pglens' },
  { id: 'activity', label: 'Activity', labelKey: 'locks.view.activity' },
  { id: 'incidents', label: 'Captured incidents', labelKey: 'locks.view.incidents' },
] as const satisfies readonly { id: string; label: string; labelKey: MessageKey }[]

export type LocksViewId = (typeof LOCKS_VIEWS)[number]['id']

export const DEFAULT_LOCKS_VIEW: LocksViewId = 'overview'

/** The tab a `?view=` value names; absent or unknown values mean the blocking tree. */
export function readLocksView(value: string | null): LocksViewId {
  return LOCKS_VIEWS.find((view) => view.id === value)?.id ?? DEFAULT_LOCKS_VIEW
}
