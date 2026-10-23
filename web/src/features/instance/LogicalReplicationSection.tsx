import { useMemo } from 'react'

import { useInstanceLogicalReplication } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { formatBytes, formatCount, formatRate, formatTimestamp } from '@/lib/format'

type Subscription = Schemas['Subscription']
type LogicalSlot = Schemas['LogicalSlotStats']

/** How long an apply worker may hear nothing from its publisher, as in the advisor. */
const SUBSCRIPTION_SILENT_MS = 5 * 60 * 1000
/** Spill rate from which a slot is marked, as in the advisor. */
const SPILL_BYTES_PER_SECOND = 1024 * 1024

function subscriptionState(sub: Subscription, readAt: number) {
  if (!sub.enabled) return <Badge variant="secondary">disabled</Badge>
  if (!sub.apply_worker) return <Badge variant="destructive">not running</Badge>
  if (sub.last_message_at !== null) {
    const silent = readAt - new Date(sub.last_message_at).getTime()
    if (silent >= SUBSCRIPTION_SILENT_MS) return <Badge variant="destructive">silent</Badge>
  }
  return <Badge variant="outline">running</Badge>
}

function subscriptionColumns(readAt: number): DataTableProps<Subscription>['columns'] {
  return [
    { accessorKey: 'name', header: 'Subscription' },
    { accessorKey: 'database', header: 'Database' },
    {
      id: 'state',
      header: 'State',
      accessorFn: (sub) => (sub.enabled ? (sub.apply_worker ? 2 : 1) : 0),
      cell: ({ row }) => subscriptionState(row.original, readAt),
    },
    {
      id: 'publications',
      header: 'Publications',
      accessorFn: (sub) => sub.publications.join(', '),
    },
    {
      id: 'slot',
      header: 'Slot',
      accessorFn: (sub) => sub.slot_name ?? '',
      cell: ({ row }) =>
        row.original.slot_name ?? <span className="text-muted-foreground">none</span>,
    },
    {
      id: 'last_message',
      header: 'Last message',
      accessorFn: (sub) => sub.last_message_at ?? '',
      cell: ({ row }) =>
        row.original.last_message_at === null
          ? null
          : formatTimestamp(row.original.last_message_at, 'UTC'),
    },
    {
      id: 'errors',
      header: 'Errors (apply / sync)',
      accessorFn: (sub) => sub.apply_errors + sub.sync_errors,
      cell: ({ row }) =>
        `${formatCount(row.original.apply_errors)} / ${formatCount(row.original.sync_errors)}`,
    },
    {
      id: 'conflicts',
      header: 'Conflicts',
      accessorFn: (sub) => sub.conflicts ?? -1,
      cell: ({ row }) =>
        row.original.conflicts === null ? (
          <span className="text-muted-foreground">n/a</span>
        ) : (
          formatCount(row.original.conflicts)
        ),
    },
  ]
}

function perSecond(bytes: number): string {
  return `${formatBytes(bytes)}/s`
}

function slotColumns(): DataTableProps<LogicalSlot>['columns'] {
  return [
    { accessorKey: 'slot_name', header: 'Slot' },
    {
      accessorKey: 'bytes_per_second',
      header: 'Decoded',
      cell: ({ row }) => perSecond(row.original.bytes_per_second),
    },
    {
      accessorKey: 'txns_per_second',
      header: 'Transactions',
      cell: ({ row }) => formatRate(row.original.txns_per_second),
    },
    {
      accessorKey: 'spill_bytes_per_second',
      header: 'Spilled to disk',
      cell: ({ row }) =>
        row.original.spill_bytes_per_second >= SPILL_BYTES_PER_SECOND ? (
          <Badge variant="secondary">{perSecond(row.original.spill_bytes_per_second)}</Badge>
        ) : (
          perSecond(row.original.spill_bytes_per_second)
        ),
    },
    {
      accessorKey: 'stream_bytes_per_second',
      header: 'Streamed',
      cell: ({ row }) => perSecond(row.original.stream_bytes_per_second),
    },
  ]
}

export function LogicalReplicationSection({ instanceId }: { instanceId: string }) {
  const query = useInstanceLogicalReplication(instanceId)
  // Silence is judged against when the data was read, not the clock.
  const readAt = query.dataUpdatedAt
  const subscriptions = useMemo(() => subscriptionColumns(readAt), [readAt])
  const slots = useMemo(() => slotColumns(), [])

  let body
  if (query.error && !query.data) {
    body = (
      <ErrorState
        endpoint="instance logical replication"
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  } else if (query.isPending || query.data === undefined) {
    body = (
      <section aria-busy="true" aria-label="Loading logical replication" role="status">
        Loading logical replication…
      </section>
    )
  } else if (query.data.subscriptions.length === 0 && query.data.slots.length === 0) {
    body = (
      <EmptyState
        title="No logical replication"
        description="The instance has no subscription and no logical slot with decoding statistics."
      />
    )
  } else {
    const minutes = Math.round(query.data.window_seconds / 60)
    body = (
      <div className="space-y-6">
        {query.data.subscriptions.length > 0 ? (
          <DataTable
            ariaLabel="Subscriptions"
            columns={subscriptions}
            data={query.data.subscriptions}
            emptyState={null}
            scope="subscriptions"
            total={query.data.subscriptions.length}
          />
        ) : null}
        {query.data.slots.length > 0 ? (
          <div className="space-y-2">
            <p className="text-muted-foreground text-sm">
              Decoding of each logical slot, averaged over the last {minutes} minutes.
            </p>
            <DataTable
              ariaLabel="Logical slots"
              columns={slots}
              data={query.data.slots}
              emptyState={null}
              scope="slots"
              total={query.data.slots.length}
            />
          </div>
        ) : null}
      </div>
    )
  }

  return (
    <Section
      id="logical-replication"
      title="Logical replication"
      description="Subscriptions this instance applies, and the logical slots it decodes for. Connection strings are never read."
    >
      {body}
    </Section>
  )
}
