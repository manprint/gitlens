/**
 * Advisor rules and observation windows (11.4, D18, D22c): the rules the
 * Advisor rules tab applies before it calls the server. The server keeps the
 * same rules (`internal/server/advisor_settings.go`): a window is a whole
 * number of seconds between 3600 (1 hour) and 7776000 (90 days), and only the
 * two window keys exist.
 */

import type { ApiFailure } from '@/api/client'
import type { Schemas } from '@/api/types'

export type AdvisorRule = Schemas['AdvisorRule']
export type AdvisorWindow = Schemas['AdvisorWindow']
export type AdvisorWindowKey = Schemas['AdvisorWindowKey']

/** The category of a rule: the part of its id before the first dot (`index.unused` → `index`). */
export function ruleCategory(id: string): string {
  const dot = id.indexOf('.')
  return dot > 0 ? id.slice(0, dot) : id
}

/** What each window looks back over, in the words of the checks that use it. */
export const WINDOW_LABELS: Record<AdvisorWindowKey, string> = {
  'window.unused_index': 'Unused-index history',
  'window.backup_history': 'Backup history',
}

export const WINDOW_HELP: Record<AdvisorWindowKey, string> = {
  'window.unused_index':
    'How long an index must go without a scan, in the instance history or since the statistics reset, before the unused-index check reports it.',
  'window.backup_history':
    'How long an instance must have been monitored before the backup checks read "no base backup seen" as a finding rather than as not enough history.',
}

export type WindowUnit = 'hours' | 'days'

export const WINDOW_UNIT_SECONDS: Record<WindowUnit, number> = { hours: 3600, days: 86400 }

/**
 * A window as the form shows it: whole days when it is a number of days, else
 * hours. A stored value that is not a whole number of hours (5400 s) shows as
 * a decimal (1.5) and is never rounded: the form only rewrites a typed value.
 */
export function windowDraft(seconds: number): { value: string; unit: WindowUnit } {
  if (seconds % WINDOW_UNIT_SECONDS.days === 0) {
    return { value: String(seconds / WINDOW_UNIT_SECONDS.days), unit: 'days' }
  }
  if (seconds % WINDOW_UNIT_SECONDS.hours === 0) {
    return { value: String(seconds / WINDOW_UNIT_SECONDS.hours), unit: 'hours' }
  }
  return {
    value: String(Math.round((seconds / WINDOW_UNIT_SECONDS.hours) * 100) / 100),
    unit: 'hours',
  }
}

/** "7 days", "36 hours", "1 hour": a window in the unit that reads it exactly. */
export function formatWindow(seconds: number): string {
  const draft = windowDraft(seconds)
  const n = Number(draft.value)
  if (draft.unit === 'days') return `${n} ${n === 1 ? 'day' : 'days'}`
  if (seconds % WINDOW_UNIT_SECONDS.hours !== 0) return `${seconds} seconds`
  return `${n} ${n === 1 ? 'hour' : 'hours'}`
}

export type WindowValidation = { ok: true; seconds: number } | { ok: false; error: string }

/** Reads a typed window: a whole number of hours or days within the window's bounds. */
export function validateWindow(
  text: string,
  unit: WindowUnit,
  window: Pick<AdvisorWindow, 'min_seconds' | 'max_seconds'>,
): WindowValidation {
  const value = text.trim()
  const range = `between ${formatWindow(window.min_seconds)} and ${formatWindow(window.max_seconds)}`
  if (value === '') return { ok: false, error: `Enter a window ${range}.` }
  if (!/^\d+$/.test(value)) return { ok: false, error: `Enter a whole number of ${unit}.` }
  const seconds = Number(value) * WINDOW_UNIT_SECONDS[unit]
  if (seconds < window.min_seconds || seconds > window.max_seconds) {
    return { ok: false, error: `Enter a window ${range}.` }
  }
  return { ok: true, seconds }
}

/** Whether a window is shorter than its default, the case the false-positive warning is about. */
export function isShortWindow(seconds: number, window: Pick<AdvisorWindow, 'default_seconds'>) {
  return seconds < window.default_seconds
}

function isApiFailure(error: unknown): error is ApiFailure {
  return (
    typeof error === 'object' && error !== null && 'kind' in error && typeof error.kind === 'string'
  )
}

/** The inline message of a failed write; the stored value is unchanged (I-11). */
export function advisorErrorMessage(error: unknown, subject: string): string {
  if (!isApiFailure(error)) return `${subject} was not changed. Try again.`
  switch (error.kind) {
    case 'server':
      if (error.status === 503) return `${subject} was not changed: the store is unavailable.`
      return `${subject} was not changed: ${error.detail ? `${error.error}: ${error.detail}` : error.error}`
    case 'unprocessable':
      return `${subject} was not changed: ${error.detail ? `${error.error}: ${error.detail}` : error.error}`
    case 'not_found':
      return `${subject} was not changed: the server does not know it. Reload the page.`
    case 'unauthorized':
      return 'Your session has expired. Sign in again to change advisor settings.'
    case 'forbidden':
      return `${subject} was not changed: the server refused the change.`
    case 'network':
    case 'malformed':
    case 'client':
      return `${subject} was not changed: ${error.message}`
  }
}
