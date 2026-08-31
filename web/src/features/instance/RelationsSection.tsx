import { useMemo, useState, type ReactNode } from 'react'

import type { ApiFailure } from '@/api/client'
import { useInstanceBloat, useInstanceIndexes, useInstanceTables } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState, Truncated, Unknown } from '@/components/state'
import { formatBytes, formatCount, formatPercent, formatTimestamp } from '@/lib/format'

const RELATION_LIMIT = 50

type RelationResponse = Schemas['RelationResponse']
type RelationItem = RelationResponse['items'][number]

interface RelationColumn {
  key: string
  label: string
  render: (item: RelationItem) => ReactNode
  sortValue: (item: RelationItem) => string | number | null
}

interface RelationQueryState {
  data: RelationResponse | undefined
  error: ApiFailure | null
  refetch: () => unknown
}

function valueOf(item: RelationItem, key: string): unknown {
  return item[key]
}

function stringValue(item: RelationItem, key: string): string | null {
  const value = valueOf(item, key)
  return typeof value === 'string' && value.length > 0 ? value : null
}

function numberValue(item: RelationItem, key: string): number | null {
  const value = valueOf(item, key)
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function booleanValue(item: RelationItem, key: string): boolean | null {
  const value = valueOf(item, key)
  return typeof value === 'boolean' ? value : null
}

function relationName(item: RelationItem): string | null {
  const schema = stringValue(item, 'schemaname')
  const relation = stringValue(item, 'relname')
  if (!relation) return null
  return schema ? `${schema}.${relation}` : relation
}

function relationCell(item: RelationItem): ReactNode {
  return relationName(item) ?? <Unknown reason="The relation name was not reported." />
}

function textCell(item: RelationItem, key: string): ReactNode {
  return stringValue(item, key) ?? <Unknown />
}

function countCell(item: RelationItem, key: string): ReactNode {
  const value = numberValue(item, key)
  return value === null ? <Unknown /> : formatCount(value)
}

function bytesCell(item: RelationItem, key: string): ReactNode {
  const value = numberValue(item, key)
  return value === null ? <Unknown /> : formatBytes(value)
}

function percentCell(item: RelationItem, key: string): ReactNode {
  const value = numberValue(item, key)
  if (value === null) return <Unknown />
  return formatPercent(value, 1) ?? <Unknown />
}

function booleanCell(item: RelationItem, key: string): ReactNode {
  const value = booleanValue(item, key)
  return value === null ? <Unknown /> : value ? 'yes' : 'no'
}

function timestampCell(item: RelationItem): ReactNode {
  const value = stringValue(item, 'ts')
  return value ? formatTimestamp(value, 'UTC') : <Unknown />
}

function compareValues(left: string | number | null, right: string | number | null): number {
  if (left === right) return 0
  if (left === null) return 1
  if (right === null) return -1
  if (typeof left === 'number' && typeof right === 'number') return left - right
  return String(left).localeCompare(String(right), undefined, {
    numeric: true,
    sensitivity: 'base',
  })
}

function SortableRelationTable({
  caption,
  items,
  columns,
}: {
  caption: string
  items: RelationItem[]
  columns: RelationColumn[]
}) {
  const [sortKey, setSortKey] = useState(columns[0]?.key ?? '')
  const [ascending, setAscending] = useState(false)
  const sortedItems = useMemo(() => {
    const column = columns.find((candidate) => candidate.key === sortKey) ?? columns[0]
    if (!column) return items
    return [...items].sort((left, right) => {
      const result = compareValues(column.sortValue(left), column.sortValue(right))
      return ascending ? result : -result
    })
  }, [ascending, columns, items, sortKey])

  function toggleSort(column: RelationColumn) {
    if (column.key === sortKey) {
      setAscending((current) => !current)
      return
    }
    setSortKey(column.key)
    setAscending(true)
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr className="text-muted-foreground border-b">
            {columns.map((column) => {
              const selected = column.key === sortKey
              return (
                <th key={column.key} scope="col" className="px-3 py-2 font-medium">
                  <button
                    type="button"
                    className="whitespace-nowrap hover:underline"
                    aria-label={`Sort ${caption} by ${column.label}`}
                    aria-pressed={selected}
                    onClick={() => toggleSort(column)}
                  >
                    {column.label}
                    {selected ? (ascending ? ' ↑' : ' ↓') : null}
                  </button>
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>
          {sortedItems.map((item, index) => (
            <tr
              key={`${relationName(item) ?? 'relation'}-${index}`}
              className="border-b last:border-0"
            >
              {columns.map((column, columnIndex) =>
                columnIndex === 0 ? (
                  <th
                    key={column.key}
                    scope="row"
                    className="px-3 py-2 text-left align-top font-normal"
                  >
                    {column.render(item)}
                  </th>
                ) : (
                  <td key={column.key} className="px-3 py-2 align-top">
                    {column.render(item)}
                  </td>
                ),
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

const TABLE_COLUMNS: RelationColumn[] = [
  {
    key: 'relation',
    label: 'Relation',
    render: relationCell,
    sortValue: (item) => relationName(item),
  },
  {
    key: 'live_tuples',
    label: 'Live tuples',
    render: (item) => countCell(item, 'n_live_tup'),
    sortValue: (item) => numberValue(item, 'n_live_tup'),
  },
  {
    key: 'dead_tuples',
    label: 'Dead tuples',
    render: (item) => countCell(item, 'n_dead_tup'),
    sortValue: (item) => numberValue(item, 'n_dead_tup'),
  },
  {
    key: 'dead_ratio',
    label: 'Dead ratio',
    render: (item) => percentCell(item, 'dead_ratio'),
    sortValue: (item) => numberValue(item, 'dead_ratio'),
  },
  {
    key: 'size',
    label: 'Size',
    render: (item) => {
      const value = numberValue(item, 'total_bytes') ?? numberValue(item, 'size_bytes')
      return value === null ? <Unknown /> : formatBytes(value)
    },
    sortValue: (item) => numberValue(item, 'total_bytes') ?? numberValue(item, 'size_bytes'),
  },
  {
    key: 'seq_scan',
    label: 'Sequential scans',
    render: (item) => countCell(item, 'seq_scan'),
    sortValue: (item) => numberValue(item, 'seq_scan'),
  },
  {
    key: 'observed',
    label: 'Observed',
    render: timestampCell,
    sortValue: (item) => stringValue(item, 'ts'),
  },
]

const INDEX_COLUMNS: RelationColumn[] = [
  {
    key: 'relation',
    label: 'Relation',
    render: relationCell,
    sortValue: (item) => relationName(item),
  },
  {
    key: 'index',
    label: 'Index',
    render: (item) => textCell(item, 'indexrelname'),
    sortValue: (item) => stringValue(item, 'indexrelname'),
  },
  {
    key: 'scans',
    label: 'Index scans',
    render: (item) => countCell(item, 'idx_scan'),
    sortValue: (item) => numberValue(item, 'idx_scan'),
  },
  {
    key: 'size',
    label: 'Size',
    render: (item) => bytesCell(item, 'index_bytes'),
    sortValue: (item) => numberValue(item, 'index_bytes'),
  },
  {
    key: 'unique',
    label: 'Unique',
    render: (item) => booleanCell(item, 'is_unique'),
    sortValue: (item) => {
      const value = booleanValue(item, 'is_unique')
      return value === null ? null : value ? 1 : 0
    },
  },
  {
    key: 'primary',
    label: 'Primary',
    render: (item) => booleanCell(item, 'is_primary'),
    sortValue: (item) => {
      const value = booleanValue(item, 'is_primary')
      return value === null ? null : value ? 1 : 0
    },
  },
  {
    key: 'valid',
    label: 'Valid',
    render: (item) => booleanCell(item, 'is_valid'),
    sortValue: (item) => {
      const value = booleanValue(item, 'is_valid')
      return value === null ? null : value ? 1 : 0
    },
  },
  {
    key: 'observed',
    label: 'Observed',
    render: timestampCell,
    sortValue: (item) => stringValue(item, 'ts'),
  },
]

const BLOAT_COLUMNS: RelationColumn[] = [
  {
    key: 'relation',
    label: 'Relation',
    render: relationCell,
    sortValue: (item) => relationName(item),
  },
  {
    key: 'object',
    label: 'Object',
    render: (item) => textCell(item, 'indexrelname'),
    sortValue: (item) => stringValue(item, 'indexrelname'),
  },
  {
    key: 'real_bytes',
    label: 'Real size',
    render: (item) => bytesCell(item, 'real_bytes'),
    sortValue: (item) => numberValue(item, 'real_bytes'),
  },
  {
    key: 'expected_bytes',
    label: 'Expected size',
    render: (item) => bytesCell(item, 'expected_bytes'),
    sortValue: (item) => numberValue(item, 'expected_bytes'),
  },
  {
    key: 'bloat_bytes',
    label: 'Bloat',
    render: (item) => bytesCell(item, 'bloat_bytes'),
    sortValue: (item) => numberValue(item, 'bloat_bytes'),
  },
  {
    key: 'bloat_ratio',
    label: 'Bloat ratio',
    render: (item) => percentCell(item, 'bloat_ratio'),
    sortValue: (item) => numberValue(item, 'bloat_ratio'),
  },
  {
    key: 'method',
    label: 'Method',
    render: (item) => textCell(item, 'method'),
    sortValue: (item) => stringValue(item, 'method'),
  },
  {
    key: 'observed',
    label: 'Observed',
    render: timestampCell,
    sortValue: (item) => stringValue(item, 'ts'),
  },
]

function BudgetNotice({ response, scope }: { response: RelationResponse; scope: string }) {
  if (!response.truncated) return null
  return (
    <div className="space-y-1">
      <Truncated shown={response.items.length} budget={RELATION_LIMIT} scope={scope} />
      <p className="text-muted-foreground text-sm">
        This budget is shared per instance across databases. Change it at{' '}
        <code>checks.table_stats.top_n</code>.
      </p>
    </div>
  )
}

function RelationPanel({
  title,
  endpoint,
  query,
  columns,
  emptyDescription,
  scope,
}: {
  title: string
  endpoint: string
  query: RelationQueryState
  columns: RelationColumn[]
  emptyDescription: string
  scope: string
}) {
  if (query.error && !query.data) {
    return (
      <ErrorState endpoint={endpoint} failure={query.error} onRetry={() => void query.refetch()} />
    )
  }
  if (!query.data) {
    return (
      <div role="status" aria-label={`Loading ${title}`}>
        Loading {title.toLowerCase()}…
      </div>
    )
  }
  if (query.data.items.length === 0) {
    return <EmptyState title={`No ${title.toLowerCase()}`} description={emptyDescription} />
  }
  return (
    <div className="space-y-3">
      <SortableRelationTable caption={title} items={query.data.items} columns={columns} />
      <BudgetNotice response={query.data} scope={scope} />
    </div>
  )
}

function ExactBloatAction({
  instanceId,
  available,
}: {
  instanceId: string
  available: boolean | null
}) {
  if (available === null) {
    return <div role="status">Checking whether pgstattuple is available…</div>
  }
  if (available) {
    return (
      <a
        className="text-primary underline"
        href={`/instances/${instanceId}/queries?command=pgstattuple`}
      >
        Run exact bloat with pgstattuple in Query Inspector
      </a>
    )
  }
  return (
    <div role="status" className="border-muted space-y-2 border p-3 text-sm">
      <p>
        Exact bloat is unavailable because the <code>pgstattuple</code> extension is absent. pglens
        never installs extensions.
      </p>
      <button
        type="button"
        disabled
        aria-label="Exact bloat unavailable because pgstattuple is absent"
      >
        Exact bloat unavailable
      </button>
    </div>
  )
}

export function RelationsSection({ instanceId }: { instanceId: string }) {
  const tablesQuery = useInstanceTables(instanceId, { limit: RELATION_LIMIT })
  const indexesQuery = useInstanceIndexes(instanceId, { limit: RELATION_LIMIT })
  const bloatQuery = useInstanceBloat(instanceId, { limit: RELATION_LIMIT })
  const exactBloatAvailable = bloatQuery.data
    ? bloatQuery.data.items.some((item) => stringValue(item, 'method') === 'pgstattuple')
    : null

  return (
    <Section
      title="Relations, bloat and truncation"
      description={
        'Tables and indexes use a shared per-instance top-N budget. Bloat is a statistical estimate, not a measurement; relations under 1 MiB and never-analysed relations are not estimated.'
      }
      id="instance-relations"
    >
      <div className="space-y-8">
        <div className="space-y-3">
          <h3 className="text-base font-medium">Tables</h3>
          <RelationPanel
            title="Tables"
            endpoint="table relations"
            query={tablesQuery}
            columns={TABLE_COLUMNS}
            emptyDescription="No table relations are currently reported."
            scope="tables shared per instance across databases"
          />
        </div>

        <div className="space-y-3">
          <h3 className="text-base font-medium">Indexes</h3>
          <RelationPanel
            title="Indexes"
            endpoint="index relations"
            query={indexesQuery}
            columns={INDEX_COLUMNS}
            emptyDescription="No index relations are currently reported."
            scope="indexes shared per instance across databases"
          />
        </div>

        <div className="space-y-3">
          <h3 className="text-base font-medium">Bloat estimates</h3>
          <RelationPanel
            title="Bloat estimates"
            endpoint="bloat estimates"
            query={bloatQuery}
            columns={BLOAT_COLUMNS}
            emptyDescription="No bloat estimates are currently reported."
            scope="bloat estimates shared per instance across databases"
          />
          <ExactBloatAction instanceId={instanceId} available={exactBloatAvailable} />
        </div>
      </div>
    </Section>
  )
}
