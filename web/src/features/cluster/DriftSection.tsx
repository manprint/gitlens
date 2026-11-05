import { useMemo } from 'react'

import type { ApiFailure } from '@/api/client'
import {
  DataTable,
  type DataTableProps,
  type DataTableToolbar,
} from '@/components/layout/DataTable'
import { EmptyState } from '@/components/state/EmptyState'
import { ErrorState } from '@/components/state/ErrorState'
import type { components } from '@/api/generated'
import { replicationInstanceLabel, type ReplicationInstance } from '@/lib/replication'

import { EmptyCaptioned } from './EmptyCaptioned'

type DriftEntry = components['schemas']['DriftEntry']
type DriftValue = components['schemas']['DriftValue']

interface DriftTableRow {
  name: string
  values: Readonly<Record<string, string | null>>
  differingInstances: ReadonlySet<string>
}

export interface DriftSectionProps {
  entries?: readonly DriftEntry[]
  /** The cluster's instances name the columns; without them a column shows the instance id. */
  instances?: readonly ReplicationInstance[]
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

const DRIFT_TOOLBAR: DataTableToolbar<DriftTableRow> = { searchFields: [(row) => row.name] }

function driftColumns(
  ids: readonly string[],
  instances: readonly ReplicationInstance[],
): DataTableProps<DriftTableRow>['columns'] {
  return [
    { accessorKey: 'name', header: 'Setting' },
    ...ids.map((instanceId) => ({
      id: `instance-${instanceId}`,
      accessorFn: (row: DriftTableRow) => row.values[instanceId],
      header: replicationInstanceLabel(instances, instanceId),
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

const NO_INSTANCES: readonly ReplicationInstance[] = []

export function DriftSection({
  entries = [],
  instances = NO_INSTANCES,
  failure,
  onRetry,
}: DriftSectionProps) {
  const ids = useMemo(() => instanceIds(entries), [entries])
  const rows = useMemo(() => entries.map(makeRow), [entries])
  const columns = useMemo(() => driftColumns(ids, instances), [ids, instances])
  // The server lists only the settings that differ between two or more instances that
  // reported in the last hour, so an empty list also covers a cluster of one instance.
  const alone = instances.length === 1
  const noDrift = alone ? (
    <EmptyState
      title="Nothing to compare yet"
      description="Only one instance of this cluster is monitored, so there is nothing to compare yet."
    />
  ) : (
    <EmptyState
      title="No drift detected"
      description="No setting differs between the instances of this cluster that reported in the last hour."
    />
  )

  return (
    <section aria-labelledby="settings-drift-heading">
      <h2 id="settings-drift-heading">Configuration drift</h2>
      {failure ? (
        <ErrorState
          endpoint="cluster settings drift"
          failure={failure}
          onRetry={onRetry ?? (() => undefined)}
        />
      ) : rows.length === 0 ? (
        <EmptyCaptioned ariaLabel="Configuration drift" captionId="cluster.drift-table">
          <div role="status">{noDrift}</div>
        </EmptyCaptioned>
      ) : (
        <DataTable
          ariaLabel="Configuration drift"
          captionId="cluster.drift-table"
          tableId="configuration-drift"
          toolbar={DRIFT_TOOLBAR}
          columns={columns}
          data={rows}
          emptyState={noDrift}
        />
      )}
    </section>
  )
}
