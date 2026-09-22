import { Link } from 'react-router-dom'

import { useAshTop } from '@/api/queries'
import type { AshTopQuery } from '@/lib/ash'
import { topQueries } from '@/lib/ash'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'

interface AshTopQueriesProps {
  database?: string
  from: Date
  instanceId: string
  to: Date
  /** The drilled wait event, so the list ranks the queries seen in it. */
  waitEvent?: string
  waitEventType?: string
}

function queryPath(instanceId: string, queryid: string): string {
  return `/instances/${encodeURIComponent(instanceId)}/queries/${encodeURIComponent(queryid)}`
}

function QueryRow({ instanceId, query }: { instanceId: string; query: AshTopQuery }) {
  return (
    <li className="bg-card rounded-lg border p-4 shadow-sm">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <Link className="font-medium underline" to={queryPath(instanceId, query.queryid)}>
          Query {query.queryid}
        </Link>
        <span className="text-text-secondary text-sm">
          {query.avg_active_sessions === null
            ? 'Unknown average active sessions'
            : `${query.avg_active_sessions} avg active sessions`}
        </span>
      </div>
      <code className="mt-2 block text-sm whitespace-pre-wrap">{query.query_text}</code>
      <p className="text-text-secondary mt-2 text-xs">
        {query.samples} samples across {query.ticks} ticks
      </p>
    </li>
  )
}

function scopeDescription(waitEventType?: string, waitEvent?: string): string {
  if (waitEvent !== undefined) {
    return waitEventType === undefined
      ? `Ranked by samples in wait event ${waitEvent}.`
      : `Ranked by samples in wait event ${waitEventType}/${waitEvent}.`
  }
  return waitEventType === undefined
    ? 'Ranked by samples across every wait event.'
    : `Ranked by samples in wait event type ${waitEventType}.`
}

export function AshTopQueries({
  database,
  from,
  instanceId,
  to,
  waitEvent,
  waitEventType,
}: AshTopQueriesProps) {
  const params = {
    from: from.toISOString(),
    instance_id: instanceId,
    limit: 20,
    to: to.toISOString(),
    ...(database === undefined ? {} : { database }),
    ...(waitEventType === undefined ? {} : { wait_event_type: waitEventType }),
    ...(waitEvent === undefined ? {} : { wait_event: waitEvent }),
  }
  const query = useAshTop(params)

  if (query.error && !query.data) {
    return (
      <ErrorState
        endpoint="ASH top queries"
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }

  if (query.isPending || query.data === undefined) {
    return (
      <section aria-busy="true" aria-label="Loading ASH top queries" role="status">
        Loading ASH top queries…
      </section>
    )
  }

  const entries = topQueries(query.data.entries)

  return (
    <Section
      title="Top queries"
      description={`${scopeDescription(waitEventType, waitEvent)} Query text is the normalised text from pg_stat_statements. ASH never collects live query text; it collects only query_id. This deliberate PII decision means literal values are not available here.`}
    >
      {entries.length > 0 ? (
        <ol className="grid gap-3" aria-label="ASH top queries">
          {entries.map((entry) => (
            <QueryRow instanceId={instanceId} key={entry.queryid} query={entry} />
          ))}
        </ol>
      ) : (
        <EmptyState
          title="No top queries"
          description="No query ids were sampled in the selected time range."
        />
      )}
    </Section>
  )
}
