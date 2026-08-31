import type { ReactNode } from 'react'

import { formatDuration } from '@/lib/format'

interface StaleProps {
  age: number
  threshold: number
  children: ReactNode
}

export function Stale({ age, threshold, children }: StaleProps) {
  return (
    <span role="status" aria-label={`Stale data: ${formatDuration(age)} old`}>
      <span>{children}</span>{' '}
      <span title="Data is older than the freshness threshold">
        Stale — {formatDuration(age)} old (threshold {formatDuration(threshold)})
      </span>
    </span>
  )
}
