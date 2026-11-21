import type { ReactNode } from 'react'

import { formatDuration } from '@/lib/format'
import { useT } from '@/i18n/react'

interface StaleProps {
  age: number
  threshold: number
  children: ReactNode
}

export function Stale({ age, threshold, children }: StaleProps) {
  const t = useT()
  return (
    <span
      role="status"
      aria-label={t('shell.state.staleLabel', { age: formatDuration(age) })}
      className="border-warning/40 bg-warning/10 text-warning-foreground inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium"
    >
      <span aria-hidden="true" className="bg-warning size-1.5 rounded-full" />
      <span>{children}</span>{' '}
      <span className="opacity-80" title={t('shell.state.staleTitle')}>
        {t('shell.state.stale', {
          age: formatDuration(age),
          threshold: formatDuration(threshold),
        })}
      </span>
    </span>
  )
}
