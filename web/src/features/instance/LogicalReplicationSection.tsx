import { useMemo } from 'react'

import { useInstanceLogicalReplication } from '@/api/queries'
import type { Schemas } from '@/api/types'
import {
  DataTable,
  type DataTableProps,
  type DataTableToolbar,
} from '@/components/layout/DataTable'
import { HelpLink } from '@/components/layout/HelpLink'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
import {
  formatBytes,
  formatCount,
  formatRate,
  formatTimestamp,
  TIMESTAMP_COLUMN_WIDTH,
} from '@/lib/format'

import { InstancePanel, InstanceViewTabs, ToolbarNote } from './InstancePanel'
import { LOGICAL_VIEW_IDS } from './instanceSections'
import { useInstanceView, type InstanceView } from './instanceViews'

type Subscription = Schemas['Subscription']
type LogicalSlot = Schemas['LogicalSlotStats']

/** How long an apply worker may hear nothing from its publisher, as in the advisor. */
const SUBSCRIPTION_SILENT_MS = 5 * 60 * 1000
/** Spill rate from which a slot is marked, as in the advisor. */
const SPILL_BYTES_PER_SECOND = 1024 * 1024

const SUBSCRIPTION_STATES = ['disabled', 'not running', 'silent', 'running'] as const
type SubscriptionState = (typeof SUBSCRIPTION_STATES)[number]

const SUBSCRIPTION_STATE_KEYS: Record<SubscriptionState, MessageKey> = {
  disabled: 'instance.logical.state.disabled',
  'not running': 'instance.logical.state.notRunning',
  silent: 'instance.logical.state.silent',
  running: 'instance.logical.state.running',
}

/** The state the State column shows, also what its filter matches. */
function subscriptionStateLabel(sub: Subscription, readAt: number): SubscriptionState {
  if (!sub.enabled) return 'disabled'
  if (!sub.apply_worker) return 'not running'
  if (sub.last_message_at !== null) {
    const silent = readAt - new Date(sub.last_message_at).getTime()
    if (silent >= SUBSCRIPTION_SILENT_MS) return 'silent'
  }
  return 'running'
}

function subscriptionToolbar(readAt: number, t: Translate): DataTableToolbar<Subscription> {
  return {
    searchFields: [(sub) => sub.name, (sub) => sub.publications.join(', '), (sub) => sub.slot_name],
    filters: [
      {
        id: 'state',
        label: t('instance.logical.col.state'),
        kind: 'select',
        options: SUBSCRIPTION_STATES.map((value) => ({
          value,
          label: t(SUBSCRIPTION_STATE_KEYS[value]),
        })),
        accessor: (sub) => subscriptionStateLabel(sub, readAt),
      },
      {
        id: 'database',
        label: t('instance.logical.col.database'),
        kind: 'select',
        accessor: (sub) => sub.database,
      },
    ],
  }
}

const SLOT_TOOLBAR: DataTableToolbar<LogicalSlot> = { searchFields: [(slot) => slot.slot_name] }

function subscriptionState(sub: Subscription, readAt: number, t: Translate) {
  const state = subscriptionStateLabel(sub, readAt)
  if (state === 'disabled')
    return <Badge variant="secondary">{t('instance.logical.state.disabled')}</Badge>
  if (state === 'not running')
    return <Badge variant="destructive">{t('instance.logical.state.notRunning')}</Badge>
  if (state === 'silent')
    return <Badge variant="destructive">{t('instance.logical.state.silent')}</Badge>
  return <Badge variant="outline">{t('instance.logical.state.running')}</Badge>
}

function subscriptionColumns(
  readAt: number,
  t: Translate,
): DataTableProps<Subscription>['columns'] {
  return [
    {
      accessorKey: 'name',
      header: t('instance.logical.col.subscription'),
      meta: { truncate: true },
    },
    {
      accessorKey: 'database',
      header: t('instance.logical.col.database'),
      meta: { width: '9rem', truncate: true },
    },
    {
      id: 'state',
      header: t('instance.logical.col.state'),
      meta: { width: '8rem' },
      accessorFn: (sub) => (sub.enabled ? (sub.apply_worker ? 2 : 1) : 0),
      cell: ({ row }) => subscriptionState(row.original, readAt, t),
    },
    {
      id: 'publications',
      header: t('instance.logical.col.publications'),
      accessorFn: (sub) => sub.publications.join(', '),
      meta: { truncate: true },
    },
    {
      id: 'slot',
      header: t('instance.logical.col.slot'),
      accessorFn: (sub) => sub.slot_name,
      meta: { width: '10rem', truncate: true },
      cell: ({ row }) =>
        row.original.slot_name ?? (
          <span className="text-muted-foreground">{t('instance.logical.none')}</span>
        ),
    },
    {
      id: 'last_message',
      header: t('instance.logical.col.lastMessage'),
      accessorFn: (sub) => sub.last_message_at,
      meta: { width: TIMESTAMP_COLUMN_WIDTH },
      cell: ({ row }) =>
        row.original.last_message_at === null
          ? null
          : formatTimestamp(row.original.last_message_at),
    },
    {
      id: 'errors',
      header: t('instance.logical.col.errors'),
      accessorFn: (sub) => sub.apply_errors + sub.sync_errors,
      meta: { width: '17rem', help: 'logical.errors' },
      cell: ({ row }) =>
        `${formatCount(row.original.apply_errors)} / ${formatCount(row.original.sync_errors)}`,
    },
    {
      id: 'conflicts',
      header: t('instance.logical.col.conflicts'),
      accessorFn: (sub) => sub.conflicts,
      meta: { width: '11.5rem', help: 'logical.conflicts' },
      cell: ({ row }) =>
        row.original.conflicts === null ? (
          <span className="text-muted-foreground">{t('instance.logical.notAvailable')}</span>
        ) : (
          formatCount(row.original.conflicts)
        ),
    },
  ]
}

