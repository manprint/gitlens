import { ALERT_LIST_LIMIT } from '@/lib/alerts'

interface AlertListTruncatedProps {
  truncated: boolean
}

/**
 * The server keeps the most recently evaluated alerts and cuts the rest. Any
 * count or list drawn from the alerts on the page is then a lower bound.
 */
export function AlertListTruncated({ truncated }: AlertListTruncatedProps) {
  if (!truncated) return null
  return (
    <div className="border-warning/40 bg-warning/10 text-sm" role="status">
      <strong>Only the {ALERT_LIST_LIMIT} most recently evaluated alerts are loaded.</strong>
      <p>
        Older alerts are not on this page, so any count or list built from alerts is a lower bound.
      </p>
    </div>
  )
}
