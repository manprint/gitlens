import type { Schemas } from '@/api/types'
import { formatBytes } from '@/lib/format'
import { localizedNumber } from '@/lib/format/shared'
import { activeIntl } from '@/i18n/store'

export type HostProcess = Schemas['HostProcess']
export type HostProcessGroup = Schemas['HostProcessGroup']

/** The group selector: `none` lists the processes of the newest sample. */
export const PROCESS_GROUPINGS = [
  'none',
  'backend_type',
  'datname',
  'usename',
  'application_name',
] as const
export type ProcessGrouping = (typeof PROCESS_GROUPINGS)[number]
export type ProcessGroupBy = Exclude<ProcessGrouping, 'none'>

/** Rows the snapshot asks for; the table sorts them in the browser. */
export const PROCESS_SNAPSHOT_LIMIT = 200

export function isProcessGrouping(value: string): value is ProcessGrouping {
  return (PROCESS_GROUPINGS as readonly string[]).includes(value)
}

/** CPU as percent of one core: 1.0 is 100 %, two busy cores 200 %. */
export function formatCoreShare(ratio: number, intl = activeIntl()): string {
  return localizedNumber(ratio, intl, {
    style: 'percent',
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  })
}

/** Bytes per second, or null when the agent could not read the counters. */
export function formatByteRate(value: number | null, intl = activeIntl()): string | null {
  if (value === null || !Number.isFinite(value)) return null
  return `${formatBytes(value, intl)}/s`
}

/** No sample near the requested time: the agent ships no process facts. */
export function snapshotIsEmpty(data: Schemas['HostProcessesResponse'] | undefined): boolean {
  return data !== undefined && (data.ts === null || data.processes.length === 0)
}

export function processKey(row: HostProcess): string {
  return String(row.pid)
}
