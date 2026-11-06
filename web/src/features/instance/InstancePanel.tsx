import { useId, type ReactNode } from 'react'

import { Caption } from '@/components/layout/Caption'
import { InfoHint } from '@/components/layout/InfoHint'
import { TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { CaptionId } from '@/lib/captions'

import type { InstanceView } from './instanceViews'

/** The tab list of a panel's views, compact enough for the panel's heading line from 1600 px. */
export function InstanceViewTabs({
  label,
  views,
}: {
  label: string
  views: readonly InstanceView[]
}) {
  return (
    <TabsList
      aria-label={label}
      className="h-auto max-w-full flex-wrap justify-start min-[1600px]:h-8"
    >
      {views.map((view) => (
        <TabsTrigger key={view.id} value={view.id}>
          {view.label}
        </TabsTrigger>
      ))}
    </TabsList>
  )
}

/**
 * A hint or notice in a table's toolbar line (`toolbarExtra`). Below 1600 px it
 * wraps onto its own line; from 1600 px it takes the free width of the toolbar
 * line in at most two small lines, with the full text in its title (D26).
 */
export function ToolbarNote({
  children,
  status = false,
  wrap = false,
}: {
  children: string
  status?: boolean
  /** Keep the whole sentence on screen, for a notice a reader must be able to read in full. */
  wrap?: boolean
}) {
  return (
    <p
      className={
        wrap
          ? 'text-muted-foreground text-sm'
          : 'text-muted-foreground text-sm min-[1600px]:line-clamp-2 min-[1600px]:min-w-0 min-[1600px]:shrink min-[1600px]:grow min-[1600px]:basis-0 min-[1600px]:self-center min-[1600px]:text-xs min-[1600px]:leading-snug'
      }
      title={children}
      {...(status ? { role: 'status' } : {})}
    >
      {children}
    </p>
  )
}

/**
 * A table section with nothing to list keeps its caption and ⓘ, so a reader
 * still learns what the section shows; the empty state follows it.
 */
export function CaptionedBlock({
  ariaLabel,
  captionId,
  children,
}: {
  ariaLabel: string
  captionId: CaptionId
  children: ReactNode
}) {
  return (
    <div className="space-y-3" data-card="" data-caption-id={captionId}>
      <div className="flex w-full min-w-0 items-center gap-1">
        <Caption className="text-xs" id={captionId} marked={false} oneLine />
        <InfoHint className="shrink-0" id={captionId} label={ariaLabel} />
      </div>
      {children}
    </div>
  )
}

interface InstancePanelProps {
  title: string
  description?: ReactNode
  children: ReactNode
  id?: string
  /** Controls, views and summaries shown beside the heading from 1600 px. */
  actions?: ReactNode
  /**
   * The panel's caption (D30): the catalogue sentence under the title, with an ⓘ
   * for the details. A `description` given too wraps below it.
   */
  captionId?: CaptionId
}

/**
 * The frame of an instance tab. Below 1600 px it is the `Section` card, with
 * the actions under the heading. From 1600 px (D26) it is flat: one heading
 * line holds the title, the description (two lines at most) and the actions, and the
 * table below it keeps its 20 rows on a Full HD screen.
 */
export function InstancePanel({
  title,
  description,
  children,
  id,
  actions,
  captionId,
}: InstancePanelProps) {
  const generatedId = useId().replaceAll(':', '')
  const titleId = `${id ?? generatedId}-title`

  return (
    <section
      aria-labelledby={titleId}
      className="bg-card shadow-card scroll-mt-20 rounded-xl border min-[1600px]:rounded-none min-[1600px]:border-0 min-[1600px]:bg-transparent min-[1600px]:shadow-none"
      {...(id === undefined ? {} : { id })}
      {...(captionId === undefined ? {} : { 'data-card': '', 'data-caption-id': captionId })}
    >
      <div className="flex flex-col gap-3 border-b px-5 py-4 min-[1600px]:flex-row min-[1600px]:flex-wrap min-[1600px]:items-center min-[1600px]:justify-between min-[1600px]:gap-x-4 min-[1600px]:gap-y-2 min-[1600px]:border-b-0 min-[1600px]:px-0 min-[1600px]:pt-0 min-[1600px]:pb-2">
        <div className="min-w-0 min-[1600px]:flex-[1_1_30rem]">
          <div className="min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-3">
            <div className="flex items-center gap-1 min-[1600px]:shrink-0">
              <h2 id={titleId} className="text-foreground text-base font-semibold">
                {title}
              </h2>
              {captionId !== undefined ? <InfoHint id={captionId} label={title} /> : null}
            </div>
            {captionId !== undefined ? (
              <Caption
                className="mt-1 leading-relaxed min-[1600px]:mt-0"
                id={captionId}
                marked={false}
                oneLine
              />
            ) : description ? (
              <p
                className="text-muted-foreground mt-1 text-sm leading-relaxed min-[1600px]:mt-0 min-[1600px]:line-clamp-2 min-[1600px]:min-w-0"
                title={typeof description === 'string' ? description : undefined}
              >
                {description}
              </p>
            ) : null}
          </div>
          {captionId !== undefined && description ? (
            <p className="text-muted-foreground mt-1 text-sm leading-relaxed">{description}</p>
          ) : null}
        </div>
        {actions ? (
          <div
            className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2 min-[1600px]:shrink-0 min-[1600px]:flex-nowrap"
            data-testid="instance-panel-actions"
          >
            {actions}
          </div>
        ) : null}
      </div>
      <div className="min-w-0 p-5 min-[1600px]:p-0">{children}</div>
    </section>
  )
}
