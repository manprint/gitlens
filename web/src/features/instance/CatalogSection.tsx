import { useMemo, useState, type ReactNode } from 'react'

import {
  useInstanceExtensions,
  useInstanceFunctions,
  useInstancePartitionedTables,
  useInstanceSecurityDefinerFunctions,
  useInstanceSequences,
  useInstanceSpatialColumns,
  useInstanceTriggers,
  useInstanceUnindexedForeignKeys,
  type ApiQueryResult,
} from '@/api/queries'
import type { RowData } from '@tanstack/react-table'

import type { Schemas } from '@/api/types'
import type { CaptionId } from '@/lib/captions'
import {
  DataTable,
  type DataTableProps,
  type DataTableToolbar,
} from '@/components/layout/DataTable'
import { EmptyState, ErrorState, Unknown } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import {
  formatBytes,
  formatCount,
  formatIntegerString,
  formatPercent,
  formatRate,
  formatTimestamp,
} from '@/lib/format'
import { NativeSelect } from '@/components/ui/native-select'

import { DENSE_TABLE } from './denseTable'
import { CaptionedBlock, InstancePanel, InstanceViewTabs, ToolbarNote } from './InstancePanel'
import { CATALOG_VIEW_IDS } from './instanceSections'
import { useInstanceView, type InstanceView } from './instanceViews'

interface CatalogSectionProps {
  instanceId: string
}

type Trigger = Schemas['Trigger']
type SpatialColumn = Schemas['SpatialColumn']
type Sequence = Schemas['Sequence']
type PartitionedTable = Schemas['PartitionedTable']
type FunctionStat = Schemas['FunctionStat']
type SecurityDefinerFunction = Schemas['SecurityDefinerFunction']

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
    // A page control in the table's toolbar line; from 1600 px its name is
    // read out only, as the shared filters do (D26).
    <label className="flex flex-col gap-1 text-sm" htmlFor={id}>
      <span className="font-medium min-[1600px]:sr-only">Database</span>
      <NativeSelect
        className="h-8 w-44"
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
      </NativeSelect>
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
    <ToolbarNote status wrap>
      {`${names.join(', ')} ${names.length === 1 ? 'has' : 'have'} more ${what} than the agent reports: ${kept}.`}
    </ToolbarNote>
  )
}

/** A typed cell title for `meta.title`, which sees the row as unknown. */
function titleOf<T>(title: (row: T) => string): (row: unknown) => string {
  return (row) => title(row as T)
}

/**
 * A catalog table with its page controls and notes in the toolbar line (D26).
 * A table with no row draws no toolbar, so the controls then stay above its
 * empty state, where a filter that matched nothing can be undone.
 */
