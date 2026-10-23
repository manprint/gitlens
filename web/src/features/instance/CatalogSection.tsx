import { useMemo, useState, type ReactNode } from 'react'

import {
  useInstanceExtensions,
  useInstancePartitionedTables,
  useInstanceSequences,
  useInstanceSpatialColumns,
  useInstanceTriggers,
  useInstanceUnindexedForeignKeys,
  type ApiQueryResult,
} from '@/api/queries'
import type { Schemas } from '@/api/types'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState, Unknown } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  formatBytes,
  formatCount,
  formatIntegerString,
  formatPercent,
  formatTimestamp,
} from '@/lib/format'

interface CatalogSectionProps {
  instanceId: string
}

type Trigger = Schemas['Trigger']
type SpatialColumn = Schemas['SpatialColumn']
type Sequence = Schemas['Sequence']
type PartitionedTable = Schemas['PartitionedTable']

const selectClassName = 'border-input bg-background h-9 w-full rounded-md border px-3'

/** Keeps the rows of one database, or of every database for "all". */
function useDatabaseFilter<T extends { datname: string }>(items: readonly T[]) {
  const [database, setDatabase] = useState('all')
  const databases = useMemo(
    () =>
      [...new Set(items.map((item) => item.datname))].sort((left, right) =>
        left.localeCompare(right),
      ),
    [items],
  )
  const rows = useMemo(
    () => items.filter((item) => database === 'all' || item.datname === database),
    [database, items],
  )
  return { database, databases, rows, setDatabase }
}

function DatabaseFilter({
  id,
  databases,
  value,
  onChange,
}: {
  id: string
  databases: readonly string[]
  value: string
  onChange: (value: string) => void
}) {
  return (
    <label className="space-y-1 text-sm" htmlFor={id}>
      <span className="font-medium">Database</span>
      <select
        className={selectClassName}
        id={id}
        onChange={(event) => onChange(event.target.value)}
        value={value}
      >
        <option value="all">All databases</option>
        {databases.map((name) => (
          <option key={name} value={name}>
            {name}
          </option>
        ))}
      </select>
    </label>
  )
}

/** Names the databases whose latest list was cut at the agent's limit. */
function TruncatedNotice({
  truncated,
  database,
  what,
  kept,
}: {
  truncated: readonly string[]
  database: string
  what: string
  kept: string
}) {
  const names = truncated.filter((name) => database === 'all' || name === database)
  if (names.length === 0) return null
  return (
    <p role="status" className="text-muted-foreground text-sm">
      {names.join(', ')} {names.length === 1 ? 'has' : 'have'} more {what} than the agent reports:{' '}
      {kept}.
    </p>
  )
}

/** A disabled internal trigger means its constraint is no longer enforced. */
function triggerStateBadge(trigger: Trigger) {
  if (trigger.state === 'disabled') {
    return (
      <Badge variant={trigger.kind === 'internal' ? 'destructive' : 'secondary'}>disabled</Badge>
    )
  }
  return <Badge variant="outline">{trigger.state}</Badge>
}

function triggerColumns(): DataTableProps<Trigger>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database' },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (trigger) => `${trigger.schemaname}.${trigger.relname}`,
    },
    { accessorKey: 'name', header: 'Trigger' },
    {
      accessorKey: 'state',
      header: 'State',
      cell: ({ row }) => triggerStateBadge(row.original),
    },
    {
      id: 'enforces',
      header: 'Enforces',
      accessorFn: (trigger) =>
        trigger.kind === 'internal'
          ? `${trigger.constraint_type || 'constraint'} ${trigger.constraint}`.trim()
          : '',
    },
    {
      id: 'fires',
      header: 'Fires',
      accessorFn: (trigger) => `${trigger.timing} ${trigger.events} FOR EACH ${trigger.level}`,
    },
    { accessorKey: 'function', header: 'Function' },
    {
      accessorKey: 'changed_at',
      header: 'State since',
      cell: ({ row }) => formatTimestamp(row.original.changed_at, 'UTC'),
    },
  ]
}

