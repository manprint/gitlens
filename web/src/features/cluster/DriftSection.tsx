import { useMemo } from 'react'

import type { ApiFailure } from '@/api/client'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { EmptyState } from '@/components/state/EmptyState'
import { ErrorState } from '@/components/state/ErrorState'
import type { components } from '@/api/generated'

type DriftEntry = components['schemas']['DriftEntry']
type DriftValue = components['schemas']['DriftValue']

interface DriftTableRow {
  name: string
  values: Readonly<Record<string, string | null>>
  differingInstances: ReadonlySet<string>
}

export interface DriftSectionProps {
  entries?: readonly DriftEntry[]
  failure?: ApiFailure
  onRetry?: () => void
}

function instanceIds(entries: readonly DriftEntry[]): string[] {
  return [
    ...new Set(entries.flatMap((entry) => entry.values.map((value) => value.instance_id))),
  ].sort((left, right) => left.localeCompare(right))
}

/**
 * The reference is the value most instances report, so the outlier is the one
 * flagged. A tie goes to the primary's value, which is what replicas are
 * compared against. Without a primary, the lowest instance_id decides, so the
 * flags never depend on the order the response happens to list instances in.
 */
function referenceValue(values: readonly DriftValue[]): string | undefined {
  const byInstance = [...values].sort((left, right) =>
    left.instance_id.localeCompare(right.instance_id),
  )
  // Counting in instance order keeps a tie between two non-primary values
  // independent of the response order too.
  const counts = new Map<string, number>()
  for (const value of byInstance) {
    counts.set(value.value, (counts.get(value.value) ?? 0) + 1)
  }
  let reference = values.find((value) => value.role === 'primary')?.value ?? byInstance[0]?.value
  let referenceCount = reference === undefined ? 0 : (counts.get(reference) ?? 0)
  for (const [value, count] of counts) {
    if (count > referenceCount) {
      reference = value
      referenceCount = count
    }
  }
  return reference
}

function makeRow(entry: DriftEntry): DriftTableRow {
  const values = Object.fromEntries(
    entry.values.map((value) => [value.instance_id, value.value]),
  ) as Record<string, string | null>
  const distinctValues = new Set(entry.values.map((value) => value.value))
  const reference = referenceValue(entry.values)
  const differingInstances = new Set(
    distinctValues.size > 1
      ? entry.values.filter((value) => value.value !== reference).map((value) => value.instance_id)
      : [],
  )
  return { differingInstances, name: entry.name, values }
}

function driftColumns(ids: readonly string[]): DataTableProps<DriftTableRow>['columns'] {
  return [
    { accessorKey: 'name', header: 'Setting' },
    ...ids.map((instanceId) => ({
      id: `instance-${instanceId}`,
      accessorFn: (row: DriftTableRow) => row.values[instanceId],
      header: instanceId,
      cell: ({ row }: { row: { original: DriftTableRow } }) => {
        const differing = row.original.differingInstances.has(instanceId)
        const value = row.original.values[instanceId] ?? 'Unknown'
        return (
          <span
            aria-label={differing ? `${instanceId} differs from the reference` : undefined}
            className={differing ? 'bg-warning/20 font-semibold' : undefined}
            data-drift-different={differing ? 'true' : undefined}
          >
            {value}
          </span>
        )
      },
    })),
  ]
}

export function DriftSection({ entries = [], failure, onRetry }: DriftSectionProps) {
  const ids = useMemo(() => instanceIds(entries), [entries])
  const rows = useMemo(() => entries.map(makeRow), [entries])
  const columns = useMemo(() => driftColumns(ids), [ids])

  return (
    <section aria-labelledby="settings-drift-heading">
      <h2 id="settings-drift-heading">Configuration drift</h2>
      {failure ? (
        <ErrorState
          endpoint="cluster settings drift"
          failure={failure}
          onRetry={onRetry ?? (() => undefined)}
        />
      ) : (
        <DataTable
          ariaLabel="Configuration drift"
          columns={columns}
          data={rows}
          emptyState={
            <EmptyState
              title="No drift detected"
              description="All observed settings match across the cluster instances."
            />
          }
        />
      )}
    </section>
  )
}