function CatalogTable<T extends RowData>({
  captionId,
  controls,
  notes,
  dense = false,
  ...table
}: DataTableProps<T> & {
  captionId: CaptionId
  controls: ReactNode
  notes?: ReactNode
  dense?: boolean
}) {
  const extra = (
    <>
      {controls}
      {notes}
    </>
  )
  if (table.data.length === 0) {
    return (
      <CaptionedBlock ariaLabel={table.ariaLabel} captionId={captionId}>
        <div className="space-y-4">
          <div className="flex flex-wrap items-end gap-3">{extra}</div>
          <DataTable {...table} captionId={captionId} />
        </div>
      </CaptionedBlock>
    )
  }
  const content = <DataTable {...table} captionId={captionId} toolbarExtra={extra} />
  return dense ? <div className={DENSE_TABLE}>{content}</div> : content
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

// Every tab has its Database select above the table: no database filter here.
const TRIGGER_TOOLBAR: DataTableToolbar<Trigger> = {
  searchFields: [
    (trigger) => trigger.name,
    (trigger) => `${trigger.schemaname}.${trigger.relname}`,
    (trigger) => trigger.function,
  ],
}

function triggerColumns(): DataTableProps<Trigger>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database', meta: { width: '9rem', truncate: true } },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (trigger) => `${trigger.schemaname}.${trigger.relname}`,
      meta: { truncate: true },
    },
    { accessorKey: 'name', header: 'Trigger', meta: { truncate: true } },
    {
      accessorKey: 'state',
      header: 'State',
      meta: { width: '7.5rem' },
      cell: ({ row }) => triggerStateBadge(row.original),
    },
    {
      id: 'enforces',
      header: 'Enforces',
      accessorFn: (trigger) =>
        trigger.kind === 'internal'
          ? `${trigger.constraint_type || 'constraint'} ${trigger.constraint}`.trim()
          : '',
      meta: { truncate: true },
    },
    {
      id: 'fires',
      header: 'Fires',
      accessorFn: (trigger) => `${trigger.timing} ${trigger.events} FOR EACH ${trigger.level}`,
      meta: { truncate: true },
    },
    { accessorKey: 'function', header: 'Function', meta: { truncate: true } },
    {
      accessorKey: 'changed_at',
      header: 'State since',
      meta: { width: '13rem' },
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
    <CatalogTable
      controls={
        <>
          <DatabaseFilter
            id="catalog-trigger-database"
            databases={filter.databases}
            value={filter.database}
            onChange={filter.setDatabase}
          />
          <label className="flex h-8 items-center gap-2 text-sm">
            <input
              checked={disabledOnly}
              onChange={(event) => setDisabledOnly(event.target.checked)}
              type="checkbox"
            />
            <span>Disabled only</span>
          </label>
        </>
      }
      notes={
        <>
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what="triggers"
            kept="disabled triggers are listed first, enabled ones may be missing"
          />
        </>
      }
      captionId="catalog.triggers"
      ariaLabel="Triggers"
      tableId="triggers"
      toolbar={TRIGGER_TOOLBAR}
      columns={columns}
      data={rows}
      emptyState={emptyState}
      scope="triggers"
      total={response.triggers.length}
    />
  )
}

const FOREIGN_KEY_TOOLBAR: DataTableToolbar<Schemas['UnindexedForeignKey']> = {
  searchFields: [
    (fk) => `${fk.schemaname}.${fk.relname}`,
    (fk) => fk.constraint,
    (fk) => String(fk.columns),
    (fk) => fk.referenced,
  ],
}

function foreignKeyColumns(): DataTableProps<Schemas['UnindexedForeignKey']>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database', meta: { width: '9rem', truncate: true } },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (fk) => `${fk.schemaname}.${fk.relname}`,
      meta: { truncate: true },
    },
    { accessorKey: 'constraint', header: 'Constraint', meta: { truncate: true } },
    {
      accessorKey: 'columns',
      header: 'Columns',
      meta: {
        truncate: true,
        title: titleOf<Schemas['UnindexedForeignKey']>((fk) => String(fk.columns)),
      },
    },
    { accessorKey: 'referenced', header: 'References', meta: { truncate: true } },
    {
      accessorKey: 'table_bytes',
      header: 'Table size',
      meta: { width: '9.5rem' },
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
    <CatalogTable
      controls={
        <>
          <DatabaseFilter
            id="catalog-fk-database"
            databases={filter.databases}
            value={filter.database}
            onChange={filter.setDatabase}
          />
        </>
      }
      notes={
        <>
          <ToolbarNote>{`Deleting a referenced row, or updating its key, scans the whole referencing table when no index leads with the foreign key's columns.`}</ToolbarNote>
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what="unindexed foreign keys"
            kept="the largest tables are listed"
          />
        </>
      }
      captionId="catalog.foreign-keys"
      ariaLabel="Unindexed foreign keys"
      tableId="unindexed-foreign-keys"
      toolbar={FOREIGN_KEY_TOOLBAR}
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
  )
}

const EXTENSION_TOOLBAR: DataTableToolbar<Schemas['Extension']> = {
  searchFields: [(extension) => extension.name, (extension) => extension.schema],
  filters: [
    { id: 'schema', label: 'Schema', kind: 'select', accessor: (extension) => extension.schema },
  ],
}