function TriggersTab({ response }: { response: Schemas['TriggersResponse'] }) {
  const filter = useDatabaseFilter(response.triggers)
  const [disabledOnly, setDisabledOnly] = useState(false)
  const rows = useMemo(
    () => filter.rows.filter((trigger) => !disabledOnly || trigger.state === 'disabled'),
    [disabledOnly, filter.rows],
  )
  const columns = useMemo(() => triggerColumns(), [])
  const emptyState =
    response.triggers.length === 0 ? (
      <EmptyState
        title="No triggers reported"
        description="The trigger inventory found no user triggers and no disabled constraint triggers."
      />
    ) : (
      <EmptyState
        title="No triggers match these filters"
        description="Try another database or show enabled triggers too."
      />
    )

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-3">
        <DatabaseFilter
          id="catalog-trigger-database"
          databases={filter.databases}
          value={filter.database}
          onChange={filter.setDatabase}
        />
        <label className="flex items-center gap-2 self-end text-sm">
          <input
            checked={disabledOnly}
            onChange={(event) => setDisabledOnly(event.target.checked)}
            type="checkbox"
          />
          <span>Disabled only</span>
        </label>
      </div>
      <TruncatedNotice
        truncated={response.truncated_databases}
        database={filter.database}
        what="triggers"
        kept="disabled triggers are listed first, enabled ones may be missing"
      />
      <DataTable
        ariaLabel="Triggers"
        columns={columns}
        data={rows}
        emptyState={emptyState}
        scope="triggers"
        total={response.triggers.length}
      />
    </div>
  )
}

function foreignKeyColumns(): DataTableProps<Schemas['UnindexedForeignKey']>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database' },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (fk) => `${fk.schemaname}.${fk.relname}`,
    },
    { accessorKey: 'constraint', header: 'Constraint' },
    { accessorKey: 'columns', header: 'Columns' },
    { accessorKey: 'referenced', header: 'References' },
    {
      accessorKey: 'table_bytes',
      header: 'Table size',
      cell: ({ row }) => formatBytes(row.original.table_bytes),
    },
  ]
}

function UnindexedForeignKeysTab({
  response,
}: {
  response: Schemas['UnindexedForeignKeysResponse']
}) {
  const filter = useDatabaseFilter(response.foreign_keys)
  const columns = useMemo(() => foreignKeyColumns(), [])

  return (
    <div className="space-y-4">
      <p className="text-muted-foreground text-sm">
        Deleting a referenced row, or updating its key, scans the whole referencing table when no
        index leads with the foreign key&apos;s columns.
      </p>
      <div className="grid gap-4 sm:grid-cols-3">
        <DatabaseFilter
          id="catalog-fk-database"
          databases={filter.databases}
          value={filter.database}
          onChange={filter.setDatabase}
        />
      </div>
      <TruncatedNotice
        truncated={response.truncated_databases}
        database={filter.database}
        what="unindexed foreign keys"
        kept="the largest tables are listed"
      />
      <DataTable
        ariaLabel="Unindexed foreign keys"
        columns={columns}
        data={filter.rows}
        emptyState={
          <EmptyState
            title="Every foreign key has an index"
            description="No foreign key without an index leading with its columns was reported."
          />
        }
        scope="foreign keys"
        total={response.foreign_keys.length}
      />
    </div>
  )
}

function extensionColumns(): DataTableProps<Schemas['Extension']>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database' },
    { accessorKey: 'name', header: 'Extension' },
    { accessorKey: 'schema', header: 'Schema' },
    { accessorKey: 'version', header: 'Installed' },
    {
      accessorKey: 'default_version',
      header: 'Packages',
      cell: ({ row }) =>
        row.original.default_version || <Unknown reason="The packages no longer carry it." />,
    },
    {
      accessorKey: 'upgrade',
      header: 'Update',
      cell: ({ row }) =>
        row.original.upgrade === 'available' ? (
          <Badge variant="secondary">available</Badge>
        ) : row.original.upgrade === 'unknown' ? (
          <Unknown reason="The versions cannot be compared." />
        ) : (
          'up to date'
        ),
    },
  ]
}

