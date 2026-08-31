import type { ReactNode } from 'react'

interface PageHeaderProps {
  title: string
  subtitle?: ReactNode
  freshness?: ReactNode
  actions?: ReactNode
}

export function PageHeader({ title, subtitle, freshness, actions }: PageHeaderProps) {
  return (
    <header className="flex flex-col gap-4 border-b pb-5 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h1 id="page-title" className="text-2xl font-semibold tracking-tight">
          {title}
        </h1>
        {subtitle ? <p className="text-muted-foreground mt-1 text-sm">{subtitle}</p> : null}
      </div>
      {freshness || actions ? (
        <div className="flex flex-wrap items-center gap-2">
          {freshness}
          {actions}
        </div>
      ) : null}
    </header>
  )
}
