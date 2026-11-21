import { useT } from '@/i18n/react'

interface TruncatedProps {
  shown: number
  budget: number
  scope: string
}

export function Truncated({ shown, budget, scope }: TruncatedProps) {
  const t = useT()
  return (
    <div role="status" className="text-muted-foreground text-sm">
      {t('shell.state.truncated', { shown, budget, scope })}
    </div>
  )
}