function ExtensionsTab({ response }: { response: Schemas['ExtensionsResponse'] }) {
  const filter = useDatabaseFilter(response.extensions)
  const columns = useMemo(() => extensionColumns(), [])

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-3">
        <DatabaseFilter
          id="catalog-extension-database"
          databases={filter.databases}
          value={filter.database}
          onChange={filter.setDatabase}
        />
      </div>
      <TruncatedNotice
        truncated={response.truncated_databases}
        database={filter.database}
        what="extensions"
        kept="the first ones by name are listed"
      />
      <DataTable
        ariaLabel="Extensions"
        columns={columns}
        data={filter.rows}
        emptyState={
          <EmptyState
            title="No extensions reported"
            description="The extension inventory has not reported for any database yet."
          />
        }
        scope="extensions"
        total={response.extensions.length}
      />
    </div>
  )
}

function sridCell(column: SpatialColumn): ReactNode {
  if (column.srid === null) {
    return <Unknown reason="Declared without a typmod: each value carries its own SRID." />
  }
  if (column.srid_known === false) {
    return (
      <span className="inline-flex items-center gap-1">
        {column.srid}
        <Badge variant="secondary">{column.srid === 0 ? 'unknown' : 'undefined'}</Badge>
      </span>
    )
  }
  return column.srid
}

function spatialColumns(): DataTableProps<SpatialColumn>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database' },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (column) => `${column.schemaname}.${column.relname}`,
    },
    { accessorKey: 'column', header: 'Column' },
    {
      id: 'declared',
      header: 'Type',
      accessorFn: (column) => `${column.type}(${column.subtype})`,
    },
    {
      accessorKey: 'srid',
      header: 'SRID',
      cell: ({ row }) => sridCell(row.original),
    },
    {
      accessorKey: 'index_method',
      header: 'Spatial index',
      cell: ({ row }) =>
        row.original.index_method ? (
          row.original.index_method
        ) : (
          <Badge variant="secondary">missing</Badge>
        ),
    },
    {
      accessorKey: 'rows',
      header: 'Rows (est.)',
      cell: ({ row }) => formatCount(row.original.rows),
    },
  ]
}

function PostGISTab({
  spatial,
  extensions,
}: {
  spatial: Schemas['SpatialColumnsResponse']
  extensions: Schemas['ExtensionsResponse'] | undefined
}) {
  const filter = useDatabaseFilter(spatial.columns)
  const columns = useMemo(() => spatialColumns(), [])
  const postgis = useMemo(
    () =>
      (extensions?.extensions ?? []).filter(
        (extension) =>
          extension.name === 'postgis' &&
          (filter.database === 'all' || extension.datname === filter.database),
      ),
    [extensions, filter.database],
  )

  return (
    <div className="space-y-4">
      {postgis.length > 0 ? (
        <ul className="text-sm" aria-label="PostGIS versions">
          {postgis.map((extension) => (
            <li key={extension.datname}>
              {extension.datname}: PostGIS {extension.version}
              {extension.upgrade === 'available'
                ? ` (packages carry ${extension.default_version})`
                : null}
            </li>
          ))}
        </ul>
      ) : null}
      <div className="grid gap-4 sm:grid-cols-3">
        <DatabaseFilter
          id="catalog-spatial-database"
          databases={filter.databases}
          value={filter.database}
          onChange={filter.setDatabase}
        />
      </div>
      <TruncatedNotice
        truncated={spatial.truncated_databases}
        database={filter.database}
        what="spatial columns"
        kept="unindexed columns of the largest tables are listed first"
      />
      <DataTable
        ariaLabel="Spatial columns"
        columns={columns}
        data={filter.rows}
        emptyState={
          <EmptyState
            title="No spatial columns reported"
            description="No monitored database with PostGIS has geometry or geography columns."
          />
        }
        scope="spatial columns"
        total={spatial.columns.length}
      />
    </div>
  )
}

