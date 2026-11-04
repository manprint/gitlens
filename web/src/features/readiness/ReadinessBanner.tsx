import { Link } from 'react-router-dom'

import { useReadiness } from '@/api/queries'
import { Callout } from '@/components/ui/callout'
import { readinessBadgeText, readinessLevel } from '@/lib/readiness'

/**
 * The instance dashboards' readiness chip (14.2): hidden while the server is
 * monitored in full, else "Partial · N warnings" linking to the Readiness tab.
 * A chip-link that never truncates, so it fits the header row from 1600 px
 * beside the coverage banner (R-13.3b).
 */
export function ReadinessBanner({
  compact = false,
  instanceId,
}: {
  compact?: boolean
  instanceId: string
}) {
  const readiness = useReadiness(instanceId)
  const report = readiness.data
  if (report === undefined || readinessLevel(report) === 'full') return null
  const text = readinessBadgeText('partial', report.counts.warning)
  return (
    <Callout
      aria-live="polite"
      className={
        compact
          ? 'flex w-auto shrink-0 items-center gap-1.5 px-2.5 py-1'
          : 'flex items-center gap-2 py-2 min-[1600px]:py-1.5'
      }
      data-caption-id="instance.readiness"
      data-testid="readiness-banner"
      role="status"
      variant="warning"
    >
      <Link
        aria-label={`${text}. Open the readiness tab.`}
        className="font-medium whitespace-nowrap underline"
        to={`/instances/${encodeURIComponent(instanceId)}?section=readiness`}
      >
        {text}
      </Link>
      {compact ? null : (
        <span className="text-muted-foreground">
          Some features have less data than they could. See what is missing and how to fix it.
        </span>
      )}
    </Callout>
  )
}
