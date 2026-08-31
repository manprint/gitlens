import { useId, type ReactNode } from 'react'

interface EmptyStateProps {
  title: string
  description: string
  action?: ReactNode
}

export function EmptyState({ title, description, action }: EmptyStateProps) {
  const titleId = `${useId().replaceAll(':', '')}-empty-state-title`

  return (
    <section aria-labelledby={titleId} className="border-muted bg-muted/20">
      <h2 id={titleId}>{title}</h2>
      <p>{description}</p>
      {action ? <div>{action}</div> : null}
    </section>
  )
}