function extensionColumns(): DataTableProps<Schemas['Extension']>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database', meta: { width: '9rem', truncate: true } },
    { accessorKey: 'name', header: 'Extension', meta: { truncate: true } },
    { accessorKey: 'schema', header: 'Schema', meta: { width: '9rem', truncate: true } },
    { accessorKey: 'version', header: 'Installed', meta: { width: '9rem', truncate: true } },
    {
      accessorKey: 'default_version',
      header: 'Packages',
      meta: { width: '11rem', help: 'catalog.extension-packages' },
      cell: ({ row }) =>
        row.original.default_version || <Unknown reason="The packages no longer carry it." />,
    },
    {
      accessorKey: 'upgrade',
      header: 'Update',
      meta: { width: '10.5rem', help: 'catalog.extension-update' },
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
    <CatalogTable
      controls={
        <>
          <DatabaseFilter
            id="catalog-extension-database"
            databases={filter.databases}
            value={filter.database}
            onChange={filter.setDatabase}
          />
        </>
      }
      notes={
        <>
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what="extensions"
            kept="the first ones by name are listed"
          />
        </>
      }
      captionId="catalog.extensions"
      ariaLabel="Extensions"
      tableId="extensions"
      toolbar={EXTENSION_TOOLBAR}
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

const SPATIAL_TOOLBAR: DataTableToolbar<SpatialColumn> = {
  searchFields: [(column) => `${column.schemaname}.${column.relname}`, (column) => column.column],
  filters: [
    {
      id: 'type',
      label: 'Type',
      kind: 'select',
      accessor: (column) => `${column.type}(${column.subtype})`,
    },
    {
      id: 'index',
      label: 'Spatial index',
      kind: 'select',
      accessor: (column) => column.index_method || 'missing',
    },
  ],
}

function spatialColumns(): DataTableProps<SpatialColumn>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database', meta: { width: '9rem', truncate: true } },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (column) => `${column.schemaname}.${column.relname}`,
      meta: { truncate: true },
    },
    { accessorKey: 'column', header: 'Column', meta: { truncate: true } },
    {
      id: 'declared',
      header: 'Type',
      accessorFn: (column) => `${column.type}(${column.subtype})`,
      meta: { truncate: true },
    },
    {
      accessorKey: 'srid',
      header: 'SRID',
      meta: { width: '8rem' },
      cell: ({ row }) => sridCell(row.original),
    },
    {
      accessorKey: 'index_method',
      header: 'Spatial index',
      meta: { width: '11rem' },
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
      meta: { width: '10rem' },
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
    <CatalogTable
      controls={
        <>
          <DatabaseFilter
            id="catalog-spatial-database"
            databases={filter.databases}
            value={filter.database}
            onChange={filter.setDatabase}
          />
        </>
      }
      notes={
        <>
          {postgis.length > 0 ? (
            // From 1600 px the versions are one truncated line of the toolbar (D26).
            <ul
              className="text-sm min-[1600px]:min-w-0 min-[1600px]:shrink min-[1600px]:grow min-[1600px]:basis-0 min-[1600px]:self-center min-[1600px]:truncate"
              aria-label="PostGIS versions"
            >
              {postgis.map((extension) => (
                <li key={extension.datname} className="min-[1600px]:mr-3 min-[1600px]:inline">
                  {extension.datname}: PostGIS {extension.version}
                  {extension.upgrade === 'available'
                    ? ` (packages carry ${extension.default_version})`
                    : null}
                </li>
              ))}
            </ul>
          ) : null}
          <TruncatedNotice
            truncated={spatial.truncated_databases}
            database={filter.database}
            what="spatial columns"
            kept="unindexed columns of the largest tables are listed first"
          />
        </>
      }
      captionId="catalog.spatial-columns"
      ariaLabel="Spatial columns"
      tableId="spatial-columns"
      toolbar={SPATIAL_TOOLBAR}
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

const SEQUENCE_TOOLBAR: DataTableToolbar<Sequence> = {
  searchFields: [
    (sequence) => `${sequence.schemaname}.${sequence.name}`,
    (sequence) =>
      sequence.column ? `${sequence.table_schema}.${sequence.table}.${sequence.column}` : null,
  ],
  filters: [
    { id: 'type', label: 'Type', kind: 'select', accessor: (sequence) => sequence.data_type },
  ],
}

function sequenceColumns(): DataTableProps<Sequence>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database', meta: { width: '9rem', truncate: true } },
    {
      id: 'sequence',
      header: 'Sequence',
      accessorFn: (sequence) => `${sequence.schemaname}.${sequence.name}`,
      meta: { width: '20rem', truncate: true },
      cell: ({ row }) => (
        <span className="flex min-w-0 items-center gap-1">
          <span className="min-w-0 truncate">
            {row.original.schemaname}.{row.original.name}
          </span>
          {row.original.cycle ? (
            <Badge className="shrink-0" variant="outline">
              cycles
            </Badge>
          ) : null}
        </span>
      ),
    },
    { accessorKey: 'data_type', header: 'Type', meta: { width: '7.5rem' } },
    {
      id: 'column',
      header: 'Feeds',
      meta: {
        truncate: true,
        title: titleOf<Sequence>((sequence) =>
          sequence.column
            ? `${sequence.table_schema}.${sequence.table}.${sequence.column} (${sequence.column_type})`
            : '',
        ),
      },
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
      meta: { width: '12.5rem' },
      accessorFn: (sequence) => sequence.last_value,
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
      meta: { width: '18.5rem', help: 'catalog.sequence-limit' },
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
      meta: { width: '9.5rem', help: 'catalog.sequence-used' },
      accessorFn: (sequence) => sequence.used_ratio,
      cell: ({ row }) => sequenceUsedCell(row.original),
    },
    {
      id: 'remaining',
      header: 'Remaining',
      meta: { width: '12.5rem' },
      accessorFn: (sequence) => sequence.remaining,
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
    <CatalogTable
      controls={
        <>
          <DatabaseFilter
            id="catalog-sequence-database"
            databases={filter.databases}
            value={filter.database}
            onChange={filter.setDatabase}
          />
        </>
      }
      notes={
        <>
          <ToolbarNote>{`A sequence runs out at the nearer of its own bound and its column's bound. pglens only reads sequences, never advances them.`}</ToolbarNote>
          {hidden > 0 ? (
            <ToolbarNote
              status
            >{`The values of ${hidden} ${hidden === 1 ? 'sequence are' : 'sequences are'} not readable by the monitoring role: grant it SELECT on them (never USAGE, which allows nextval), or run the agent at tier 1.`}</ToolbarNote>
          ) : null}
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what="sequences"
            kept="those closest to their limit are listed"
          />
        </>
      }
      captionId="catalog.sequences"
      ariaLabel="Sequences"
      tableId="sequences"
      toolbar={SEQUENCE_TOOLBAR}
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

const PARTITION_TOOLBAR: DataTableToolbar<PartitionedTable> = {
  searchFields: [(table) => `${table.schemaname}.${table.relname}`, (table) => table.partition_key],
}

function partitionColumns(now: Date): DataTableProps<PartitionedTable>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database', meta: { width: '9rem', truncate: true } },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (table) => `${table.schemaname}.${table.relname}`,
      meta: { truncate: true },
    },
    { accessorKey: 'partition_key', header: 'Key', meta: { truncate: true } },
    {
      accessorKey: 'partitions',
      header: 'Partitions',
      meta: { width: '8.5rem' },
      cell: ({ row }) =>
        row.original.depth > 1
          ? `${formatCount(row.original.partitions)} in ${row.original.depth} levels`
          : formatCount(row.original.partitions),
    },
    {
      accessorKey: 'bytes',
      header: 'Size (est.)',
      meta: { width: '8rem' },
      cell: ({ row }) => formatBytes(row.original.bytes),
    },
    {
      accessorKey: 'rows',
      header: 'Rows (est.)',
      cell: ({ row }) => formatCount(row.original.rows),
      meta: { width: '8.75rem' },
    },
    {
      id: 'default',
      header: 'Default partition',
      accessorFn: (table) => table.default_rows,
      meta: {
        width: '12rem',
        truncate: true,
        title: titleOf<PartitionedTable>((table) =>
          table.default_partition === null
            ? 'none'
            : `${table.default_partition}${(table.default_rows ?? 0) > 0 ? ` (${formatCount(table.default_rows ?? 0)} rows)` : ''}`,
        ),
      },
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
      meta: {
        width: '10.5rem',
        truncate: true,
        title: titleOf<PartitionedTable>((table) => table.invalid_indexes.join(', ')),
      },
      cell: ({ row }) =>
        row.original.invalid_indexes.length > 0 ? (
          <Badge variant="secondary">{row.original.invalid_indexes.join(', ')}</Badge>
        ) : null,
    },
    {
      id: 'analyzed',
      header: 'Analyzed',
      accessorFn: (table) => table.analyzed_at,
      meta: {
        width: '9rem',
        truncate: true,
        title: titleOf<PartitionedTable>((table) =>
          table.analyzed_at === null ? 'never' : formatTimestamp(table.analyzed_at, 'UTC'),
        ),
      },
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
      accessorFn: (table) => table.covered_until,
      meta: {
        help: 'catalog.partitions-reach',
        width: '14rem',
        truncate: true,
        title: titleOf<PartitionedTable>((table) =>
          table.covered_until === null
            ? ''
            : table.covered_until === 'unbounded'
              ? 'no end (MAXVALUE)'
              : formatTimestamp(table.covered_until, 'UTC'),
        ),
      },
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
    <CatalogTable
      controls={
        <>
          <DatabaseFilter
            id="catalog-partition-database"
            databases={filter.databases}
            value={filter.database}
            onChange={filter.setDatabase}
          />
        </>
      }
      notes={
        <>
          <ToolbarNote>{`Each partitioned table with its whole tree. Sizes and rows are the planner's estimates, so reading them locks no partition.`}</ToolbarNote>
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what="partitioned tables"
            kept="the largest are listed"
          />
        </>
      }
      // Ten sortable columns fit 1600 px only with the dense cells (D26).
      dense
      captionId="catalog.partitions"
      ariaLabel="Partitioned tables"
      tableId="partitioned-tables"
      toolbar={PARTITION_TOOLBAR}
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
  )
}

