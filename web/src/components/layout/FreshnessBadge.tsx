import { Badge } from '@/components/ui/badge'
import { Stale } from '@/components/state'
import { REFRESH, type RefreshSurface } from '@/api/policy'
import { useFreshness } from '@/hooks/useFreshness'

interface FreshnessBadgeProps {
  dataUpdatedAt: number
  policy: RefreshSurface
}

export function FreshnessBadge({ dataUpdatedAt, policy }: FreshnessBadgeProps) {
  const { age, isStale, label } = useFreshness(dataUpdatedAt, policy)
  const { staleAfter } = REFRESH[policy]

  if (isStale) {
    return (
      <Stale age={Math.floor(age / 1_000)} threshold={Math.floor(staleAfter / 1_000)}>
        {label}
      </Stale>
    )
  }

  return (
    <Badge role="status" variant="secondary" aria-label={`Data freshness: ${label}`}>
      {label}
    </Badge>
  )
}
