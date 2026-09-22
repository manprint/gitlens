import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type ReactNode } from 'react'

import type { ApiFailure } from '@/api/client'
import { useCreateCommand } from '@/api/commands'
import { qk } from '@/api/keys'
import { useInstanceBloat, useInstanceIndexes, useInstanceTables } from '@/api/queries'
import type { PermTier, Schemas } from '@/api/types'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState, NotPermitted, Truncated, Unknown } from '@/components/state'
import { describeCommandState, isTerminalCommandState } from '@/lib/commands'
import { formatBytes, formatCount, formatPercent, formatRate, formatTimestamp } from '@/lib/format'

const RELATION_LIMIT = 50

type RelationResponse = Schemas['RelationResponse']
type RelationItem = RelationResponse['items'][number]

interface RelationColumn {
  key: string
  label: string
  render: (item: RelationItem) => ReactNode
  /** Absent for action columns, which have nothing to sort by. */
  sortValue?: (item: RelationItem) => string | number | null
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

function rateCell(item: RelationItem, key: string): ReactNode {
  const value = numberValue(item, key)
  return value === null ? <Unknown /> : formatRate(value)
}

function timestampCell(item: RelationItem): ReactNode {
  const value = stringValue(item, 'ts')
  return value ? formatTimestamp(value, 'UTC') : <Unknown />
}

function timeValue(item: RelationItem, key: string): { iso: string; ms: number } | null {
  const iso = stringValue(item, key)
  if (!iso) return null
  const ms = new Date(iso).getTime()
  return Number.isFinite(ms) ? { iso, ms } : null
}

/**
 * The newer of a manual and an automatic maintenance time. The API reports
 * them separately, as pg_stat_user_tables does; a table only autovacuum
 * maintains has no manual time at all.
 */
function latestMaintenance(
  item: RelationItem,
  manualKey: string,
  autoKey: string,
): { iso: string; ms: number; auto: boolean } | null {
  const manual = timeValue(item, manualKey)
  const automatic = timeValue(item, autoKey)
  if (automatic && (!manual || automatic.ms > manual.ms)) return { ...automatic, auto: true }
  return manual ? { ...manual, auto: false } : null
}

function maintenanceColumn(
  key: string,
  label: string,
  manualKey: string,
  autoKey: string,
  never: string,
): RelationColumn {
  return {
    key,
    label,
    render: (item) => {
      const latest = latestMaintenance(item, manualKey, autoKey)
      if (!latest) return <Unknown reason={never} />
      return `${formatTimestamp(latest.iso, 'UTC')} (${latest.auto ? 'auto' : 'manual'})`
    },
    sortValue: (item) => latestMaintenance(item, manualKey, autoKey)?.ms ?? null,
  }
}

function isTableRow(item: RelationItem): boolean {
  const kind = stringValue(item, 'object_kind')
  return kind ? kind === 'table' : !stringValue(item, 'indexrelname')
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
    const sortValue = column?.sortValue
    if (!sortValue) return items
    return [...items].sort((left, right) => {
      const result = compareValues(sortValue(left), sortValue(right))
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
              if (!column.sortValue) {
                return (
                  <th key={column.key} scope="col" className="px-3 py-2 font-medium">
                    {column.label}
                  </th>
                )
              }
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

const RELATION_COLUMN: RelationColumn = {
  key: 'relation',
  label: 'Relation',
  render: relationCell,
  sortValue: (item) => relationName(item),
}

// The budget is shared across databases, so the same schema and table name
// can appear once per database; the database tells the rows apart.
const DATABASE_COLUMN: RelationColumn = {
  key: 'database',
  label: 'Database',
  render: (item) => textCell(item, 'datname'),
  sortValue: (item) => stringValue(item, 'datname'),
}

const OBSERVED_COLUMN: RelationColumn = {
  key: 'observed',
  label: 'Observed',
  render: timestampCell,
  sortValue: (item) => stringValue(item, 'ts'),
}

function booleanColumn(key: string, label: string, field: string): RelationColumn {
  return {
    key,
    label,
    render: (item) => booleanCell(item, field),
    sortValue: (item) => {
      const value = booleanValue(item, field)
      return value === null ? null : value ? 1 : 0
    },
  }
}

// seq_scan_rate and idx_scan_rate are scans per second over the last scrape
// interval. The columns used to read seq_scan and idx_scan, which the API
// never returns, so every scan cell rendered as not measured.
const TABLE_COLUMNS: RelationColumn[] = [
  RELATION_COLUMN,
  DATABASE_COLUMN,
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
    render: (item) => bytesCell(item, 'total_bytes'),
    sortValue: (item) => numberValue(item, 'total_bytes'),
  },
  {
    key: 'seq_scan_rate',
    label: 'Sequential scans',
    render: (item) => rateCell(item, 'seq_scan_rate'),
    sortValue: (item) => numberValue(item, 'seq_scan_rate'),
  },
  {
    key: 'idx_scan_rate',
    label: 'Index scans',
    render: (item) => rateCell(item, 'idx_scan_rate'),
    sortValue: (item) => numberValue(item, 'idx_scan_rate'),
  },
  maintenanceColumn(
    'last_vacuum',
    'Last vacuum',
    'last_vacuum_at',
    'last_autovacuum_at',
    'Never vacuumed since the statistics were reset.',
  ),
  maintenanceColumn(
    'last_analyze',
    'Last analyze',
    'last_analyze_at',
    'last_autoanalyze_at',
    'Never analysed since the statistics were reset.',
  ),
  OBSERVED_COLUMN,
]

const INDEX_COLUMNS: RelationColumn[] = [
  RELATION_COLUMN,
  DATABASE_COLUMN,
  {
    key: 'index',
    label: 'Index',
    render: (item) => textCell(item, 'indexrelname'),
    sortValue: (item) => stringValue(item, 'indexrelname'),
  },
  {
    key: 'idx_scan_rate',
    label: 'Index scans',
    render: (item) => rateCell(item, 'idx_scan_rate'),
    sortValue: (item) => numberValue(item, 'idx_scan_rate'),
  },
  {
    key: 'size',
    label: 'Size',
    render: (item) => bytesCell(item, 'index_bytes'),
    sortValue: (item) => numberValue(item, 'index_bytes'),
  },
  booleanColumn('unique', 'Unique', 'is_unique'),
  booleanColumn('primary', 'Primary', 'is_primary'),
  booleanColumn('valid', 'Valid', 'is_valid'),
  OBSERVED_COLUMN,
]

const BLOAT_COLUMNS: RelationColumn[] = [
  RELATION_COLUMN,
  DATABASE_COLUMN,
  {
    key: 'object',
    label: 'Object',
    // A table row has no index name; it used to render as not measured.
    render: (item) => stringValue(item, 'indexrelname') ?? textCell(item, 'object_kind'),
    sortValue: (item) => stringValue(item, 'indexrelname') ?? stringValue(item, 'object_kind'),
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
  OBSERVED_COLUMN,
]

function BudgetNotice({
  response,
  scope,
  configKey,
}: {
  response: RelationResponse
  scope: string
  configKey: string
}) {
  if (!response.truncated) return null
  return (
    <div className="space-y-1">
      <Truncated shown={response.items.length} budget={RELATION_LIMIT} scope={scope} />
      <p className="text-muted-foreground text-sm">
        This budget is shared per instance across databases. Change it at <code>{configKey}</code>.
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
  configKey,
}: {
  title: string
  endpoint: string
  query: RelationQueryState
  columns: RelationColumn[]
  emptyDescription: string
  scope: string
  configKey: string
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
      <BudgetNotice response={query.data} scope={scope} configKey={configKey} />
    </div>
  )
}

/**
 * Runs pgstattuple on one table through the agent. The link it replaces
 * pointed at the Query Inspector, which has no pgstattuple command, and the
 * extension was declared absent whenever no measurement had been taken yet,
 * which is always the case before the first one: the measurement could not
 * be started from anywhere. Whether the extension exists is known only to the
 * agent, which rejects the command and says so when it does not.
 */
function ExactMeasurement({
  instanceId,
  item,
  permitted,
}: {
  instanceId: string
  item: RelationItem
  permitted: boolean
}) {
  const command = useCreateCommand(instanceId)
  const queryClient = useQueryClient()
  const state = command.command.data?.state
  const schema = stringValue(item, 'schemaname')
  const relation = stringValue(item, 'relname')
  const datname = stringValue(item, 'datname')

  useEffect(() => {
    if (state === 'done') {
      void queryClient.invalidateQueries({ queryKey: qk.instanceBloat(instanceId, RELATION_LIMIT) })
    }
  }, [instanceId, queryClient, state])

  if (!isTableRow(item) || !schema || !relation) {
    return <Unknown reason="pgstattuple measures tables only." />
  }
  const name = `${schema}.${relation}`
  const running = command.isPending || (state !== undefined && !isTerminalCommandState(state))
  return (
    <div className="space-y-1">
      <button
        type="button"
        className="rounded-md border px-2 py-1 text-xs font-medium whitespace-nowrap"
        aria-label={`Measure ${name} exactly with pgstattuple`}
        disabled={!permitted || running}
        onClick={() =>
          command.mutate({
            kind: 'pgstattuple',
            args: { schema, relation, ...(datname ? { datname } : {}) },
          })
        }
      >
        Measure exactly
      </button>
      {command.command.data ? (
        <p role="status" className="text-xs">
          {describeCommandState(command.command.data.state, command.command.data.error)}
        </p>
      ) : null}
      {command.error ? (
        <p role="alert" className="text-destructive text-xs">
          Could not submit the measurement: {command.error.message}
        </p>
      ) : null}
    </div>
  )
}

function bloatColumns(instanceId: string, permitted: boolean): RelationColumn[] {
  return [
    ...BLOAT_COLUMNS,
    {
      key: 'exact',
      label: 'Exact measurement',
      render: (item) => (
        <ExactMeasurement instanceId={instanceId} item={item} permitted={permitted} />
      ),
    },
  ]
}

function ExactBloatNotice({ currentTier }: { currentTier: PermTier }) {
  return (
    <div className="text-muted-foreground space-y-2 text-sm">
      <p>
        An exact measurement runs <code>pgstattuple</code> on the agent in a read-only transaction.
        It reads the whole table (bounded at 50 seconds) and needs the extension to be installed
        already in the database that holds the table: pglens never installs extensions, and the
        agent rejects the command when it is missing there.
      </p>
      {currentTier === 'T0' ? <NotPermitted required="T1" current={currentTier} /> : null}
    </div>
  )
}

export function RelationsSection({
  instanceId,
  currentTier,
}: {
  instanceId: string
  currentTier: PermTier
}) {
  const tablesQuery = useInstanceTables(instanceId, { limit: RELATION_LIMIT })
  const indexesQuery = useInstanceIndexes(instanceId, { limit: RELATION_LIMIT })
  const bloatQuery = useInstanceBloat(instanceId, { limit: RELATION_LIMIT })
  const permitted = currentTier !== 'T0'
  const bloatTableColumns = useMemo(
    () => bloatColumns(instanceId, permitted),
    [instanceId, permitted],
  )

  return (
    <Section
      title="Relations, bloat and truncation"
      description={
        'Tables, indexes and bloat estimates each use a top-N budget per database. A bloat estimate is a statistical estimate, not a measurement: it covers tables only, and relations under 1 MiB, never-analysed relations and tables with columns the monitoring role cannot see in pg_stats are not estimated. Rows with method pgstattuple are exact measurements.'
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
            configKey="checks.table_stats.top_n"
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
            configKey="checks.index_stats.top_n"
          />
        </div>

        <div className="space-y-3">
          <h3 className="text-base font-medium">Bloat estimates</h3>
          <RelationPanel
            title="Bloat estimates"
            endpoint="bloat estimates"
            query={bloatQuery}
            columns={bloatTableColumns}
            emptyDescription="No bloat estimates are currently reported."
            scope="bloat estimates shared per instance across databases"
            configKey="checks.bloat_estimate.top_n"
          />
          <ExactBloatNotice currentTier={currentTier} />
        </div>
      </div>
    </Section>
  )
}