const MILLISECONDS = new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 })

function formatMilliseconds(value: number): string {
  return `${MILLISECONDS.format(value)} ms`
}

function functionName(fn: { schemaname: string; name: string; arguments: string }): string {
  return `${fn.schemaname}.${fn.name}(${fn.arguments})`
}

const FUNCTION_TOOLBAR: DataTableToolbar<FunctionStat> = { searchFields: [functionName] }

function functionColumns(): DataTableProps<FunctionStat>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database', meta: { width: '9rem', truncate: true } },
    { id: 'function', header: 'Function', accessorFn: functionName, meta: { truncate: true } },
    { accessorKey: 'language', header: 'Language', meta: { width: '9.25rem' } },
    {
      accessorKey: 'calls_per_second',
      header: 'Calls',
      meta: { width: '7.5rem' },
      cell: ({ row }) => formatRate(row.original.calls_per_second),
    },
    {
      accessorKey: 'self_ms_per_second',
      header: 'Own time',
      meta: { width: '11rem', help: 'catalog.function-own-time' },
      cell: ({ row }) => `${formatMilliseconds(row.original.self_ms_per_second)}/s`,
    },
    {
      accessorKey: 'total_ms_per_second',
      header: 'Total time',
      meta: { width: '9.75rem' },
      cell: ({ row }) => `${formatMilliseconds(row.original.total_ms_per_second)}/s`,
    },
    {
      id: 'mean',
      header: 'Mean per call',
      meta: { width: '11rem' },
      accessorFn: (fn) => fn.mean_ms,
      cell: ({ row }) =>
        row.original.mean_ms === null ? (
          <span className="text-muted-foreground">not called</span>
        ) : (
          formatMilliseconds(row.original.mean_ms)
        ),
    },
  ]
}

