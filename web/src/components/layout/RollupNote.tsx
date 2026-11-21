import { useT } from '@/i18n/react'
import { isHourlyRollup } from '@/lib/rollup'

/** Says a chart or table shows hourly averages when its response came from the rollups. */
export function RollupNote({ resolution }: { resolution: string | null | undefined }) {
  const t = useT()
  if (!isHourlyRollup(resolution)) return null
  return (
    <p className="text-muted-foreground text-sm" data-testid="rollup-note" role="note">
      {t('shell.rollup.note')}
    </p>
  )
}
