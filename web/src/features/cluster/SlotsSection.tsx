import { useMemo } from 'react'

import type { ApiFailure } from '@/api/client'
import {
  DataTable,
  type DataTableProps,
  type DataTableToolbar,
} from '@/components/layout/DataTable'
import { HelpLink } from '@/components/layout/HelpLink'
import { EmptyState } from '@/components/state/EmptyState'
import { ErrorState } from '@/components/state/ErrorState'
import { useT, type Translate } from '@/i18n/react'
import { formatBytes } from '@/lib/format/bytes'
import { formatDuration } from '@/lib/format/duration'
import type { SlotRow } from '@/lib/replication'

import { EmptyCaptioned } from './EmptyCaptioned'

export type { SlotRow } from '@/lib/replication'

export const DEFAULT_RETAINED_BYTES_THRESHOLD = 1024 ** 3

export interface SlotsSectionProps {
  slots?: readonly SlotRow[]
  failure?: ApiFailure
  onRetry?: () => void
  retainedBytesThreshold?: number
}

function activeLabel(active: boolean | null): string {
  if (active === true) return 'Active'
  if (active === false) return 'Inactive'
  return 'Unknown'
}

function walStatusLabel(slot: SlotRow): string {
  if (slot.walStatus) return slot.walStatus
  if (slot.active === false) return 'Retaining WAL'
  if (slot.active === true) return 'Streaming'
  return 'Unknown'
}

function activeText(active: boolean | null, t: Translate): string {
  if (active === true) return t('cluster.slots.stateActive')
  if (active === false) return t('cluster.slots.stateInactive')
  return t('cluster.common.unknown')
}

function walStatusText(slot: SlotRow, t: Translate): string {
  if (slot.walStatus) return slot.walStatus
  if (slot.active === false) return t('cluster.slots.walRetaining')
  if (slot.active === true) return t('cluster.slots.walStreaming')
  return t('cluster.common.unknown')
}

// Accessors and option values stay English: they are the filter keys, the labels are translated.
function slotsToolbar(t: Translate): DataTableToolbar<SlotRow> {
  return {
    searchFields: [(slot) => slot.name, (slot) => slot.owningInstance],
    filters: [
      {
        id: 'type',
        label: t('cluster.slots.filterType'),
        kind: 'select',
        accessor: (slot) => slot.slotType ?? 'Unknown',
      },
      {
        id: 'active',
        label: t('cluster.slots.filterActive'),
        kind: 'select',
        options: [
          { value: 'active', label: t('cluster.slots.stateActive') },
          { value: 'inactive', label: t('cluster.slots.stateInactive') },
          { value: 'unknown', label: t('cluster.common.unknown') },
        ],
        accessor: (slot) => activeLabel(slot.active).toLowerCase(),
      },
      { id: 'wal', label: t('cluster.slots.filterWal'), kind: 'select', accessor: walStatusLabel },
    ],
  }
}

function sortSlots(slots: readonly SlotRow[]): SlotRow[] {
  const rank = (active: boolean | null) => (active === false ? 0 : active === true ? 1 : 2)
  return [...slots].sort((left, right) => {
    const rankDifference = rank(left.active) - rank(right.active)
    return rankDifference || left.name.localeCompare(right.name)
  })
}

function slotColumns(threshold: number, t: Translate): DataTableProps<SlotRow>['columns'] {
  return [
    { accessorKey: 'name', header: t('cluster.slots.colName') },
    { accessorKey: 'owningInstance', header: t('cluster.slots.colInstance') },
    {
      accessorKey: 'slotType',
      header: t('cluster.slots.filterType'),
      cell: ({ getValue }) => getValue<string | null>() ?? t('cluster.common.unknown'),
    },
    {
      accessorKey: 'active',
      header: t('cluster.slots.filterActive'),
      cell: ({ getValue }) => activeText(getValue<boolean | null>(), t),
    },
    {
      id: 'walStatus',
      accessorKey: 'walStatus',
      header: t('cluster.slots.filterWal'),
      cell: ({ row }) => walStatusText(row.original, t),
    },
    {
      accessorKey: 'retainedBytes',
      header: t('cluster.slots.colRetained'),
      meta: { help: 'slots.retained-bytes' },
      cell: ({ getValue }) => {
        const retainedBytes = getValue<number | null>()
        const warning = retainedBytes !== null && retainedBytes > threshold
        return (
          <span
            className={warning ? 'bg-warning/20 font-semibold' : undefined}
            data-retention-warning={warning ? 'true' : undefined}
            title={warning ? t('cluster.slots.retentionWarning') : undefined}
          >
            {formatBytes(retainedBytes)}
            {warning ? ` — ${t('cluster.slots.retentionWarning')}` : null}
          </span>
        )
      },
    },
    {
      accessorKey: 'safeWalSizeBytes',
      header: t('cluster.slots.colSafeWal'),
      meta: { help: 'slots.wal-before-invalidation' },
      // null means max_slot_wal_keep_size is unlimited (the slot is never
      // invalidated, and can retain WAL until the disk is full), or that the
      // slot has already lost WAL it needs: PostgreSQL reports NULL for both.
      cell: ({ row, getValue }) => {
        const safe = getValue<number | null>()
        if (safe !== null) return formatBytes(safe)
        return row.original.walStatus === 'lost'
          ? t('cluster.slots.safeWalLost')
          : t('cluster.slots.unlimited')
      },
    },
    {
      accessorKey: 'inactiveAgeSeconds',
      header: t('cluster.slots.colAge'),
      meta: { help: 'slots.age-inactive' },
      cell: ({ row, getValue }) =>
        row.original.active === true ? '—' : formatDuration(getValue<number | null>()),
    },
  ]
}

export function SlotsSection({
  slots = [],
  failure,
  onRetry,
  retainedBytesThreshold = DEFAULT_RETAINED_BYTES_THRESHOLD,
}: SlotsSectionProps) {
  const t = useT()
  const sortedSlots = useMemo(() => sortSlots(slots), [slots])
  const columns = useMemo(() => slotColumns(retainedBytesThreshold, t), [retainedBytesThreshold, t])
  const toolbar = useMemo(() => slotsToolbar(t), [t])
  const noSlots = (
    <EmptyState
      title={t('cluster.slots.noneTitle')}
      description={t('cluster.slots.noneDescription')}
    />
  )

  return (
    <section aria-labelledby="replication-slots-heading">
      <div className="flex flex-wrap items-center gap-2">
        <h2 id="replication-slots-heading">{t('cluster.slots.heading')}</h2>
        <HelpLink anchor="cluster#slots" label={t('cluster.slots.helpLabel')} />
      </div>
      {failure ? (
        <ErrorState
          endpoint={t('cluster.endpoint.slots')}
          failure={failure}
          onRetry={onRetry ?? (() => undefined)}
        />
      ) : sortedSlots.length === 0 ? (
        <EmptyCaptioned ariaLabel={t('cluster.slots.heading')} captionId="cluster.slots-table">
          <div role="status">{noSlots}</div>
        </EmptyCaptioned>
      ) : (
        <DataTable
          ariaLabel={t('cluster.slots.heading')}
          captionId="cluster.slots-table"
          tableId="replication-slots"
          toolbar={toolbar}
          columns={columns}
          data={sortedSlots}
          emptyState={noSlots}
        />
      )}
    </section>
  )
}