function FunctionsTab({ response }: { response: Schemas['FunctionsResponse'] }) {
  const filter = useDatabaseFilter(response.functions)
  const columns = useMemo(() => functionColumns(), [])
  const untracked = response.databases
    .filter(
      (db) =>
        db.track_functions === 'none' &&
        db.tracked_functions === 0 &&
        (filter.database === 'all' || db.datname === filter.database),
    )
    .map((db) => db.datname)
  const minutes = Math.round(response.window_seconds / 60)

  return (
    <CatalogTable
      controls={
        <>
          <DatabaseFilter
            id="catalog-function-database"
            databases={filter.databases}
            value={filter.database}
            onChange={filter.setDatabase}
          />
        </>
      }
      notes={
        <>
          <ToolbarNote>{`The tracked functions with the most own time and the most calls, averaged over the last ${minutes} minutes.`}</ToolbarNote>
          {untracked.length > 0 ? (
            <ToolbarNote
              status
              wrap
            >{`Function calls are not tracked in ${untracked.join(', ')}: track_functions is none. Set it to pl to track functions written in a procedural language; pglens never changes it.`}</ToolbarNote>
          ) : null}
        </>
      }
      captionId="catalog.functions"
      ariaLabel="Functions"
      tableId="functions"
      toolbar={FUNCTION_TOOLBAR}
      columns={columns}
      data={filter.rows}
      emptyState={
        <EmptyState
          title="No function calls reported"
          description="No tracked function was called in the window, or track_functions is none."
        />
      }
      scope="functions"
      total={response.functions.length}
    />
  )
}

