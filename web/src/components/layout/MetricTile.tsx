import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

import { Unknown } from '../state/Unknown'

interface MetricTileProps {
  label: string
  value: ReactNode
  unit?: ReactNode
  sparkline?: ReactNode
  /** Extra classes, such as a tighter padding where tiles share a header. */
  className?: string
}

export function MetricTile({ label, value, unit, sparkline, className }: MetricTileProps) {
  return (
    <article
      aria-label={label}
      className={cn(
        'bg-card shadow-card hover:border-primary/30 rounded-xl border p-4 transition-colors min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:justify-between min-[1600px]:gap-3 min-[1600px]:px-4 min-[1600px]:py-3',
        className,
      )}
    >
      <p className="text-muted-foreground text-xs font-medium tracking-wide uppercase">{label}</p>
      <div className="mt-2 flex items-baseline gap-1.5 min-[1600px]:mt-0">
        <span className="text-foreground text-2xl font-semibold tracking-tight tabular-nums min-[1600px]:text-xl">
          {value ?? <Unknown />}
        </span>
        {unit ? <span className="text-muted-foreground text-sm">{unit}</span> : null}
      </div>
      {sparkline ? <div className="mt-3">{sparkline}</div> : null}
    </article>
  )
}
