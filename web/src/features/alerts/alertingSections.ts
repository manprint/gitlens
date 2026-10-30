/**
 * The tabs of the alerting pages (15.3, D26): one view per tab so each holds
 * one primary table. `?section=` names the tab; the default tab is never
 * written to the URL and an unknown value means the default.
 */

export const ALERTS_SECTIONS = [
  { id: 'alerts', label: 'Alerts' },
  { id: 'delivery', label: 'Notification delivery' },
] as const

export type AlertsSectionId = (typeof ALERTS_SECTIONS)[number]['id']

export const DEFAULT_ALERTS_SECTION: AlertsSectionId = 'alerts'

export const EVENTS_SECTIONS = [
  { id: 'events', label: 'Events' },
  { id: 'alerts', label: 'Alert status' },
] as const

export type EventsSectionId = (typeof EVENTS_SECTIONS)[number]['id']

export const DEFAULT_EVENTS_SECTION: EventsSectionId = 'events'

/** The tab of the alerts page a `?section=` value names; absent or unknown values mean Alerts. */
export function readAlertsSection(value: string | null): AlertsSectionId {
  return ALERTS_SECTIONS.find((section) => section.id === value)?.id ?? DEFAULT_ALERTS_SECTION
}

/** The tab of the events page a `?section=` value names; absent or unknown values mean Events. */
export function readEventsSection(value: string | null): EventsSectionId {
  return EVENTS_SECTIONS.find((section) => section.id === value)?.id ?? DEFAULT_EVENTS_SECTION
}

/**
 * The search string with `section` set to `next`, or removed for the default
 * tab, keeping every other parameter (filters, table state).
 */
export function withSection(current: URLSearchParams, next: string, fallback: string) {
  const params = new URLSearchParams(current)
  if (next === fallback) params.delete('section')
  else params.set('section', next)
  return params
}
