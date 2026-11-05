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
  /**
   * From 1600 px the title is cut to one bounded line with the whole text in
   * its tooltip: for a title a user chose (a cluster or instance name).
   */
  truncateTitle?: boolean
  /**
   * For a header whose actions need the whole screen width to share the title
   * line: from 1600 px the actions drop under the title, and only from 1900 px
   * (the Full HD reference screen) do both sit on one line.
   */
  wrapActionsBelowFullHd?: boolean
}

export function PageHeader({
  title,
  subtitle,
  freshness,
  actions,
  icon: Icon,
  eyebrow,
  truncateTitle = false,
  wrapActionsBelowFullHd = false,
}: PageHeaderProps) {
  return (
    // From 1600 px the eyebrow, title and subtitle share one line (D26).
    <header
      className={`flex flex-col gap-4 pb-2 min-[1600px]:items-center min-[1600px]:pb-0 sm:flex-row sm:items-start sm:justify-between ${
        wrapActionsBelowFullHd
          ? 'min-[1600px]:flex-wrap min-[1600px]:gap-y-2 min-[1900px]:flex-nowrap'
          : ''
      }`}
    >
      <div className="flex min-w-0 items-start gap-4 min-[1600px]:items-center min-[1600px]:gap-3">
        {Icon ? (
          <span
            aria-hidden="true"
            className="bg-primary/10 text-primary ring-primary/15 mt-0.5 hidden size-11 shrink-0 items-center justify-center rounded-xl ring-1 min-[1600px]:mt-0 min-[1600px]:size-9 sm:flex"
          >
            <Icon className="size-5" />
          </span>
        ) : null}
        <div className="min-w-0 min-[1600px]:flex min-[1600px]:items-baseline min-[1600px]:gap-3">
          {eyebrow ? (
            <p className="text-primary mb-1 text-xs font-semibold tracking-wider uppercase min-[1600px]:mb-0 min-[1600px]:shrink-0">
              {eyebrow}
            </p>
          ) : null}
          <h1
            id="page-title"
            className={`text-foreground text-2xl font-semibold tracking-tight break-words min-[1600px]:text-xl ${
              truncateTitle
                ? 'min-[1600px]:max-w-md min-[1600px]:min-w-0 min-[1600px]:shrink-0 min-[1600px]:truncate'
                : 'min-[1600px]:shrink-0'
            }`}
            {...(truncateTitle ? { title } : {})}
          >
            {title}
          </h1>
          {subtitle ? (
            // From 1600 px the subtitle stays on one line (D26); the full text is its title.
            <p
              className="text-muted-foreground mt-1.5 max-w-3xl text-sm leading-relaxed min-[1600px]:mt-0 min-[1600px]:max-w-none min-[1600px]:min-w-0 min-[1600px]:truncate"
              title={typeof subtitle === 'string' ? subtitle : undefined}
            >
              {subtitle}
            </p>
          ) : null}
        </div>
      </div>
      {freshness || actions ? (
        <div className="flex flex-wrap items-center gap-2 min-[1600px]:shrink-0 min-[1600px]:flex-nowrap sm:justify-end">
          {freshness}
          {actions}
        </div>
      ) : null}
    </header>
  )
}
