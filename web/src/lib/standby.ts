/** True only for an instance whose role is `standby`. */
export function isStandby(role: string | null | undefined): boolean {
  return role === 'standby'
}

/**
 * Which statistics a standby keeps itself (V-10): the pg_stat_* activity
 * counters are per server, while sizes, definitions, pg_class row estimates
 * and pg_stats arrive from the primary by WAL.
 */
export const STANDBY_STATS_NOTE =
  "Activity statistics are this standby's own: PostgreSQL counts scans, row changes, dead rows and vacuums on each server, so they do not describe the primary or the other standbys. Sizes, definitions and row estimates match the primary."
