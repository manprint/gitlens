import { ALERT_LIST_LIMIT } from '@/lib/alerts'
import { Callout } from '@/components/ui/callout'
import { useT } from '@/i18n/react'

interface AlertListTruncatedProps {
  truncated: boolean
}

/**
 * The server keeps the most recently evaluated alerts and cuts the rest. Any
 * count or list drawn from the alerts on the page is then a lower bound.
 */
export function AlertListTruncated({ truncated }: AlertListTruncatedProps) {
  const t = useT()
  if (!truncated) return null
  return (
    <Callout role="status" variant="warning">
      <strong>{t('shell.state.alertsCapped', { limit: ALERT_LIST_LIMIT })}</strong>
      <p>{t('shell.state.alertsCappedText')}</p>
    </Callout>
  )
}
