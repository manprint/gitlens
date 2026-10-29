import { useId, type ReactNode } from 'react'

export interface ChartGroupProps {
  id: string
  title: string
  description: string
  children: ReactNode
}

/** A titled group of ChartCards in a two-column grid on large screens. */
export function ChartGroup({ id, title, description, children }: ChartGroupProps) {
  const titleId = `${useId().replaceAll(':', '')}-chart-group-title`

  return (
    <section aria-labelledby={titleId} className="space-y-3" id={id}>
      <div>
        <h2 className="text-foreground text-base font-semibold" id={titleId}>
          {title}
        </h2>
        <p className="text-muted-foreground text-sm">{description}</p>
      </div>
      <div className="grid gap-6 lg:grid-cols-2" data-testid={`${id}-grid`}>
        {children}
      </div>
    </section>
  )
}
