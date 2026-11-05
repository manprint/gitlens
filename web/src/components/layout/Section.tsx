import { useId, type ReactNode } from 'react'

import type { CaptionId } from '@/lib/captions'

import { Caption } from './Caption'
import { InfoHint } from './InfoHint'

interface SectionProps {
  title: string
  description?: ReactNode
  children: ReactNode
  id?: string
  /** Controls shown at the right of the section heading. */
  actions?: ReactNode
  /** The section's caption (D30): replaces `description`, with an ⓘ for the details. */
  captionId?: CaptionId
}

export function Section({ title, description, children, id, actions, captionId }: SectionProps) {
  const generatedId = useId().replaceAll(':', '')
  const titleId = `${id ?? generatedId}-title`

  return (
    <section
      aria-labelledby={titleId}
      data-card=""
      className="bg-card shadow-card scroll-mt-20 rounded-xl border"
      {...(id === undefined ? {} : { id })}
      {...(captionId === undefined ? {} : { 'data-caption-id': captionId })}
    >
      <div
        className="flex flex-col gap-3 border-b px-5 py-4 min-[1600px]:items-center min-[1600px]:py-2.5 sm:flex-row sm:items-start sm:justify-between"
        data-testid="section-header"
      >
        {/* From 1600 px the title and its description share one line (D26). */}
        <div className="min-w-0 min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-3">
          <div className="flex items-center gap-1 min-[1600px]:shrink-0">
            <h2
              id={titleId}
              className="text-foreground text-base font-semibold min-[1600px]:shrink-0"
            >
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
              className="text-muted-foreground mt-1 text-sm leading-relaxed min-[1600px]:mt-0 min-[1600px]:min-w-0 min-[1600px]:truncate"
              title={typeof description === 'string' ? description : undefined}
            >
              {description}
            </p>
          ) : null}
        </div>
        {actions ? (
          <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>
        ) : null}
      </div>
      <div className="min-w-0 p-5 min-[1600px]:p-4">{children}</div>
    </section>
  )
}
