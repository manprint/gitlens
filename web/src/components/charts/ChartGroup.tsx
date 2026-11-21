import type { ReactNode } from 'react'

import { Section } from '@/components/layout/Section'
import type { CaptionId } from '@/lib/captions'

export interface ChartGroupProps {
  id: string
  title: string
  /** The group's caption: its sentence under the title, details behind the ⓘ. */
  captionId: CaptionId
  /** Extra classes of the group card, e.g. its scroll margin. */
  className?: string
  children: ReactNode
}

/** A titled card of ChartCards, built like every Section, in two columns on large screens. */
export function ChartGroup({ id, title, captionId, className, children }: ChartGroupProps) {
  return (
    <Section
      captionId={captionId}
      id={id}
      title={title}
      {...(className === undefined ? {} : { className })}
    >
      <div className="grid gap-6 lg:grid-cols-2" data-testid={`${id}-grid`}>
        {children}
      </div>
    </Section>
  )
}
