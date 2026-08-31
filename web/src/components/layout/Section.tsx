import { useId, type ReactNode } from 'react'

interface SectionProps {
  title: string
  description?: ReactNode
  children: ReactNode
  id?: string
}

export function Section({ title, description, children, id }: SectionProps) {
  const generatedId = useId().replaceAll(':', '')
  const titleId = `${id ?? generatedId}-title`

  return (
    <section aria-labelledby={titleId} className="space-y-4">
      <div>
        <h2 id={titleId} className="text-lg font-medium">
          {title}
        </h2>
        {description ? <p className="text-muted-foreground mt-1 text-sm">{description}</p> : null}
      </div>
      <div>{children}</div>
    </section>
  )
}
