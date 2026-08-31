import { useMemo } from 'react'

import type { ApiFailure } from '@/api/client'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { EmptyState } from '@/components/state/EmptyState'
import { ErrorState } from '@/components/state/ErrorState'
import type { components } from '@/api/generated'

type DriftEntry = components['schemas']['DriftEntry']

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

function makeRow(entry: DriftEntry): DriftTableRow {
  const values = Object.fromEntries(
    entry.values.map((value) => [value.instance_id, value.value]),
  ) as Record<string, string | null>
  const distinctValues = new Set(entry.values.map((value) => value.value))
  const referenceValue = entry.values[0]?.value
  const differingInstances = new Set(
    distinctValues.size > 1
      ? entry.values
          .filter((value) => value.value !== referenceValue)
          .map((value) => value.instance_id)
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