/** A SECURITY DEFINER function every role can call as a superuser, as the advisor's critical case. */
function securityDefinerExposed(fn: SecurityDefinerFunction): boolean {
  return fn.search_path === null && fn.owner_superuser && fn.public_execute
}

const SECURITY_DEFINER_TOOLBAR: DataTableToolbar<SecurityDefinerFunction> = {
  searchFields: [functionName, (fn) => fn.owner],
}

function securityDefinerColumns(): DataTableProps<SecurityDefinerFunction>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database', meta: { width: '9rem', truncate: true } },
    {
      id: 'function',
      header: 'Function',
      accessorFn: functionName,
      meta: { truncate: true, title: titleOf<SecurityDefinerFunction>(functionName) },
      cell: ({ row }) => (
        <span className="inline-flex items-center gap-1">
          {functionName(row.original)}
          {row.original.kind === 'procedure' ? <Badge variant="outline">procedure</Badge> : null}
        </span>
      ),
    },
    {
      accessorKey: 'owner',
      header: 'Runs as',
      meta: {
        width: '12rem',
        truncate: true,
        title: titleOf<SecurityDefinerFunction>((fn) => fn.owner),
      },
      cell: ({ row }) => (
        <span className="inline-flex items-center gap-1">
          {row.original.owner}
          {row.original.owner_superuser ? <Badge variant="secondary">superuser</Badge> : null}
        </span>
      ),
    },
    {
      id: 'search_path',
      header: 'search_path',
      accessorFn: (fn) => fn.search_path,
      meta: {
        width: '14rem',
        truncate: true,
        title: titleOf<SecurityDefinerFunction>((fn) =>
          fn.search_path === null ? "caller's" : fn.search_path || '""',
        ),
      },
      cell: ({ row }) => {
        const fn = row.original
        if (fn.search_path === null) {
          return (
            <Badge variant={securityDefinerExposed(fn) ? 'destructive' : 'secondary'}>
              caller&apos;s
            </Badge>
          )
        }
        return (
          <span className="inline-flex items-center gap-1">
            <code className="text-xs">{fn.search_path === '' ? '""' : fn.search_path}</code>
            {fn.pg_temp_last ? null : (
              <span className="text-muted-foreground text-xs">(pg_temp first)</span>
            )}
          </span>
        )
      },
    },
    {
      id: 'public',
      header: 'Executable by',
      meta: { width: '11.5rem' },
      accessorFn: (fn) => (fn.public_execute ? 1 : 0),
      cell: ({ row }) => (row.original.public_execute ? 'every role (PUBLIC)' : 'granted roles'),
    },
  ]
}

