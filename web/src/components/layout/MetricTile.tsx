import type { ReactNode } from 'react'

import { Unknown } from '../state/Unknown'

interface MetricTileProps {
  label: string
  value: number | null
  unit?: ReactNode
  sparkline?: ReactNode
}

export function MetricTile({ label, value, unit, sparkline }: MetricTileProps) {
  return (
    <article aria-label={label} className="bg-card rounded-lg border p-4 shadow-sm">
      <p className="text-muted-foreground text-sm">{label}</p>
      <div className="mt-2 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tabular-nums">{value ?? <Unknown />}</span>
        {unit ? <span className="text-muted-foreground text-sm">{unit}</span> : null}
      </div>
      {sparkline ? <div className="mt-3">{sparkline}</div> : null}
    </article>
  )
}
