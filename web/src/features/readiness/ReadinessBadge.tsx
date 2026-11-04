import { Badge } from '@/components/ui/badge'
import { readinessBadgeLabel, readinessBadgeText } from '@/lib/readiness'

/** "Full" or "Partial · N warnings", with a full accessible name (RDY-UI-006). */
export function ReadinessBadge({
  infos = 0,
  level,
  warnings,
}: {
  infos?: number
  level: 'full' | 'partial'
  warnings: number
}) {
  return (
    <Badge
      aria-label={readinessBadgeLabel(level, warnings, infos)}
      className="shrink-0 whitespace-nowrap"
      data-caption-id="fleet.readiness"
      data-testid="readiness-badge"
      role="status"
      variant={level === 'full' ? 'success' : 'warning'}
    >
      {readinessBadgeText(level, warnings)}
    </Badge>
  )
}
