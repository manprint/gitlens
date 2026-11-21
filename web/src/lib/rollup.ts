import type { AshDimension } from '@/lib/ash'

/** The `resolution` a response carries when it was answered from the hourly rollups. */
export const HOURLY_ROLLUP = '1h'

/** Whether a response was answered from the hourly rollups (resolution "1h"). */
export function isHourlyRollup(resolution: string | null | undefined): boolean {
  return resolution === HOURLY_ROLLUP
}

/** Session dimensions the ASH rollup does not keep; the server refuses them on a rollup range. */
export const ROLLUP_DROPPED_DIMENSIONS: readonly AshDimension[] = [
  'usename',
  'client_addr',
  'application_name',
]
