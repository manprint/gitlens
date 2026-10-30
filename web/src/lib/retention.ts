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

export type RetentionFamily = Schemas['RetentionFamily']
export type RetentionPreview = Schemas['RetentionPreview']
export type RetentionFamilyName = Schemas['RetentionFamilyName']

/** What each family holds, in the words of the pages that show it. */
export const RETENTION_FAMILY_LABELS: Record<RetentionFamilyName, string> = {
  metrics: 'Instance and database metrics',
  metrics_statements: 'Statement statistics',
  metrics_ash: 'Active session history',
  metrics_replication: 'Replication lag and slots',
  metrics_tables: 'Table statistics',
  metrics_indexes: 'Index statistics',
  metrics_bloat: 'Bloat estimates',
}

export type DaysValidation = { ok: true; value: number } | { ok: false; error: string }

/** Reads the days typed for a family: a whole number between `min` and `max`. */
export function validateRetentionDays(text: string, min: number, max: number): DaysValidation {
  const value = text.trim()
  if (value === '') return { ok: false, error: 'Enter a number of days.' }
  if (!/^\d+$/.test(value)) return { ok: false, error: 'Enter a whole number of days.' }
  const days = Number(value)
  if (days < min || days > max) {
    return { ok: false, error: `Enter between ${min} and ${max} days.` }
  }
  return { ok: true, value: days }
}

/**
 * Whether `days` keeps less history than the live retention. A family without
 * a retention policy keeps everything, so any retention shortens it.
 */
export function isShortening(current: number | null, days: number): boolean {
  return current === null || days < current
}

const MIB = 1024 * 1024

/** A size in MiB for the confirmation text: one decimal under 10 MiB, whole MiB above. */
export function formatMiB(bytes: number): string {
  const mib = bytes / MIB
  if (bytes > 0 && mib < 0.1) return '0.1'
  return mib < 10 ? mib.toFixed(1) : formatCount(Math.round(mib))
}

/** "N chunks", with the singular for one. */
export function chunkCount(chunks: number): string {
  return `${formatCount(chunks)} ${chunks === 1 ? 'chunk' : 'chunks'}`
}

/** The text of the destructive confirmation of a shortening (11.3 contract). */
export function confirmationText(preview: RetentionPreview): string {
  return (
    `This will permanently delete ${chunkCount(preview.chunks_to_drop)} ` +
    `(~${formatMiB(preview.bytes_to_free)} MiB) of ${preview.family} data older than ` +
    `${preview.days} days at the next policy run. Take a backup first.`
  )
}

/** The one-line result of a preview, shown next to the Apply button. */
export function previewSummary(preview: RetentionPreview, current: number | null): string {
  if (!isShortening(current, preview.days)) {
    return `Keeps more history: nothing is deleted.`
  }
  if (preview.chunks_to_drop === 0) {
    return `Nothing older than ${preview.days} days is stored now.`
  }
  return `Deletes ${chunkCount(preview.chunks_to_drop)} (~${formatMiB(preview.bytes_to_free)} MiB) at the next policy run.`
}

function isApiFailure(error: unknown): error is ApiFailure {
  return (
    typeof error === 'object' && error !== null && 'kind' in error && typeof error.kind === 'string'
  )
}

/** The inline message of a failed preview or apply; the stored retention is unchanged. */
export function retentionErrorMessage(error: unknown): string {
  if (!isApiFailure(error)) return 'The retention was not changed. Try again.'
  switch (error.kind) {
    case 'server':
      if (error.status === 409) {
        return 'The retention changed or needs confirmation. Preview again, then apply.'
      }
      if (error.status === 503) return 'The store is unavailable. Try again later.'
      return error.detail ? `${error.error}: ${error.detail}` : error.error
    case 'unprocessable':
      return error.detail ? `${error.error}: ${error.detail}` : error.error
    case 'not_found':
      return 'The server does not know this family. Reload the page.'
    case 'unauthorized':
      return 'Your session has expired. Sign in again to change the retention.'
    case 'forbidden':
      return 'The server refused the change.'
    case 'network':
    case 'malformed':
    case 'client':
      return error.message
  }
}
