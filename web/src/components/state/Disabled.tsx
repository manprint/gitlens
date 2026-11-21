import { useT } from '@/i18n/react'

interface DisabledProps {
  feature: string
  configKey: string
}

export function Disabled({ feature, configKey }: DisabledProps) {
  const t = useT()
  return (
    <div
      role="status"
      className="bg-muted/50 text-muted-foreground rounded-lg border px-3 py-2 text-sm"
    >
      <strong className="text-foreground">{t('shell.state.disabled', { feature })}</strong>{' '}
      {t('shell.state.disabledKey', { key: configKey })}
    </div>
  )
}
