import type { ReactNode } from 'react'

interface EmptyStateProps {
  title: string
  description: string
  action?: ReactNode
}

export function EmptyState({ title, description, action }: EmptyStateProps) {
  return (
    <section aria-labelledby="empty-state-title" className="border-muted bg-muted/20">
      <h2 id="empty-state-title">{title}</h2>
      <p>{description}</p>
      {action ? <div>{action}</div> : null}
    </section>
  )
}
