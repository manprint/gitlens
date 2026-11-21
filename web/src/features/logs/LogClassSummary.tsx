import { Section } from '@/components/layout/Section'
import { ErrorState } from '@/components/state'
import type { ApiFailure } from '@/api/client'
import type { components } from '@/api/generated'
import { useT } from '@/i18n/react'
import { cn } from '@/lib/utils'

type LogClassCount = components['schemas']['LogClassCount']

export interface LogClassSummaryProps {
  classes: readonly LogClassCount[] | undefined
  loading: boolean
  failure: ApiFailure | null
  onRetry: () => void
  selected: string
  onSelect: (logClass: string) => void
}

/** The line count of each class in the range; a class is a filter toggle. */
export function LogClassSummary({
  classes,
  loading,
  failure,
  onRetry,
  selected,
  onSelect,
}: LogClassSummaryProps) {
  const t = useT()
  let body
  if (loading && classes === undefined) {
    body = <p role="status">{t('logs.classes.loading')}</p>
  } else if (failure && classes === undefined) {
    body = <ErrorState failure={failure} onRetry={onRetry} endpoint="logs" />
  } else {
    const chip = (value: string, label: string) => (
      <li key={value || '*'}>
        <button
          aria-pressed={selected === value}
          className={cn(
            'hover:bg-muted rounded-md border px-2.5 py-1 text-sm',
            selected === value && 'bg-muted border-primary font-medium',
          )}
          onClick={() => onSelect(value)}
          type="button"
        >
          {label}
        </button>
      </li>
    )
    body = (
      <ul aria-label={t('logs.classes.label')} className="flex flex-wrap gap-2">
        {chip('', t('logs.classes.all'))}
        {(classes ?? []).map((c) =>
          chip(c.class, t('logs.classes.item', { class: c.class, count: c.count })),
        )}
      </ul>
    )
  }
  return (
    <Section captionId="logs.class-summary" title={t('logs.classes.title')}>
      {body}
    </Section>
  )
}