/** Used fractions from which a sequence is a warning and critical, as in the advisor. */
const SEQUENCE_WARN_RATIO = 0.75
const SEQUENCE_CRITICAL_RATIO = 0.9

function sequenceAtRisk(sequence: Sequence): boolean {
  return sequence.exhausts && (sequence.used_ratio ?? 0) >= SEQUENCE_WARN_RATIO
}

function sequenceUsedCell(sequence: Sequence): ReactNode {
  switch (sequence.state) {
    case 'hidden':
      return (
        <Unknown reason="The monitoring role has no SELECT on this sequence: grant it, or run the agent at tier 1." />
      )
    case 'unread':
      return <Unknown reason="Reading the sequence waited too long for a lock." />
    case 'unused':
    case 'used':
      break
  }
  const ratio = sequence.used_ratio ?? 0
  const percent = formatPercent(ratio, 1) ?? ''
  if (!sequenceAtRisk(sequence)) return percent
  return (
    <Badge variant={ratio >= SEQUENCE_CRITICAL_RATIO ? 'destructive' : 'secondary'}>
      {percent}
    </Badge>
  )
}

function sequenceColumns(): DataTableProps<Sequence>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database' },
    {
      id: 'sequence',
      header: 'Sequence',
      accessorFn: (sequence) => `${sequence.schemaname}.${sequence.name}`,
      cell: ({ row }) => (
        <span className="inline-flex items-center gap-1">
          {row.original.schemaname}.{row.original.name}
          {row.original.cycle ? <Badge variant="outline">cycles</Badge> : null}
        </span>
      ),
    },
    { accessorKey: 'data_type', header: 'Type' },
    {
      id: 'column',
      header: 'Feeds',
      accessorFn: (sequence) =>
        sequence.column
          ? `${sequence.table_schema}.${sequence.table}.${sequence.column} (${sequence.column_type})`
          : '',
      cell: ({ getValue, row }) => {
        const shown = getValue<string>()
        if (!shown) return <span className="text-muted-foreground">no column</span>
        const more = row.original.columns > 1 ? ` +${row.original.columns - 1}` : ''
        return `${shown}${more}`
      },
    },
    {
      id: 'last_value',
      header: 'Last value',
      accessorFn: (sequence) => sequence.last_value ?? '',
      cell: ({ row }) =>
        row.original.last_value !== null ? (
          formatIntegerString(row.original.last_value)
        ) : row.original.state === 'unused' ? (
          <span className="text-muted-foreground">never called</span>
        ) : (
          <Unknown reason="Not readable." />
        ),
    },
    {
      accessorKey: 'limit',
      header: 'Limit',
      cell: ({ row }) => (
        <span className="inline-flex items-center gap-1">
          {formatIntegerString(row.original.limit)}
          {row.original.limit_source === 'column' ? (
            <Badge variant="secondary">column type</Badge>
          ) : null}
        </span>
      ),
    },
    {
      id: 'used',
      header: 'Used',
      accessorFn: (sequence) => sequence.used_ratio ?? -1,
      cell: ({ row }) => sequenceUsedCell(row.original),
    },
    {
      id: 'remaining',
      header: 'Remaining',
      accessorFn: (sequence) => sequence.remaining ?? '',
      cell: ({ row }) =>
        row.original.remaining !== null ? formatIntegerString(row.original.remaining) : null,
    },
  ]
}

