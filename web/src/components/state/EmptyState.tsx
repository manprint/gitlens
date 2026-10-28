import { useId, type ReactNode } from 'react'
import { Inbox } from 'lucide-react'

interface EmptyStateProps {
  title: string
  description: string
  action?: ReactNode
}

export function EmptyState({ title, description, action }: EmptyStateProps) {
  const titleId = `${useId().replaceAll(':', '')}-empty-state-title`

  return (
    <section
      aria-labelledby={titleId}
      className="border-border bg-muted/30 flex flex-col items-center justify-center rounded-xl border border-dashed px-6 py-10 text-center"
    >
      <span
        aria-hidden="true"
        className="bg-card text-muted-foreground shadow-card mb-3 flex size-10 items-center justify-center rounded-full border"
      >
        <Inbox className="size-5" />
      </span>
      <h2 id={titleId} className="text-foreground text-sm font-semibold">
        {title}
      </h2>
      <p className="text-muted-foreground mt-1 max-w-md text-sm">{description}</p>
      {action ? <div className="mt-4">{action}</div> : null}
    </section>
  )
}
