import type { ComponentType, ReactNode } from 'react'

interface PageHeaderProps {
  title: string
  subtitle?: ReactNode
  freshness?: ReactNode
  actions?: ReactNode
  /** A glyph shown beside the title, matching the page's navigation icon. */
  icon?: ComponentType<{ className?: string }>
  /** A small label above the title, such as the section of the product. */
  eyebrow?: ReactNode
}

export function PageHeader({
  title,
  subtitle,
  freshness,
  actions,
  icon: Icon,
  eyebrow,
}: PageHeaderProps) {
  return (
    <header className="flex flex-col gap-4 pb-2 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex min-w-0 items-start gap-4">
        {Icon ? (
          <span
            aria-hidden="true"
            className="bg-primary/10 text-primary ring-primary/15 mt-0.5 hidden size-11 shrink-0 items-center justify-center rounded-xl ring-1 sm:flex"
          >
            <Icon className="size-5" />
          </span>
        ) : null}
        <div className="min-w-0">
          {eyebrow ? (
            <p className="text-primary mb-1 text-xs font-semibold tracking-wider uppercase">
              {eyebrow}
            </p>
          ) : null}
          <h1
            id="page-title"
            className="text-foreground text-2xl font-semibold tracking-tight break-words"
          >
            {title}
          </h1>
          {subtitle ? (
            <p className="text-muted-foreground mt-1.5 max-w-3xl text-sm leading-relaxed">
              {subtitle}
            </p>
          ) : null}
        </div>
      </div>
      {freshness || actions ? (
        <div className="flex flex-wrap items-center gap-2 sm:justify-end">
          {freshness}
          {actions}
        </div>
      ) : null}
    </header>
  )
}
