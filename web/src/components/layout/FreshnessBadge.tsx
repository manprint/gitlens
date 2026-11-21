import { RefreshCw } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Stale } from '@/components/state'
import type { RefreshSurface } from '@/api/policy'
import { useFreshness } from '@/hooks/useFreshness'
import { useT } from '@/i18n/react'

interface FreshnessBadgeProps {
  dataUpdatedAt: number
  policy: RefreshSurface
}

export function FreshnessBadge({ dataUpdatedAt, policy }: FreshnessBadgeProps) {
  const t = useT()
  const { age, ageText, isStale, label, staleAfter } = useFreshness(dataUpdatedAt, policy)

  if (isStale) {
    return (
      <Stale age={Math.floor(age / 1_000)} threshold={Math.floor(staleAfter / 1_000)}>
        {label}
      </Stale>
    )
  }

  return (
    <Badge
      role="status"
      variant="outline"
      aria-label={t('shell.fresh.label', { label })}
      className="text-muted-foreground bg-card font-medium"
    >
      <RefreshCw aria-hidden="true" />
      {ageText ? (
        // A fixed-width age slot: the badge keeps its width from 0s to 99s, so the
        // ticking label does not move what sits beside it.
        <span>
          {t('shell.fresh.updatedBefore')}{' '}
          <span className="inline-block min-w-[3ch] text-end tabular-nums">{ageText}</span>{' '}
          {t('shell.fresh.updatedAfter')}
        </span>
      ) : (
        label
      )}
    </Badge>
  )
}