function perSecond(bytes: number): string {
  return `${formatBytes(bytes)}/s`
}

function slotColumns(t: Translate): DataTableProps<LogicalSlot>['columns'] {
  return [
    { accessorKey: 'slot_name', header: t('instance.logical.col.slot'), meta: { truncate: true } },
    {
      accessorKey: 'bytes_per_second',
      header: t('instance.logical.col.decoded'),
      cell: ({ row }) => perSecond(row.original.bytes_per_second),
    },
    {
      accessorKey: 'txns_per_second',
      header: t('instance.logical.col.transactions'),
      cell: ({ row }) => formatRate(row.original.txns_per_second),
    },
    {
      accessorKey: 'spill_bytes_per_second',
      header: t('instance.logical.col.spilled'),
      cell: ({ row }) =>
        row.original.spill_bytes_per_second >= SPILL_BYTES_PER_SECOND ? (
          <Badge variant="secondary">{perSecond(row.original.spill_bytes_per_second)}</Badge>
        ) : (
          perSecond(row.original.spill_bytes_per_second)
        ),
    },
    {
      accessorKey: 'stream_bytes_per_second',
      header: t('instance.logical.col.streamed'),
      cell: ({ row }) => perSecond(row.original.stream_bytes_per_second),
    },
  ]
}

export function LogicalReplicationSection({ instanceId }: { instanceId: string }) {
  const t = useT()
  const query = useInstanceLogicalReplication(instanceId)
  // Silence is judged against when the data was read, not the clock.
  const readAt = query.dataUpdatedAt
  const subscriptions = useMemo(() => subscriptionColumns(readAt, t), [readAt, t])
  const subscriptionFilters = useMemo(() => subscriptionToolbar(readAt, t), [readAt, t])
  const slots = useMemo(() => slotColumns(t), [t])
  // One table per view (15.3): only the lists the instance has are offered.
  const views: InstanceView<(typeof LOGICAL_VIEW_IDS)[number]>[] = []
  if ((query.data?.subscriptions.length ?? 0) > 0) {
    views.push({ id: 'subscriptions', label: t('instance.logical.subscriptions') })
  }
  if ((query.data?.slots.length ?? 0) > 0)
    views.push({ id: 'slots', label: t('instance.logical.slots') })
  const [view, showView] = useInstanceView(views)

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
      <section aria-busy="true" aria-label={t('instance.logical.loadingAria')} role="status">
        {t('instance.logical.loading')}
      </section>
    )
  } else if (query.data.subscriptions.length === 0 && query.data.slots.length === 0) {
    body = (
      <EmptyState
        title={t('instance.logical.noReplication')}
        description={t('instance.logical.noReplicationDescription')}
      />
    )
  } else {
    const minutes = Math.round(query.data.window_seconds / 60)
    body = (
      <>
        <TabsContent value="subscriptions">
          <DataTable
            ariaLabel={t('instance.logical.subscriptions')}
            captionId="logical.subscriptions"
            tableId="subscriptions"
            toolbar={subscriptionFilters}
            columns={subscriptions}
            data={query.data.subscriptions}
            emptyState={null}
            scope={t('instance.logical.noun.subscriptions')}
            total={query.data.subscriptions.length}
          />
        </TabsContent>
        <TabsContent value="slots">
          <DataTable
            ariaLabel={t('instance.logical.slots')}
            captionId="logical.slots"
            tableId="logical-slots"
            toolbar={SLOT_TOOLBAR}
            toolbarExtra={
              <ToolbarNote>{t('instance.logical.decodingNote', { minutes })}</ToolbarNote>
            }
            columns={slots}
            data={query.data.slots}
            emptyState={null}
            scope={t('instance.logical.noun.slots')}
            total={query.data.slots.length}
          />
        </TabsContent>
      </>
    )
  }

  return (
    <Tabs onValueChange={showView} value={view ?? ''}>
      <InstancePanel
        id="logical-replication"
        title={t('instance.logical.section')}
        captionId="logical.section"
        actions={
          <>
            {views.length > 1 ? (
              <InstanceViewTabs label={t('instance.logical.viewsLabel')} views={views} />
            ) : null}
            <HelpLink anchor="instance-overview#logical" label={t('instance.logical.section')} />
          </>
        }
      >
        {body}
      </InstancePanel>
    </Tabs>
  )
}
