import { useId, type ReactNode } from 'react'

interface SectionProps {
  title: string
  description?: ReactNode
  children: ReactNode
  id?: string
  /** Controls shown at the right of the section heading. */
  actions?: ReactNode
}

export function Section({ title, description, children, id, actions }: SectionProps) {
  const generatedId = useId().replaceAll(':', '')
  const titleId = `${id ?? generatedId}-title`

  return (
    <section
      aria-labelledby={titleId}
      className="bg-card shadow-card scroll-mt-20 rounded-xl border"
      {...(id === undefined ? {} : { id })}
    >
      <div className="flex flex-col gap-3 border-b px-5 py-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h2 id={titleId} className="text-foreground text-base font-semibold">
            {title}
          </h2>
          {description ? (
            <p className="text-muted-foreground mt-1 text-sm leading-relaxed">{description}</p>
          ) : null}
        </div>
        {actions ? (
          <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>
        ) : null}
      </div>
      <div className="min-w-0 p-5">{children}</div>
    </section>
  )
}
