import type { ReactNode } from 'react'

import type { CaptionId } from '@/lib/captions'
import { formatUnit, type TileUnit } from '@/lib/units'
import { cn } from '@/lib/utils'

import { Unknown } from '../state/Unknown'
import { InfoHint } from './InfoHint'

export type TileTone = 'ok' | 'warn' | 'crit' | 'neutral'

const TONE_DOT: Record<TileTone, string> = {
  ok: 'bg-success',
  warn: 'bg-warning',
  crit: 'bg-destructive',
  neutral: 'bg-muted-foreground',
}

interface MetricTileProps {
  label: string
  value: ReactNode
  /** One of the closed list (D31); anything else goes in `detail`. */
  unit?: TileUnit | undefined
  sparkline?: ReactNode
  /** Extra classes, such as a tighter padding where tiles share a header. */
  className?: string
  /** The tile's caption: the ⓘ in its corner explains the figure (D30, D31). */
  captionId?: CaptionId
  /** A sentence that gives each secondary number its noun, on its own line (D31). */
  detail?: ReactNode
  /** A state shown as text beside its colour, never as colour alone. */
  status?: { tone: TileTone; text: string }
}

export function MetricTile({
  label,
  value,
  unit,
  sparkline,
  className,
  captionId,
  detail,
  status,
}: MetricTileProps) {
  const hasDetail = detail !== undefined && detail !== null && detail !== false && detail !== ''
  return (
    <article
      aria-label={label}
      data-card=""
      {...(captionId === undefined ? {} : { 'data-caption-id': captionId })}
      className={cn(
        'bg-card shadow-card hover:border-primary/30 relative rounded-xl border p-4 transition-colors min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:justify-between min-[1600px]:gap-x-3 min-[1600px]:px-4 min-[1600px]:py-3',
        captionId !== undefined && 'pr-9 min-[1600px]:pr-9',
        // The status and detail lines take a line of their own beside the one-line figure.
        (hasDetail || status !== undefined) && 'min-[1600px]:flex-wrap',
        className,
      )}
    >
      <p className="text-muted-foreground text-xs font-medium tracking-wide uppercase">{label}</p>
      <div className="mt-2 flex items-baseline gap-1.5 min-[1600px]:mt-0">
        <span className="text-foreground text-2xl font-semibold tracking-tight tabular-nums min-[1600px]:text-xl">
          {value ?? <Unknown />}
        </span>
        {unit ? (
          <span className="text-muted-foreground text-sm">{formatUnit(unit, value)}</span>
        ) : null}
      </div>
      {status ? (
        <p
          className="mt-1 flex items-center gap-1.5 text-xs font-medium min-[1600px]:mt-0 min-[1600px]:w-full"
          data-tone={status.tone}
        >
          <span aria-hidden="true" className={cn('size-2 rounded-full', TONE_DOT[status.tone])} />
          {status.text}
        </p>
      ) : null}
      {hasDetail ? (
        <p className="text-muted-foreground mt-1 text-xs min-[1600px]:mt-0 min-[1600px]:w-full">
          {detail}
        </p>
      ) : null}
      {sparkline ? <div className="mt-3">{sparkline}</div> : null}
      {captionId !== undefined ? (
        <InfoHint className="absolute top-2 right-2" id={captionId} label={label} />
      ) : null}
    </article>
  )
}
