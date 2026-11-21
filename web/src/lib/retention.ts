/**
 * Data retention (11.3, D17): the rules the Data retention tab applies before
 * it calls the server. The server keeps the same rules (`internal/server/
 * retention.go`): days between the family's `min_days` and 365, and a
 * shortening (or a retention for a family that has none) needs an explicit
 * confirmation because it deletes history at the next policy run.
 */

import type { ApiFailure } from '@/api/client'
import type { Schemas } from '@/api/types'
import { formatCount } from '@/lib/format'
import { localizedNumber } from '@/lib/format/shared'
import type { MessageKey } from '@/i18n/messages/en'
import { activeIntl, translate } from '@/i18n/store'

export type RetentionFamily = Schemas['RetentionFamily']
export type RetentionPreview = Schemas['RetentionPreview']
export type RetentionFamilyName = Schemas['RetentionFamilyName']

/** The message of what each family holds, in the words of the pages that show it. */
export const RETENTION_FAMILY_KEYS = {
  metrics: 'settings.lib.retention.family.metrics',
  metrics_statements: 'settings.lib.retention.family.metricsStatements',
  metrics_ash: 'settings.lib.retention.family.metricsAsh',
  metrics_replication: 'settings.lib.retention.family.metricsReplication',
  metrics_tables: 'settings.lib.retention.family.metricsTables',
  metrics_indexes: 'settings.lib.retention.family.metricsIndexes',
  metrics_bloat: 'settings.lib.retention.family.metricsBloat',
  log_events: 'settings.lib.retention.family.logEvents',
  plan_samples: 'settings.lib.retention.family.planSamples',
  metrics_1h: 'settings.lib.retention.family.metrics1h',
  metrics_statements_1h: 'settings.lib.retention.family.metricsStatements1h',
  metrics_ash_1h: 'settings.lib.retention.family.metricsAsh1h',
} as const satisfies Record<RetentionFamilyName, MessageKey>

/** What the family holds, in the console language. */
export function retentionFamilyLabel(family: RetentionFamilyName): string {
  return translate(RETENTION_FAMILY_KEYS[family])
}

export type DaysValidation = { ok: true; value: number } | { ok: false; error: string }

/** Reads the days typed for a family: a whole number between `min` and `max`. */
export function validateRetentionDays(text: string, min: number, max: number): DaysValidation {
  const value = text.trim()
  if (value === '') return { ok: false, error: translate('settings.lib.retention.days.empty') }
  if (!/^\d+$/.test(value)) {
    return { ok: false, error: translate('settings.lib.retention.days.whole') }
  }
  const days = Number(value)
  if (days < min || days > max) {
    return { ok: false, error: translate('settings.lib.retention.days.range', { min, max }) }
  }
  return { ok: true, value: days }
}

/** The families split into the raw-data group and the hourly-rollup group, order kept. */
export function splitRetentionGroups(
  families: readonly RetentionFamily[],
): [RetentionFamily[], RetentionFamily[]] {
  return [families.filter((f) => !f.rollup), families.filter((f) => f.rollup)]
}

/**
 * Whether `days` keeps less history than the live retention. A family without
 * a retention policy keeps everything, so any retention shortens it.
 */
export function isShortening(current: number | null, days: number): boolean {
  return current === null || days < current
}

const MIB = 1024 * 1024

const ONE_DECIMAL: Intl.NumberFormatOptions = {
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
  useGrouping: false,
}

/** A size in MiB for the confirmation text: one decimal under 10 MiB, whole MiB above. */
export function formatMiB(bytes: number, locale = activeIntl()): string {
  const mib = bytes / MIB
  if (bytes > 0 && mib < 0.1) return localizedNumber(0.1, locale, ONE_DECIMAL)
  return mib < 10 ? localizedNumber(mib, locale, ONE_DECIMAL) : formatCount(Math.round(mib), locale)
}

/** "N chunks", with the singular for one. */
export function chunkCount(chunks: number): string {
  return translate('settings.lib.retention.chunks', { count: formatCount(chunks), chunks })
}

/** The text of the destructive confirmation of a shortening (11.3 contract). */
export function confirmationText(preview: RetentionPreview): string {
  return translate('settings.lib.retention.confirm', {
    chunks: chunkCount(preview.chunks_to_drop),
    mib: formatMiB(preview.bytes_to_free),
    family: preview.family,
    days: preview.days,
  })
}

/** The one-line result of a preview, shown next to the Apply button. */
export function previewSummary(preview: RetentionPreview, current: number | null): string {
  if (!isShortening(current, preview.days)) {
    return translate('settings.lib.retention.preview.keeps')
  }
  if (preview.chunks_to_drop === 0) {
    return translate('settings.lib.retention.preview.none', { days: preview.days })
  }
  return translate('settings.lib.retention.preview.deletes', {
    chunks: chunkCount(preview.chunks_to_drop),
    mib: formatMiB(preview.bytes_to_free),
  })
}

function isApiFailure(error: unknown): error is ApiFailure {
  return (
    typeof error === 'object' && error !== null && 'kind' in error && typeof error.kind === 'string'
  )
}

/** The inline message of a failed preview or apply; the stored retention is unchanged. */
export function retentionErrorMessage(error: unknown): string {
  if (!isApiFailure(error)) return translate('settings.lib.retention.error.generic')
  switch (error.kind) {
    case 'server':
      if (error.status === 409) return translate('settings.lib.retention.error.conflict')
      if (error.status === 503) return translate('settings.lib.retention.error.unavailable')
      return error.detail ? `${error.error}: ${error.detail}` : error.error
    case 'unprocessable':
      return error.detail ? `${error.error}: ${error.detail}` : error.error
    case 'not_found':
      return translate('settings.lib.retention.error.unknownFamily')
    case 'unauthorized':
      return translate('settings.lib.retention.error.unauthorized')
    case 'forbidden':
      return translate('settings.lib.retention.error.refused')
    case 'network':
    case 'malformed':
    case 'client':
      return error.message
  }
}