/** The closest to their limit first, then the columns narrower than their sequence. */
function compareSequences(left: Sequence, right: Sequence): number {
  const ratio = (right.used_ratio ?? -1) - (left.used_ratio ?? -1)
  if (ratio !== 0) return ratio
  if (left.limit_source !== right.limit_source) return left.limit_source === 'column' ? -1 : 1
  return (
    left.datname.localeCompare(right.datname) ||
    `${left.schemaname}.${left.name}`.localeCompare(`${right.schemaname}.${right.name}`)
  )
}

function SequencesTab({ response }: { response: Schemas['SequencesResponse'] }) {
  const sorted = useMemo(() => [...response.sequences].sort(compareSequences), [response])
  const filter = useDatabaseFilter(sorted)
  const columns = useMemo(() => sequenceColumns(), [])
  const hidden = filter.rows.filter((sequence) => sequence.state === 'hidden').length

  return (
    <div className="space-y-4">
      <p className="text-muted-foreground text-sm">
        A sequence runs out at the nearer of its own bound and the bound of the integer column it
        feeds. pglens only reads sequences; it never advances them.
      </p>
      <div className="grid gap-4 sm:grid-cols-3">
        <DatabaseFilter
          id="catalog-sequence-database"
          databases={filter.databases}
          value={filter.database}
          onChange={filter.setDatabase}
        />
      </div>
      {hidden > 0 ? (
        <p role="status" className="text-muted-foreground text-sm">
          The values of {hidden} {hidden === 1 ? 'sequence are' : 'sequences are'} not readable by
          the monitoring role: grant it SELECT on them (never USAGE, which allows nextval), or run
          the agent at tier 1.
        </p>
      ) : null}
      <TruncatedNotice
        truncated={response.truncated_databases}
        database={filter.database}
        what="sequences"
        kept="those closest to their limit are listed"
      />
      <DataTable
        ariaLabel="Sequences"
        columns={columns}
        data={filter.rows}
        emptyState={
          <EmptyState
            title="No sequences reported"
            description="The sequence check has not reported a sequence for any database yet."
          />
        }
        scope="sequences"
        total={response.sequences.length}
      />
    </div>
  )
}

/** How far ahead a time-range table's partitions should reach, as in the advisor. */
const PARTITION_HORIZON_MS = 7 * 24 * 60 * 60 * 1000
/**
 * How long after its partitions ended a table is taken as retired rather than
 * forgotten, as in the advisor.
 */
const PARTITION_LAPSED_MS = 30 * 24 * 60 * 60 * 1000
/** Rows from which a never-analyzed partitioned table is worth a mention, as in the advisor. */
const PARTITION_ANALYZE_ROWS = 10_000

/** Milliseconds until a table's partitions run out, or null when that is not known or never. */
function partitionAhead(table: PartitionedTable, now: Date): number | null {
  if (table.covered_until === null || table.covered_until === 'unbounded') return null
  const until = new Date(table.covered_until).getTime()
  return Number.isFinite(until) ? until - now.getTime() : null
}

function partitionNeedsAttention(table: PartitionedTable, now: Date): boolean {
  const ahead = partitionAhead(table, now)
  return (
    (table.default_rows ?? 0) > 0 ||
    table.invalid_indexes.length > 0 ||
    (ahead !== null && ahead < PARTITION_HORIZON_MS && ahead >= -PARTITION_LAPSED_MS)
  )
}