function SecurityDefinerTab({
  response,
}: {
  response: Schemas['SecurityDefinerFunctionsResponse']
}) {
  const filter = useDatabaseFilter(response.functions)
  const columns = useMemo(() => securityDefinerColumns(), [])

  return (
    <CatalogTable
      controls={
        <>
          <DatabaseFilter
            id="catalog-secdef-database"
            databases={filter.databases}
            value={filter.database}
            onChange={filter.setDatabase}
          />
        </>
      }
      notes={
        <>
          <ToolbarNote>{`These functions run with their owner's privileges. One that does not pin search_path finds the objects its body names through the caller's, so a caller can substitute their own.`}</ToolbarNote>
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what="SECURITY DEFINER functions"
            kept="those without a pinned search_path are listed first"
          />
        </>
      }
      captionId="catalog.security-definer"
      ariaLabel="SECURITY DEFINER functions"
      tableId="security-definer-functions"
      toolbar={SECURITY_DEFINER_TOOLBAR}
      columns={columns}
      data={filter.rows}
      emptyState={
        <EmptyState
          title="No SECURITY DEFINER functions reported"
          description="No monitored database defines one outside its extensions."
        />
      }
      scope="functions"
      total={response.functions.length}
    />
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

type CatalogViewId = (typeof CATALOG_VIEW_IDS)[number]

/** The views of the tab, in order: the first is the default and never written to the URL. */
const CATALOG_VIEWS: readonly { id: CatalogViewId }[] = CATALOG_VIEW_IDS.map((id) => ({ id }))

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
  const functionsQuery = useInstanceFunctions(instanceId)
  const securityDefinerQuery = useInstanceSecurityDefinerFunctions(instanceId)
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
  const unpinnedFunctions =
    securityDefinerQuery.data?.functions.filter((fn) => fn.search_path === null).length ?? 0
  const partitionIssues =
    partitionsQuery.data?.tables.filter((table) => partitionNeedsAttention(table, now)).length ?? 0
  const [view = 'triggers', showView] = useInstanceView(CATALOG_VIEWS)

  const views: InstanceView<CatalogViewId>[] = [
    {
      id: 'triggers',
      label: (
        <>
          Triggers <TabCount count={disabledTriggers} label="disabled" />
        </>
      ),
    },
    {
      id: 'sequences',
      label: (
        <>
          Sequences <TabCount count={sequencesAtRisk} label="near their limit" />
        </>
      ),
    },
    {
      id: 'partitions',
      label: (
        <>
          Partitions <TabCount count={partitionIssues} label="need attention" />
        </>
      ),
    },
    { id: 'functions', label: 'Functions' },
    {
      id: 'security-definer',
      label: (
        <>
          Security definer{' '}
          <TabCount count={unpinnedFunctions} label="without a pinned search_path" />
        </>
      ),
    },
    {
      id: 'foreign-keys',
      label: (
        <>
          Unindexed foreign keys{' '}
          <TabCount count={foreignKeysQuery.data?.foreign_keys.length ?? 0} label="reported" />
        </>
      ),
    },
    {
      id: 'extensions',
      label: (
        <>
          Extensions <TabCount count={upgradableExtensions} label="with an update" />
        </>
      ),
    },
    {
      id: 'postgis',
      label: (
        <>
          PostGIS <TabCount count={unindexedSpatial} label="unindexed" />
        </>
      ),
    },
  ]

  return (
    // One object kind per view, in the URL (15.3); Triggers is the default.
    <Tabs onValueChange={showView} value={view}>
      <InstancePanel
        id="catalog"
        title="Schema objects"
        captionId="catalog.section"
        actions={<InstanceViewTabs label="Schema object kinds" views={views} />}
      >
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
        <TabsContent value="functions">
          <CatalogTabState query={functionsQuery} label="functions">
            {(data) => <FunctionsTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="security-definer">
          <CatalogTabState query={securityDefinerQuery} label="SECURITY DEFINER functions">
            {(data) => <SecurityDefinerTab response={data} />}
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
      </InstancePanel>
    </Tabs>
  )
}
