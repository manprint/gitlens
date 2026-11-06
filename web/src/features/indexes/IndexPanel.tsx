import type { ReactNode } from 'react'

import { InfoHint } from '@/components/layout/InfoHint'
import type { CaptionId } from '@/lib/captions'
import { formatUnit, type TileUnit } from '@/lib/units'
import { cn } from '@/lib/utils'

/**
 * The layout of an Index analysis tab (D26): one card whose header is one
 * row from 1600 px (it wraps below 1920 px), above the tab's single table. It holds the
 * title, the ⓘ, the caption sentence (up to two lines, whole in its `title`),
 * the status notes, which wrap instead of being cut, the figures and the
 * actions. Below 1600 px the header stacks.
 */

/** A badge inside a table cell: no vertical padding from 1600 px, so the row keeps its height. */
export const CELL_BADGE = 'min-[1600px]:py-0'

export interface PanelStat {
  label: string
  value: string
  /** Shown beside the value at every width: one unit of the closed list (D31). */
  unit?: TileUnit
  /** Shown below 1600 px only; from 1600 px it is in the figure's `title`. */
  detail?: string
}

/** The tab's figures: tiles below 1600 px, one line of label and value from 1600 px. */
function StatStrip({ stats }: { stats: readonly PanelStat[] }) {
  return (
    <div
      className={`grid gap-4 min-[1600px]:shrink-0 min-[1600px]:auto-cols-auto min-[1600px]:grid-flow-col ${stats.length <= 4 ? 'min-[1600px]:grid-rows-4' : 'min-[1600px]:grid-rows-3'} min-[1600px]:gap-x-4 min-[1600px]:gap-y-0.5 sm:max-[1599px]:grid-cols-2 xl:max-[1599px]:grid-cols-4`}
    >
      {stats.map((stat) => (
        <article
          key={stat.label}
          aria-label={stat.label}
          className="bg-card shadow-card rounded-xl border p-4 min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:rounded-none min-[1600px]:border-0 min-[1600px]:bg-transparent min-[1600px]:p-0 min-[1600px]:whitespace-nowrap min-[1600px]:shadow-none"
          title={[
            `${stat.label}:`,
            stat.value,
            stat.unit === undefined ? undefined : formatUnit(stat.unit),
            stat.detail,
          ]
            .filter((part) => part !== undefined && part !== '')
            .join(' ')}
        >
          <p className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
            {stat.label}
          </p>
          <div className="mt-2 flex items-baseline gap-1.5 min-[1600px]:mt-0">
            <span className="text-foreground text-2xl font-semibold tracking-tight tabular-nums min-[1600px]:text-sm min-[1600px]:leading-4">
              {stat.value}
            </span>
            {stat.unit ? (
              <span className="text-muted-foreground text-sm min-[1600px]:text-xs">
                {formatUnit(stat.unit)}
              </span>
            ) : null}
            {stat.detail ? (
              <span className="text-muted-foreground text-sm min-[1600px]:hidden">
                {stat.detail}
              </span>
            ) : null}
          </div>
        </article>
      ))}
    </div>
  )
}

/**
 * A status note: a box that wraps its whole text, smaller from 1600 px; the
 * text is also its `title`.
 */
export function PanelNote({
  title,
  tone = 'muted',
  children,
}: {
  /** The full text of the note, also shown on hover. */
  title: string
  tone?: 'muted' | 'warning'
  children: ReactNode
}) {
  return (
    <p
      className={cn(
        'rounded-lg border px-4 py-3 text-sm',
        tone === 'warning' ? 'border-warning/40 bg-warning/10' : 'bg-muted/40',
        'min-[1600px]:min-w-0 min-[1600px]:flex-1 min-[1600px]:px-3 min-[1600px]:py-1 min-[1600px]:text-xs min-[1600px]:leading-snug',
      )}
      role="status"
      title={title}
    >
      {children}
    </p>
  )
}

interface IndexPanelProps {
  /** The id of the title, which names the section. */
  titleId: string
  title: string
  /** The panel's caption (D30, I-16): an ⓘ beside the title. */
  captionId?: CaptionId
  /** The caption sentence: whole, up to two lines from 1600 px (the full text is in its `title`). */
  description: string
  stats?: readonly PanelStat[]
  /** `PanelNote` elements. */
  notes?: ReactNode
  /** Controls at the end of the header line. */
  actions?: ReactNode
  children: ReactNode
}

export function IndexPanel({
  titleId,
  title,
  captionId,
  description,
  stats,
  notes,
  actions,
  children,
}: IndexPanelProps) {
  return (
    <section
      aria-labelledby={titleId}
      data-card=""
      {...(captionId === undefined ? {} : { 'data-caption-id': captionId })}
      className="bg-card shadow-card overflow-hidden rounded-xl border"
    >
      <div
        className="flex flex-col gap-4 border-b px-5 py-4 min-[1600px]:flex-row min-[1600px]:flex-wrap min-[1600px]:items-center min-[1600px]:gap-x-3 min-[1600px]:gap-y-2 min-[1600px]:py-1.5 min-[1920px]:flex-nowrap"
        data-testid="index-panel-header"
      >
        <div
          className="min-w-0 min-[1600px]:flex min-[1600px]:min-w-min min-[1600px]:flex-[1_1_12rem] min-[1600px]:flex-wrap min-[1600px]:items-baseline min-[1600px]:gap-x-3"
          data-testid="index-panel-title-block"
        >
          {captionId === undefined ? (
            <h2
              className="text-base font-semibold min-[1600px]:shrink-0 min-[1600px]:whitespace-nowrap"
              id={titleId}
            >
              {title}
            </h2>
          ) : (
            <span className="inline-flex items-baseline gap-1.5 min-[1600px]:shrink-0">
              <h2
                className="text-base font-semibold min-[1600px]:shrink-0 min-[1600px]:whitespace-nowrap"
                id={titleId}
              >
                {title}
              </h2>
              <InfoHint id={captionId} label={title} />
            </span>
          )}
          <p
            className="text-muted-foreground mt-0.5 text-sm min-[1600px]:mt-0 min-[1600px]:line-clamp-3 min-[1600px]:min-w-[10rem] min-[1600px]:flex-[1_1_10rem] min-[1600px]:text-xs min-[1600px]:leading-snug"
            title={description}
          >
            {description}
          </p>
        </div>
        {notes ? (
          <div
            className="flex flex-col gap-3 min-[1600px]:min-w-[22rem] min-[1600px]:flex-[3_0.5_34rem] min-[1600px]:flex-row min-[1600px]:gap-2"
            data-testid="index-panel-notes"
          >
            {notes}
          </div>
        ) : null}
        {stats && stats.length > 0 ? <StatStrip stats={stats} /> : null}
        {actions ? (
          <div
            className="flex shrink-0 flex-wrap items-center gap-2 min-[1600px]:ml-auto min-[1600px]:shrink min-[1600px]:justify-end"
            data-testid="index-panel-actions"
          >
            {actions}
          </div>
        ) : null}
      </div>
      <div className="min-w-0 p-5 min-[1600px]:p-2">{children}</div>
    </section>
  )
}