function coveredCell(table: PartitionedTable, now: Date): ReactNode {
  if (table.covered_until === null) return null
  if (table.covered_until === 'unbounded') return 'no end (MAXVALUE)'
  const ahead = partitionAhead(table, now)
  const shown = formatTimestamp(table.covered_until, 'UTC')
  if (ahead === null || ahead >= PARTITION_HORIZON_MS) return shown
  if (ahead < -PARTITION_LAPSED_MS) {
    return (
      <span>
        {shown} <span className="text-muted-foreground">(ended)</span>
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1">
      {shown}
      <Badge variant={ahead <= 0 && table.default_partition === null ? 'destructive' : 'secondary'}>
        {ahead <= 0 ? 'ended' : 'ends soon'}
      </Badge>
    </span>
  )
}

function partitionColumns(now: Date): DataTableProps<PartitionedTable>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database' },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (table) => `${table.schemaname}.${table.relname}`,
    },
    { accessorKey: 'partition_key', header: 'Key' },
    {
      accessorKey: 'partitions',
      header: 'Partitions',
      cell: ({ row }) =>
        row.original.depth > 1
          ? `${formatCount(row.original.partitions)} in ${row.original.depth} levels`
          : formatCount(row.original.partitions),
    },
    {
      accessorKey: 'bytes',
      header: 'Size (est.)',
      cell: ({ row }) => formatBytes(row.original.bytes),
    },
    {
      accessorKey: 'rows',
      header: 'Rows (est.)',
      cell: ({ row }) => formatCount(row.original.rows),
    },
    {
      id: 'default',
      header: 'Default partition',
      accessorFn: (table) => table.default_rows ?? -1,
      cell: ({ row }) => {
        const { default_partition: name, default_rows: rows } = row.original
        if (name === null) return <span className="text-muted-foreground">none</span>
        if ((rows ?? 0) <= 0) return name
        return (
          <span className="inline-flex items-center gap-1">
            {name}
            <Badge variant="secondary">{formatCount(rows ?? 0)} rows</Badge>
          </span>
        )
      },
    },
    {
      id: 'invalid',
      header: 'Invalid indexes',
      accessorFn: (table) => table.invalid_indexes.length,
      cell: ({ row }) =>
        row.original.invalid_indexes.length > 0 ? (
          <Badge variant="secondary">{row.original.invalid_indexes.join(', ')}</Badge>
        ) : null,
    },
    {
      id: 'analyzed',
      header: 'Analyzed',
      accessorFn: (table) => table.analyzed_at ?? '',
      cell: ({ row }) =>
        row.original.analyzed_at !== null ? (
          formatTimestamp(row.original.analyzed_at, 'UTC')
        ) : row.original.rows >= PARTITION_ANALYZE_ROWS ? (
          <Badge variant="secondary">never</Badge>
        ) : (
          <span className="text-muted-foreground">never</span>
        ),
    },
    {
      id: 'covered',
      header: 'Partitions reach',
      accessorFn: (table) => table.covered_until ?? '',
      cell: ({ row }) => coveredCell(row.original, now),
    },
  ]
}

function PartitionsTab({
  response,
  now,
}: {
  response: Schemas['PartitionedTablesResponse']
  now: Date
}) {
  const filter = useDatabaseFilter(response.tables)
  const columns = useMemo(() => partitionColumns(now), [now])

  return (
    <div className="space-y-4">
      <p className="text-muted-foreground text-sm">
        Each partitioned table with its whole tree. Sizes and rows are the planner&apos;s estimates,
        so reading them locks no partition.
      </p>
      <div className="grid gap-4 sm:grid-cols-3">
        <DatabaseFilter
          id="catalog-partition-database"
          databases={filter.databases}
          value={filter.database}
          onChange={filter.setDatabase}
        />
      </div>
      <TruncatedNotice
        truncated={response.truncated_databases}
        database={filter.database}
        what="partitioned tables"
        kept="the largest are listed"
      />
      <DataTable
        ariaLabel="Partitioned tables"
        columns={columns}
        data={filter.rows}
        emptyState={
          <EmptyState
            title="No partitioned tables reported"
            description="No monitored database has a partitioned table."
          />
        }
        scope="partitioned tables"
        total={response.tables.length}
      />
    </div>
  )
}

interface CatalogTabStateProps<T> {
  query: ApiQueryResult<T>
  label: string
  children: (data: T) => ReactNode
}

