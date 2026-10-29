import { useId, type ReactNode } from 'react'

import type { ApiFailure } from '@/api/client'
import { EmptyState, ErrorState } from '@/components/state'
import { formatRangeLabel } from '@/lib/chartTime'
import { cn } from '@/lib/utils'

export type ChartCardState = 'ready' | 'loading' | 'empty' | 'error'

export interface ChartCardProps {
  title: string
  /** Extra muted text after the range line, e.g. `3 counter resets`. */
  subtitle?: string
  range: { from: Date; to: Date }
  stepSeconds: number
  /** Unit of the values, shown after the title (e.g. `per second`). */
  unitHint?: string
  state: ChartCardState
  error?: ApiFailure
  onRetry?: () => void
  /** Span both columns of a ChartGroup grid. */
  wide?: boolean
  /** Height of the loading placeholder; match the chart's `height` (default 300). */
  height?: number
  children?: ReactNode
}

const NO_FAILURE: ApiFailure = { kind: 'network', message: 'The chart data could not be loaded.' }

/** A titled chart with its range and step, and the shared loading, empty and error states. */
export function ChartCard({
  title,
  subtitle,
  range,
  stepSeconds,
  unitHint,
  state,
  error,
  onRetry,
  wide = false,
  height = 300,
  children,
}: ChartCardProps) {
  const titleId = `${useId().replaceAll(':', '')}-chart-title`

  return (
    <section
      aria-labelledby={titleId}
      className={cn('bg-card min-w-0 rounded-xl border p-4 shadow-sm', wide && 'lg:col-span-2')}
      data-chart-card=""
    >
      <h3 className="text-foreground text-sm font-semibold" id={titleId}>
        {title}
        {unitHint ? <span className="text-muted-foreground font-normal"> ({unitHint})</span> : null}
      </h3>
      <p className="text-muted-foreground mt-0.5 text-xs">
        {formatRangeLabel(range.from, range.to, stepSeconds)}
        {subtitle ? ` · ${subtitle}` : ''}
      </p>
      <div className="mt-3">
        {state === 'loading' ? (
          <div
            aria-busy="true"
            aria-label={`Loading ${title}`}
            className="bg-muted/40 animate-pulse rounded-lg"
            role="status"
            style={{ height }}
          />
        ) : state === 'empty' ? (
          <EmptyState description="No data in this range" title={`No ${title} data`} />
        ) : state === 'error' ? (
          <ErrorState
            endpoint={title}
            failure={error ?? NO_FAILURE}
            onRetry={onRetry ?? (() => undefined)}
          />
        ) : (
          children
        )}
      </div>
    </section>
  )
}
