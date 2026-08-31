import { useMemo } from 'react'

import type { ApiFailure } from '@/api/client'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { EmptyState } from '@/components/state/EmptyState'
import { ErrorState } from '@/components/state/ErrorState'
import { formatBytes } from '@/lib/format/bytes'
import { formatDuration } from '@/lib/format/duration'

export const DEFAULT_RETAINED_BYTES_THRESHOLD = 1024 ** 3
const RETENTION_WARNING = "A disconnected standby retains WAL and can fill the primary's disk."

export interface SlotRow {
  name: string
  owningInstance: string
  active: boolean | null
  walStatus: string | null
  retainedBytes: number | null
  inactiveAgeSeconds: number | null
}

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

function sortSlots(slots: readonly SlotRow[]): SlotRow[] {
  const rank = (active: boolean | null) => (active === false ? 0 : active === true ? 1 : 2)
  return [...slots].sort((left, right) => {
    const rankDifference = rank(left.active) - rank(right.active)
    return rankDifference || left.name.localeCompare(right.name)
  })
}

function slotColumns(threshold: number): DataTableProps<SlotRow>['columns'] {
  return [
    { accessorKey: 'name', header: 'Slot name' },
    { accessorKey: 'owningInstance', header: 'Owning instance' },
    {
      accessorKey: 'active',
      header: 'Active state',
      cell: ({ getValue }) => activeLabel(getValue<boolean | null>()),
    },
    {
      id: 'walStatus',
      accessorKey: 'walStatus',
      header: 'WAL status',
      cell: ({ row }) => walStatusLabel(row.original),
    },
    {
      accessorKey: 'retainedBytes',
      header: 'Retained bytes',
      cell: ({ getValue }) => {
        const retainedBytes = getValue<number | null>()
        const warning = retainedBytes !== null && retainedBytes > threshold
        return (
          <span
            className={warning ? 'bg-warning/20 font-semibold' : undefined}
            data-retention-warning={warning ? 'true' : undefined}
            title={warning ? RETENTION_WARNING : undefined}
          >
            {formatBytes(retainedBytes)}
            {warning ? ` — ${RETENTION_WARNING}` : null}
          </span>
        )
      },
    },
    {
      accessorKey: 'inactiveAgeSeconds',
      header: 'Age inactive',
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
  const sortedSlots = useMemo(() => sortSlots(slots), [slots])
  const columns = useMemo(() => slotColumns(retainedBytesThreshold), [retainedBytesThreshold])

  return (
    <section aria-labelledby="replication-slots-heading">
      <h2 id="replication-slots-heading">Replication slots</h2>
      {failure ? (
        <ErrorState
          endpoint="replication slots"
          failure={failure}
          onRetry={onRetry ?? (() => undefined)}
        />
      ) : (
        <DataTable
          ariaLabel="Replication slots"
          columns={columns}
          data={sortedSlots}
          emptyState={
            <EmptyState
              title="No replication slots"
              description="No replication slots were reported for this cluster."
            />
          }
        />
      )}
    </section>
  )
}
