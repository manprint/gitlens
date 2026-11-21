/**
 * Advisor rules and observation windows (11.4, D18, D22c): the rules the
 * Advisor rules tab applies before it calls the server. The server keeps the
 * same rules (`internal/server/advisor_settings.go`): a window is a whole
 * number of seconds between 3600 (1 hour) and 7776000 (90 days), and only the
 * two window keys exist.
 */

import type { ApiFailure } from '@/api/client'
import type { Schemas } from '@/api/types'
import { REFERENCE_LOCALE } from '@/i18n/locales'
import type { MessageKey } from '@/i18n/messages/en'
import { translate } from '@/i18n/store'

export type AdvisorRule = Schemas['AdvisorRule']
export type AdvisorWindow = Schemas['AdvisorWindow']
export type AdvisorWindowKey = Schemas['AdvisorWindowKey']

/** The category of a rule: the part of its id before the first dot (`index.unused` → `index`). */
export function ruleCategory(id: string): string {
  const dot = id.indexOf('.')
  return dot > 0 ? id.slice(0, dot) : id
}

const WINDOW_LABEL_KEYS = {
  'window.unused_index': 'settings.lib.advisor.window.label.unusedIndex',
  'window.backup_history': 'settings.lib.advisor.window.label.backupHistory',
} as const satisfies Record<AdvisorWindowKey, MessageKey>

const WINDOW_HELP_KEYS = {
  'window.unused_index': 'settings.lib.advisor.window.help.unusedIndex',
  'window.backup_history': 'settings.lib.advisor.window.help.backupHistory',
} as const satisfies Record<AdvisorWindowKey, MessageKey>

/** What the window looks back over, in the words of the checks that use it. */
export function windowLabel(key: AdvisorWindowKey): string {
  return translate(WINDOW_LABEL_KEYS[key])
}

export function windowHelp(key: AdvisorWindowKey): string {
  return translate(WINDOW_HELP_KEYS[key])
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

/** A window in the unit that reads it exactly: "7 days", "36 hours", "1 hour". */
export function windowText(seconds: number, locale?: string): string {
  const draft = windowDraft(seconds)
  const n = Number(draft.value)
  if (draft.unit === 'days')
    return translate('settings.lib.advisor.window.days', { days: n }, locale)
  if (seconds % WINDOW_UNIT_SECONDS.hours !== 0) {
    return translate('settings.lib.advisor.window.seconds', { seconds }, locale)
  }
  return translate('settings.lib.advisor.window.hours', { hours: n }, locale)
}

/** The English form of a window, for the index explanation that is not translated yet. */
export function formatWindow(seconds: number): string {
  return windowText(seconds, REFERENCE_LOCALE)
}

export type WindowValidation = { ok: true; seconds: number } | { ok: false; error: string }

/** Reads a typed window: a whole number of hours or days within the window's bounds. */
export function validateWindow(
  text: string,
  unit: WindowUnit,
  window: Pick<AdvisorWindow, 'min_seconds' | 'max_seconds'>,
): WindowValidation {
  const value = text.trim()
  const outOfRange = {
    min: windowText(window.min_seconds),
    max: windowText(window.max_seconds),
  }
  if (value === '') {
    return { ok: false, error: translate('settings.lib.advisor.window.outOfRange', outOfRange) }
  }
  if (!/^\d+$/.test(value)) {
    return { ok: false, error: translate('settings.lib.advisor.window.wholeNumber', { unit }) }
  }
  const seconds = Number(value) * WINDOW_UNIT_SECONDS[unit]
  if (seconds < window.min_seconds || seconds > window.max_seconds) {
    return { ok: false, error: translate('settings.lib.advisor.window.outOfRange', outOfRange) }
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
  if (!isApiFailure(error)) return translate('settings.lib.advisor.error.generic', { subject })
  switch (error.kind) {
    case 'server':
      if (error.status === 503) {
        return translate('settings.lib.advisor.error.unavailable', { subject })
      }
      return translate('settings.lib.advisor.error.failed', {
        subject,
        reason: error.detail ? `${error.error}: ${error.detail}` : error.error,
      })
    case 'unprocessable':
      return translate('settings.lib.advisor.error.failed', {
        subject,
        reason: error.detail ? `${error.error}: ${error.detail}` : error.error,
      })
    case 'not_found':
      return translate('settings.lib.advisor.error.notFound', { subject })
    case 'unauthorized':
      return translate('settings.lib.advisor.error.unauthorized')
    case 'forbidden':
      return translate('settings.lib.advisor.error.refused', { subject })
    case 'network':
    case 'malformed':
    case 'client':
      return translate('settings.lib.advisor.error.failed', { subject, reason: error.message })
  }
}
