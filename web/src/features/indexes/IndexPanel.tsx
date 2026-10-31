import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

/**
 * The layout of an Index analysis tab (D26): one card whose header carries the
 * title, the hint, the status notes, the figures and the actions, on one line
 * from 1600 px, above the tab's single table. Below 1600 px the header stacks.
 * The line holds at 1920 px; on a narrower screen that cannot hold the title,
 * the figures and the actions side by side, the figures or the actions wrap
 * to a second line rather than overlap or leave the card.
 */

/** A badge inside a table cell: no vertical padding from 1600 px, so the row keeps its height. */
export const CELL_BADGE = 'min-[1600px]:py-0'

export interface PanelStat {
  label: string
  value: string
  /** Shown beside the value at every width. */
  unit?: string
  /** Shown below 1600 px only; from 1600 px it is in the figure's `title`. */
  detail?: string
}

/** The tab's figures: tiles below 1600 px, one line of label and value from 1600 px. */
function StatStrip({ stats }: { stats: readonly PanelStat[] }) {
  return (
    <div className="grid gap-4 min-[1600px]:flex min-[1600px]:shrink-0 min-[1600px]:gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {stats.map((stat) => (
        <article
          key={stat.label}
          aria-label={stat.label}
          className="bg-card shadow-card rounded-xl border p-4 min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-1.5 min-[1600px]:rounded-none min-[1600px]:border-0 min-[1600px]:bg-transparent min-[1600px]:p-0 min-[1600px]:whitespace-nowrap min-[1600px]:shadow-none"
          title={[`${stat.label}:`, stat.value, stat.unit, stat.detail]
            .filter((part) => part !== undefined && part !== '')
            .join(' ')}
        >
          <p className="text-muted-foreground text-xs font-medium tracking-wide uppercase">
            {stat.label}
          </p>
          <div className="mt-2 flex items-baseline gap-1.5 min-[1600px]:mt-0">
            <span className="text-foreground text-2xl font-semibold tracking-tight tabular-nums min-[1600px]:text-sm">
              {stat.value}
            </span>
            {stat.unit ? (
              <span className="text-muted-foreground text-sm min-[1600px]:max-w-40 min-[1600px]:truncate min-[1600px]:text-xs">
                {stat.unit}
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
 * A status note: a box below 1600 px, a one-line chip from 1600 px whose full
 * text is its `title` (D26).
 */
export function PanelNote({
  title,
  tone = 'muted',
  children,
}: {
  /** The full text of the note, shown on hover from 1600 px. */
  title: string
  tone?: 'muted' | 'warning'
  children: ReactNode
}) {
  return (
    <p
      className={cn(
        'rounded-lg border px-4 py-3 text-sm',
        tone === 'warning' ? 'border-warning/40 bg-warning/10' : 'bg-muted/40',
        'min-[1600px]:max-w-72 min-[1600px]:min-w-0 min-[1600px]:truncate min-[1600px]:rounded-full min-[1600px]:px-2.5 min-[1600px]:py-0.5 min-[1600px]:text-xs',
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
  /** The hint: one truncated line from 1600 px with the full text in its `title`. */
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
  description,
  stats,
  notes,
  actions,
  children,
}: IndexPanelProps) {
  return (
    <section
      aria-labelledby={titleId}
      className="bg-card shadow-card overflow-hidden rounded-xl border"
    >
      <div
        className="flex flex-col gap-4 border-b px-5 py-4 min-[1600px]:flex-row min-[1600px]:flex-wrap min-[1600px]:items-center min-[1600px]:gap-y-2 min-[1600px]:py-2.5"
        data-testid="index-panel-header"
      >
        {/*
          From 1600 px the title block keeps its automatic minimum width, which is
          the width of its content once the hint has none: the title never wraps
          there, so the block cannot shrink below the whole title and the notes
          beside it truncate instead of covering its end. A title that could wrap
          let the block shrink to its longest word while the title stayed on one
          line and spilled under the first note.
        */}
        <div
          className="min-[1600px]:flex min-[1600px]:flex-1 min-[1600px]:items-baseline min-[1600px]:gap-3"
          data-testid="index-panel-title-block"
        >
          <h2
            className="text-base font-semibold min-[1600px]:shrink-0 min-[1600px]:whitespace-nowrap"
            id={titleId}
          >
            {title}
          </h2>
          <p
            className="text-muted-foreground mt-0.5 text-sm min-[1600px]:mt-0 min-[1600px]:w-0 min-[1600px]:flex-1 min-[1600px]:truncate"
            title={description}
          >
            {description}
          </p>
        </div>
        {/*
          From 1600 px the notes take the room the title block leaves (no more
          than they need) and keep at least 6rem, enough for two chips cut to
          an ellipsis: the line wraps before they could spill over the figures.
        */}
        {notes ? (
          <div
            className="flex flex-col gap-3 min-[1600px]:max-w-fit min-[1600px]:min-w-24 min-[1600px]:flex-1 min-[1600px]:flex-row min-[1600px]:gap-2"
            data-testid="index-panel-notes"
          >
            {notes}
          </div>
        ) : null}
        {stats && stats.length > 0 ? <StatStrip stats={stats} /> : null}
        {actions ? (
          // Right-aligned when the line wraps and the actions start the second line.
          <div
            className="flex shrink-0 flex-wrap items-center gap-2 min-[1600px]:ml-auto"
            data-testid="index-panel-actions"
          >
            {actions}
          </div>
        ) : null}
      </div>
      <div className="min-w-0 p-5 min-[1600px]:p-4">{children}</div>
    </section>
  )
}
