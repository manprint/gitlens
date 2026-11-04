/**
 * The tabs of the Locks page once the server sends session origins (16.3,
 * D26): one primary table per view. The registry is used by the page, the
 * tests and the full-HD route list.
 */

export const LOCKS_VIEWS = [
  { id: 'overview', label: 'Blocking' },
  { id: 'application', label: 'Application sessions' },
  { id: 'pglens', label: 'pglens sessions' },
  { id: 'activity', label: 'Activity' },
] as const

export type LocksViewId = (typeof LOCKS_VIEWS)[number]['id']

export const DEFAULT_LOCKS_VIEW: LocksViewId = 'overview'

/** The tab a `?view=` value names; absent or unknown values mean the blocking tree. */
export function readLocksView(value: string | null): LocksViewId {
  return LOCKS_VIEWS.find((view) => view.id === value)?.id ?? DEFAULT_LOCKS_VIEW
}
