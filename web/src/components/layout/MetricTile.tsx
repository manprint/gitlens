import type { ReactNode } from 'react'

import { Unknown } from '../state/Unknown'

interface MetricTileProps {
  label: string
  value: ReactNode
  unit?: ReactNode
  sparkline?: ReactNode
}

export function MetricTile({ label, value, unit, sparkline }: MetricTileProps) {
  return (
    <article
      aria-label={label}
      className="bg-card shadow-card hover:border-primary/30 rounded-xl border p-4 transition-colors"
    >
      <p className="text-muted-foreground text-xs font-medium tracking-wide uppercase">{label}</p>
      <div className="mt-2 flex items-baseline gap-1.5">
        <span className="text-foreground text-2xl font-semibold tracking-tight tabular-nums">
          {value ?? <Unknown />}
        </span>
        {unit ? <span className="text-muted-foreground text-sm">{unit}</span> : null}
      </div>
      {sparkline ? <div className="mt-3">{sparkline}</div> : null}
    </article>
  )
}
