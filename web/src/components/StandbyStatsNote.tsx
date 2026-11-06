import { isStandby, STANDBY_STATS_NOTE } from '@/lib/standby'

/** One line on a standby's statistics pages; nothing for any other role. */
export function StandbyStatsNote({ role }: { role: string | null | undefined }) {
  if (!isStandby(role)) return null
  return (
    <p
      className="bg-muted/40 truncate rounded-lg border px-4 py-2 text-sm min-[1600px]:px-3 min-[1600px]:py-0.5 min-[1600px]:text-xs"
      data-testid="standby-stats-note"
      role="note"
      title={STANDBY_STATS_NOTE}
    >
      {STANDBY_STATS_NOTE}
    </p>
  )
}
