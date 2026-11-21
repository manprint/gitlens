import { Link } from 'react-router-dom'

import { useT } from '@/i18n/react'

import { useAshTop } from '@/api/queries'
import type { AshDimensionFilters, AshTopQuery } from '@/lib/ash'
import { topQueries } from '@/lib/ash'
import type { QueryWindow } from '@/lib/timerange'
import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'

interface AshTopQueriesProps {
  database?: string
  /** The page's session filters (user, client, application). */
  dimensions?: AshDimensionFilters
  instanceId: string
  timeWindow: QueryWindow
  /** The drilled wait event, so the list ranks the queries seen in it. */
  waitEvent?: string
  waitEventType?: string
}

function queryPath(instanceId: string, queryid: string, database?: string): string {
  const path = `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(queryid)}`
  // EXPLAIN runs in the database named here, so a filtered view must name it too.
  return database ? `${path}?db=${encodeURIComponent(database)}` : path
}

function QueryRow({
  database,
  instanceId,
  query,
}: {
  database: string | undefined
  instanceId: string
  query: AshTopQuery
}) {
  const t = useT()
  return (
    <li className="bg-card rounded-lg border p-4 shadow-sm">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <Link className="font-medium underline" to={queryPath(instanceId, query.queryid, database)}>
          {t('ash.top.queryLink', { id: query.queryid })}
        </Link>
        <span className="text-muted-foreground text-sm">
          {query.avg_active_sessions === null
            ? t('ash.top.avgUnknown')
            : t('ash.top.avg', { value: query.avg_active_sessions })}
        </span>
      </div>
      <code className="mt-2 block text-sm whitespace-pre-wrap">{query.query_text}</code>
      <p className="text-muted-foreground mt-2 text-xs">
        {t('ash.top.samples', { samples: query.samples, ticks: query.ticks })}
      </p>
    </li>
  )
}

function scopeDescription(
  t: ReturnType<typeof useT>,
  waitEventType?: string,
  waitEvent?: string,
): string {
  if (waitEvent !== undefined) {
    return waitEventType === undefined
      ? t('ash.top.scope.event', { event: waitEvent })
      : t('ash.top.scope.typeEvent', { type: waitEventType, event: waitEvent })
  }
  return waitEventType === undefined
    ? t('ash.top.scope.all')
    : t('ash.top.scope.type', { type: waitEventType })
}

export function AshTopQueries({
  database,
  dimensions,
  instanceId,
  timeWindow,
  waitEvent,
  waitEventType,
}: AshTopQueriesProps) {
  const t = useT()
  const params = {
    instance_id: instanceId,
    limit: 20,
    ...(database === undefined ? {} : { database }),
    ...(waitEventType === undefined ? {} : { wait_event_type: waitEventType }),
    ...(waitEvent === undefined ? {} : { wait_event: waitEvent }),
    ...dimensions,
  }
  const query = useAshTop(params, timeWindow)

  if (query.error && !query.data) {
    return (
      <ErrorState
        endpoint={t('ash.top.listLabel')}
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }

  if (query.isPending || query.data === undefined) {
    return (
      <section aria-busy="true" aria-label={t('ash.top.loadingLabel')} role="status">
        {t('ash.top.loading')}
      </section>
    )
  }

  const entries = topQueries(query.data.entries)

  return (
    <Section
      actions={<HelpLink anchor="wait-events#top-queries" label={t('ash.top.title')} />}
      captionId="ash.top-queries"
      title={t('ash.top.title')}
    >
      <p className="text-muted-foreground mb-3 text-sm">
        {`${scopeDescription(t, waitEventType, waitEvent)} ${t('ash.top.note')}`}
      </p>
      {entries.length > 0 ? (
        <ol className="grid gap-3" aria-label={t('ash.top.listLabel')}>
          {entries.map((entry) => (
            <QueryRow
              database={database}
              instanceId={instanceId}
              key={entry.queryid}
              query={entry}
            />
          ))}
        </ol>
      ) : (
        <EmptyState title={t('ash.top.emptyTitle')} description={t('ash.top.emptyBody')} />
      )}
    </Section>
  )
}
