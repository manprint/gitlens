import { useT } from '@/i18n/react'

interface UnknownProps {
  reason?: string
}

export function Unknown({ reason }: UnknownProps) {
  const t = useT()
  return (
    <span
      aria-label={t('shell.state.notMeasured')}
      title={reason ?? t('shell.state.notMeasuredReason')}
    >
      —
    </span>
  )
}
