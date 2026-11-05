import type { ReactNode } from 'react'

import { Caption } from '@/components/layout/Caption'
import { InfoHint } from '@/components/layout/InfoHint'
import type { CaptionId } from '@/lib/captions'

/**
 * A table or chart section with nothing to list keeps its caption and ⓘ, so a
 * reader still learns what the section shows; the empty state follows it.
 */
export function EmptyCaptioned({
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