function CatalogTabState<T>({ query, label, children }: CatalogTabStateProps<T>) {
  if (query.error && !query.data) {
    return (
      <ErrorState
        endpoint={`instance ${label}`}
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (query.isPending || query.data === undefined) {
    return (
      <section aria-busy="true" aria-label={`Loading ${label}`} role="status">
        Loading {label}…
      </section>
    )
  }
  return children(query.data)
}

function TabCount({ count, label }: { count: number; label: string }) {
  if (count === 0) return null
  return (
    <Badge variant="secondary" aria-label={`${count} ${label}`}>
      {count}
    </Badge>
  )
}

export function CatalogSection({ instanceId }: CatalogSectionProps) {
  const triggersQuery = useInstanceTriggers(instanceId)
  const foreignKeysQuery = useInstanceUnindexedForeignKeys(instanceId)
  const extensionsQuery = useInstanceExtensions(instanceId)
  const spatialQuery = useInstanceSpatialColumns(instanceId)
  const sequencesQuery = useInstanceSequences(instanceId)
  const partitionsQuery = useInstancePartitionedTables(instanceId)
  // Partition reach is judged against when the data was read, not the clock.
  const partitionsReadAt = partitionsQuery.dataUpdatedAt
  const now = useMemo(() => new Date(partitionsReadAt), [partitionsReadAt])
  const disabledTriggers =
    triggersQuery.data?.triggers.filter((trigger) => trigger.state === 'disabled').length ?? 0
  const upgradableExtensions =
    extensionsQuery.data?.extensions.filter((extension) => extension.upgrade === 'available')
      .length ?? 0
  const unindexedSpatial =
    spatialQuery.data?.columns.filter((column) => column.index_method === '').length ?? 0
  const sequencesAtRisk = sequencesQuery.data?.sequences.filter(sequenceAtRisk).length ?? 0
  const partitionIssues =
    partitionsQuery.data?.tables.filter((table) => partitionNeedsAttention(table, now)).length ?? 0

  return (
    <Section
      id="catalog"
      title="Schema objects"
      description="Objects the catalog checks report for each database, refreshed every few minutes to hourly."
    >
      <Tabs defaultValue="triggers">
        <TabsList aria-label="Schema object kinds">
          <TabsTrigger value="triggers">
            Triggers <TabCount count={disabledTriggers} label="disabled" />
          </TabsTrigger>
          <TabsTrigger value="sequences">
            Sequences <TabCount count={sequencesAtRisk} label="near their limit" />
          </TabsTrigger>
          <TabsTrigger value="partitions">
            Partitions <TabCount count={partitionIssues} label="need attention" />
          </TabsTrigger>
          <TabsTrigger value="foreign-keys">
            Unindexed foreign keys{' '}
            <TabCount count={foreignKeysQuery.data?.foreign_keys.length ?? 0} label="reported" />
          </TabsTrigger>
          <TabsTrigger value="extensions">
            Extensions <TabCount count={upgradableExtensions} label="with an update" />
          </TabsTrigger>
          <TabsTrigger value="postgis">
            PostGIS <TabCount count={unindexedSpatial} label="unindexed" />
          </TabsTrigger>
        </TabsList>
        <TabsContent value="triggers">
          <CatalogTabState query={triggersQuery} label="triggers">
            {(data) => <TriggersTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="sequences">
          <CatalogTabState query={sequencesQuery} label="sequences">
            {(data) => <SequencesTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="partitions">
          <CatalogTabState query={partitionsQuery} label="partitioned tables">
            {(data) => <PartitionsTab response={data} now={now} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="foreign-keys">
          <CatalogTabState query={foreignKeysQuery} label="unindexed foreign keys">
            {(data) => <UnindexedForeignKeysTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="extensions">
          <CatalogTabState query={extensionsQuery} label="extensions">
            {(data) => <ExtensionsTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="postgis">
          <CatalogTabState query={spatialQuery} label="spatial columns">
            {(data) => <PostGISTab spatial={data} extensions={extensionsQuery.data} />}
          </CatalogTabState>
        </TabsContent>
      </Tabs>
    </Section>
  )
}
