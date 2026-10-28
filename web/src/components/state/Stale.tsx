import type { ReactNode } from 'react'

import { formatDuration } from '@/lib/format'

interface StaleProps {
  age: number
  threshold: number
  children: ReactNode
}

export function Stale({ age, threshold, children }: StaleProps) {
  return (
    <span
      role="status"
      aria-label={`Stale data: ${formatDuration(age)} old`}
      className="border-warning/40 bg-warning/10 text-warning-foreground inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium"
    >
      <span aria-hidden="true" className="bg-warning size-1.5 rounded-full" />
      <span>{children}</span>{' '}
      <span className="opacity-80" title="Data is older than the freshness threshold">
        Stale — {formatDuration(age)} old (threshold {formatDuration(threshold)})
      </span>
    </span>
  )
}
