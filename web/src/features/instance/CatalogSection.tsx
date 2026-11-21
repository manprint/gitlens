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
import { HelpLink } from '@/components/layout/HelpLink'
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
  TIMESTAMP_COLUMN_WIDTH,
} from '@/lib/format'
import { useT, type Translate } from '@/i18n/react'
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
  const t = useT()
  return (
    // A page control in the table's toolbar line; from 1600 px its name is
    // read out only, as the shared filters do (D26).
    <label className="flex flex-col gap-1 text-sm" htmlFor={id}>
      <span className="font-medium min-[1600px]:sr-only">
        {t('instance.catalog.common.database')}
      </span>
      <NativeSelect
        className="h-8 w-44"
        id={id}
        onChange={(event) => onChange(event.target.value)}
        value={value}
      >
        <option value="all">{t('instance.catalog.common.allDatabases')}</option>
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
  const t = useT()
  const names = truncated.filter((name) => database === 'all' || name === database)
  if (names.length === 0) return null
  return (
    <ToolbarNote status wrap>
      {t('instance.catalog.truncated', {
        names: names.join(', '),
        count: names.length,
        what,
        kept,
      })}
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
function triggerStateLabel(state: Trigger['state'], t: Translate): string {
  if (state === 'enabled') return t('instance.catalog.common.enabled')
  if (state === 'unknown') return t('instance.catalog.common.unknown')
  return state
}

function triggerStateBadge(trigger: Trigger, t: Translate) {
  if (trigger.state === 'disabled') {
    return (
      <Badge variant={trigger.kind === 'internal' ? 'destructive' : 'secondary'}>
        {t('instance.catalog.triggers.disabled')}
      </Badge>
    )
  }
  return <Badge variant="outline">{triggerStateLabel(trigger.state, t)}</Badge>
}

// Every tab has its Database select above the table: no database filter here.
const TRIGGER_TOOLBAR: DataTableToolbar<Trigger> = {
  searchFields: [
    (trigger) => trigger.name,
    (trigger) => `${trigger.schemaname}.${trigger.relname}`,
    (trigger) => trigger.function,
  ],
}

function triggerColumns(t: Translate): DataTableProps<Trigger>['columns'] {
  return [
    {
      accessorKey: 'datname',
      header: t('instance.catalog.common.database'),
      meta: { width: '9rem', truncate: true },
    },
    {
      id: 'table',
      header: t('instance.catalog.common.table'),
      accessorFn: (trigger) => `${trigger.schemaname}.${trigger.relname}`,
      meta: { truncate: true },
    },
    {
      accessorKey: 'name',
      header: t('instance.catalog.triggers.trigger'),
      meta: { truncate: true },
    },
    {
      accessorKey: 'state',
      header: t('instance.catalog.common.state'),
      meta: { width: '7.5rem' },
      cell: ({ row }) => triggerStateBadge(row.original, t),
    },
    {
      id: 'enforces',
      header: t('instance.catalog.triggers.enforces'),
      accessorFn: (trigger) =>
        trigger.kind === 'internal'
          ? `${trigger.constraint_type || 'constraint'} ${trigger.constraint}`.trim()
          : '',
      meta: { truncate: true },
    },
    {
      id: 'fires',
      header: t('instance.catalog.triggers.fires'),
      accessorFn: (trigger) => `${trigger.timing} ${trigger.events} FOR EACH ${trigger.level}`,
      meta: { truncate: true },
    },
    {
      accessorKey: 'function',
      header: t('instance.catalog.common.function'),
      meta: { truncate: true },
    },
    {
      accessorKey: 'changed_at',
      header: t('instance.catalog.triggers.stateSince'),
      meta: { width: TIMESTAMP_COLUMN_WIDTH },
      cell: ({ row }) => formatTimestamp(row.original.changed_at),
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
  const t = useT()
  const columns = useMemo(() => triggerColumns(t), [t])
  const emptyState =
    response.triggers.length === 0 ? (
      <EmptyState
        title={t('instance.catalog.triggers.emptyTitle')}
        description={t('instance.catalog.triggers.emptyDescription')}
      />
    ) : (
      <EmptyState
        title={t('instance.catalog.triggers.noMatchTitle')}
        description={t('instance.catalog.triggers.noMatchDescription')}
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
            <span>{t('instance.catalog.triggers.disabledOnly')}</span>
          </label>
        </>
      }
      notes={
        <>
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what={t('instance.catalog.noun.triggers')}
            kept={t('instance.catalog.kept.triggers')}
          />
        </>
      }
      captionId="catalog.triggers"
      ariaLabel={t('instance.catalog.table.triggers')}
      tableId="triggers"
      toolbar={TRIGGER_TOOLBAR}
      columns={columns}
      data={rows}
      emptyState={emptyState}
      scope={t('instance.catalog.noun.triggers')}
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

function foreignKeyColumns(
  t: Translate,
): DataTableProps<Schemas['UnindexedForeignKey']>['columns'] {
  return [
    {
      accessorKey: 'datname',
      header: t('instance.catalog.common.database'),
      meta: { width: '9rem', truncate: true },
    },
    {
      id: 'table',
      header: t('instance.catalog.common.table'),
      accessorFn: (fk) => `${fk.schemaname}.${fk.relname}`,
      meta: { truncate: true },
    },
    {
      accessorKey: 'constraint',
      header: t('instance.catalog.fk.constraint'),
      meta: { truncate: true },
    },
    {
      accessorKey: 'columns',
      header: t('instance.catalog.fk.columns'),
      meta: {
        truncate: true,
        title: titleOf<Schemas['UnindexedForeignKey']>((fk) => String(fk.columns)),
      },
    },
    {
      accessorKey: 'referenced',
      header: t('instance.catalog.fk.references'),
      meta: { truncate: true },
    },
    {
      accessorKey: 'table_bytes',
      header: t('instance.catalog.fk.tableSize'),
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
  const t = useT()
  const columns = useMemo(() => foreignKeyColumns(t), [t])

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
          <ToolbarNote>{t('instance.catalog.fk.note')}</ToolbarNote>
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what={t('instance.catalog.noun.unindexedForeignKeys')}
            kept={t('instance.catalog.kept.foreignKeys')}
          />
        </>
      }
      captionId="catalog.foreign-keys"
      ariaLabel={t('instance.catalog.table.unindexedForeignKeys')}
      tableId="unindexed-foreign-keys"
      toolbar={FOREIGN_KEY_TOOLBAR}
      columns={columns}
      data={filter.rows}
      emptyState={
        <EmptyState
          title={t('instance.catalog.fk.emptyTitle')}
          description={t('instance.catalog.fk.emptyDescription')}
        />
      }
      scope={t('instance.catalog.noun.foreignKeys')}
      total={response.foreign_keys.length}
    />
  )
}

function extensionToolbar(t: Translate): DataTableToolbar<Schemas['Extension']> {
  return {
    searchFields: [(extension) => extension.name, (extension) => extension.schema],
    filters: [
      {
        id: 'schema',
        label: t('instance.catalog.common.schema'),
        kind: 'select',
        accessor: (extension) => extension.schema,
      },
    ],
  }
}

function extensionColumns(t: Translate): DataTableProps<Schemas['Extension']>['columns'] {
  return [
    {
      accessorKey: 'datname',
      header: t('instance.catalog.common.database'),
      meta: { width: '9rem', truncate: true },
    },
    { accessorKey: 'name', header: t('instance.catalog.ext.extension'), meta: { truncate: true } },
    {
      accessorKey: 'schema',
      header: t('instance.catalog.common.schema'),
      meta: { width: '9rem', truncate: true },
    },
    {
      accessorKey: 'version',
      header: t('instance.catalog.ext.installed'),
      meta: { width: '9rem', truncate: true },
    },
    {
      accessorKey: 'default_version',
      header: t('instance.catalog.ext.packages'),
      meta: { width: '11rem', help: 'catalog.extension-packages' },
      cell: ({ row }) =>
        row.original.default_version || (
          <Unknown reason={t('instance.catalog.ext.packagesMissing')} />
        ),
    },
    {
      accessorKey: 'upgrade',
      header: t('instance.catalog.ext.update'),
      meta: { width: '10.5rem', help: 'catalog.extension-update' },
      cell: ({ row }) =>
        row.original.upgrade === 'available' ? (
          <Badge variant="secondary">{t('instance.catalog.ext.available')}</Badge>
        ) : row.original.upgrade === 'unknown' ? (
          <Unknown reason={t('instance.catalog.ext.versionsIncomparable')} />
        ) : (
          t('instance.catalog.ext.upToDate')
        ),
    },
  ]
}

function ExtensionsTab({ response }: { response: Schemas['ExtensionsResponse'] }) {
  const filter = useDatabaseFilter(response.extensions)
  const t = useT()
  const columns = useMemo(() => extensionColumns(t), [t])
  const toolbar = useMemo(() => extensionToolbar(t), [t])

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
            what={t('instance.catalog.noun.extensions')}
            kept={t('instance.catalog.kept.extensions')}
          />
        </>
      }
      captionId="catalog.extensions"
      ariaLabel={t('instance.catalog.table.extensions')}
      tableId="extensions"
      toolbar={toolbar}
      columns={columns}
      data={filter.rows}
      emptyState={
        <EmptyState
          title={t('instance.catalog.ext.emptyTitle')}
          description={t('instance.catalog.ext.emptyDescription')}
        />
      }
      scope={t('instance.catalog.noun.extensions')}
      total={response.extensions.length}
    />
  )
}

function sridCell(column: SpatialColumn, t: Translate): ReactNode {
  if (column.srid === null) {
    return <Unknown reason={t('instance.catalog.spatial.noTypmod')} />
  }
  if (column.srid_known === false) {
    return (
      <span className="inline-flex items-center gap-1">
        {column.srid}
        <Badge variant="secondary">
          {column.srid === 0
            ? t('instance.catalog.common.unknown')
            : t('instance.catalog.spatial.undefined')}
        </Badge>
      </span>
    )
  }
  return column.srid
}

function spatialToolbar(t: Translate): DataTableToolbar<SpatialColumn> {
  return {
    searchFields: [(column) => `${column.schemaname}.${column.relname}`, (column) => column.column],
    filters: [
      {
        id: 'type',
        label: t('instance.catalog.common.type'),
        kind: 'select',
        accessor: (column) => `${column.type}(${column.subtype})`,
      },
      {
        id: 'index',
        label: t('instance.catalog.common.spatialIndex'),
        kind: 'select',
        accessor: (column) => column.index_method || 'missing',
      },
    ],
  }
}

function spatialColumns(t: Translate): DataTableProps<SpatialColumn>['columns'] {
  return [
    {
      accessorKey: 'datname',
      header: t('instance.catalog.common.database'),
      meta: { width: '9rem', truncate: true },
    },
    {
      id: 'table',
      header: t('instance.catalog.common.table'),
      accessorFn: (column) => `${column.schemaname}.${column.relname}`,
      meta: { truncate: true },
    },
    {
      accessorKey: 'column',
      header: t('instance.catalog.common.column'),
      meta: { truncate: true },
    },
    {
      id: 'declared',
      header: t('instance.catalog.common.type'),
      accessorFn: (column) => `${column.type}(${column.subtype})`,
      meta: { truncate: true },
    },
    {
      accessorKey: 'srid',
      header: 'SRID',
      meta: { width: '8rem' },
      cell: ({ row }) => sridCell(row.original, t),
    },
    {
      accessorKey: 'index_method',
      header: t('instance.catalog.common.spatialIndex'),
      meta: { width: '11rem' },
      cell: ({ row }) =>
        row.original.index_method ? (
          row.original.index_method
        ) : (
          <Badge variant="secondary">{t('instance.catalog.spatial.missing')}</Badge>
        ),
    },
    {
      accessorKey: 'rows',
      header: t('instance.catalog.common.rowsEstimate'),
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
  const t = useT()
  const columns = useMemo(() => spatialColumns(t), [t])
  const toolbar = useMemo(() => spatialToolbar(t), [t])
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
              aria-label={t('instance.catalog.spatial.versionsAria')}
            >
              {postgis.map((extension) => (
                <li key={extension.datname} className="min-[1600px]:mr-3 min-[1600px]:inline">
                  {t('instance.catalog.spatial.versionLine', {
                    datname: extension.datname,
                    version: extension.version,
                  })}
                  {extension.upgrade === 'available'
                    ? t('instance.catalog.spatial.packagesCarry', {
                        version: String(extension.default_version),
                      })
                    : null}
                </li>
              ))}
            </ul>
          ) : null}
          <TruncatedNotice
            truncated={spatial.truncated_databases}
            database={filter.database}
            what={t('instance.catalog.noun.spatialColumns')}
            kept={t('instance.catalog.kept.spatialColumns')}
          />
        </>
      }
      captionId="catalog.spatial-columns"
      ariaLabel={t('instance.catalog.table.spatialColumns')}
      tableId="spatial-columns"
      toolbar={toolbar}
      columns={columns}
      data={filter.rows}
      emptyState={
        <EmptyState
          title={t('instance.catalog.spatial.emptyTitle')}
          description={t('instance.catalog.spatial.emptyDescription')}
        />
      }
      scope={t('instance.catalog.noun.spatialColumns')}
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

function sequenceUsedCell(sequence: Sequence, t: Translate): ReactNode {
  switch (sequence.state) {
    case 'hidden':
      return <Unknown reason={t('instance.catalog.seq.unknownHidden')} />
    case 'unread':
      return <Unknown reason={t('instance.catalog.seq.unknownUnread')} />
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

function sequenceToolbar(t: Translate): DataTableToolbar<Sequence> {
  return {
    searchFields: [
      (sequence) => `${sequence.schemaname}.${sequence.name}`,
      (sequence) =>
        sequence.column ? `${sequence.table_schema}.${sequence.table}.${sequence.column}` : null,
    ],
    filters: [
      {
        id: 'type',
        label: t('instance.catalog.common.type'),
        kind: 'select',
        accessor: (sequence) => sequence.data_type,
      },
    ],
  }
}

function sequenceColumns(t: Translate): DataTableProps<Sequence>['columns'] {
  return [
    {
      accessorKey: 'datname',
      header: t('instance.catalog.common.database'),
      meta: { width: '9rem', truncate: true },
    },
    {
      id: 'sequence',
      header: t('instance.catalog.seq.sequence'),
      accessorFn: (sequence) => `${sequence.schemaname}.${sequence.name}`,
      meta: { width: '20rem', truncate: true },
      cell: ({ row }) => (
        <span className="flex min-w-0 items-center gap-1">
          <span className="min-w-0 truncate">
            {row.original.schemaname}.{row.original.name}
          </span>
          {row.original.cycle ? (
            <Badge className="shrink-0" variant="outline">
              {t('instance.catalog.seq.cycles')}
            </Badge>
          ) : null}
        </span>
      ),
    },
    {
      accessorKey: 'data_type',
      header: t('instance.catalog.common.type'),
      meta: { width: '7.5rem' },
    },
    {
      id: 'column',
      header: t('instance.catalog.seq.feeds'),
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
        if (!shown)
          return <span className="text-muted-foreground">{t('instance.catalog.seq.noColumn')}</span>
        const more = row.original.columns > 1 ? ` +${row.original.columns - 1}` : ''
        return `${shown}${more}`
      },
    },
    {
      id: 'last_value',
      header: t('instance.catalog.seq.lastValue'),
      meta: { width: '12.5rem' },
      accessorFn: (sequence) => sequence.last_value,
      cell: ({ row }) =>
        row.original.last_value !== null ? (
          formatIntegerString(row.original.last_value)
        ) : row.original.state === 'unused' ? (
          <span className="text-muted-foreground">{t('instance.catalog.seq.neverCalled')}</span>
        ) : (
          <Unknown reason={t('instance.catalog.seq.notReadable')} />
        ),
    },
    {
      accessorKey: 'limit',
      header: t('instance.catalog.seq.limit'),
      meta: { width: '18.5rem', help: 'catalog.sequence-limit' },
      cell: ({ row }) => (
        <span className="inline-flex items-center gap-1">
          {formatIntegerString(row.original.limit)}
          {row.original.limit_source === 'column' ? (
            <Badge variant="secondary">{t('instance.catalog.seq.columnType')}</Badge>
          ) : null}
        </span>
      ),
    },
    {
      id: 'used',
      header: t('instance.catalog.seq.used'),
      meta: { width: '9.5rem', help: 'catalog.sequence-used' },
      accessorFn: (sequence) => sequence.used_ratio,
      cell: ({ row }) => sequenceUsedCell(row.original, t),
    },
    {
      id: 'remaining',
      header: t('instance.catalog.seq.remaining'),
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
  const t = useT()
  const columns = useMemo(() => sequenceColumns(t), [t])
  const toolbar = useMemo(() => sequenceToolbar(t), [t])
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
          <ToolbarNote>{t('instance.catalog.seq.note')}</ToolbarNote>
          {hidden > 0 ? (
            <ToolbarNote status>{t('instance.catalog.seq.hidden', { count: hidden })}</ToolbarNote>
          ) : null}
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what={t('instance.catalog.noun.sequences')}
            kept={t('instance.catalog.kept.sequences')}
          />
        </>
      }
      captionId="catalog.sequences"
      ariaLabel={t('instance.catalog.table.sequences')}
      tableId="sequences"
      toolbar={toolbar}
      columns={columns}
      data={filter.rows}
      emptyState={
        <EmptyState
          title={t('instance.catalog.seq.emptyTitle')}
          description={t('instance.catalog.seq.emptyDescription')}
        />
      }
      scope={t('instance.catalog.noun.sequences')}
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

function coveredCell(table: PartitionedTable, now: Date, t: Translate): ReactNode {
  if (table.covered_until === null) return null
  if (table.covered_until === 'unbounded') return t('instance.catalog.part.noEnd')
  const ahead = partitionAhead(table, now)
  const shown = formatTimestamp(table.covered_until)
  if (ahead === null || ahead >= PARTITION_HORIZON_MS) return shown
  if (ahead < -PARTITION_LAPSED_MS) {
    return (
      <span>
        {shown}{' '}
        <span className="text-muted-foreground">{t('instance.catalog.part.endedParen')}</span>
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1">
      {shown}
      <Badge variant={ahead <= 0 && table.default_partition === null ? 'destructive' : 'secondary'}>
        {ahead <= 0 ? t('instance.catalog.part.ended') : t('instance.catalog.part.endsSoon')}
      </Badge>
    </span>
  )
}

const PARTITION_TOOLBAR: DataTableToolbar<PartitionedTable> = {
  searchFields: [(table) => `${table.schemaname}.${table.relname}`, (table) => table.partition_key],
}

function partitionColumns(now: Date, t: Translate): DataTableProps<PartitionedTable>['columns'] {
  return [
    {
      accessorKey: 'datname',
      header: t('instance.catalog.common.database'),
      meta: { width: '9rem', truncate: true },
    },
    {
      id: 'table',
      header: t('instance.catalog.common.table'),
      accessorFn: (table) => `${table.schemaname}.${table.relname}`,
      meta: { truncate: true },
    },
    {
      accessorKey: 'partition_key',
      header: t('instance.catalog.part.key'),
      meta: { truncate: true },
    },
    {
      accessorKey: 'partitions',
      header: t('instance.catalog.part.partitions'),
      meta: { width: '8.5rem' },
      cell: ({ row }) =>
        row.original.depth > 1
          ? t('instance.catalog.part.levels', {
              count: formatCount(row.original.partitions),
              depth: row.original.depth,
            })
          : formatCount(row.original.partitions),
    },
    {
      accessorKey: 'bytes',
      header: t('instance.catalog.part.sizeEstimate'),
      meta: { width: '8rem' },
      cell: ({ row }) => formatBytes(row.original.bytes),
    },
    {
      accessorKey: 'rows',
      header: t('instance.catalog.common.rowsEstimate'),
      cell: ({ row }) => formatCount(row.original.rows),
      meta: { width: '8.75rem' },
    },
    {
      id: 'default',
      header: t('instance.catalog.part.defaultPartition'),
      accessorFn: (table) => table.default_rows,
      meta: {
        width: '12rem',
        truncate: true,
        title: titleOf<PartitionedTable>((table) =>
          table.default_partition === null
            ? t('instance.catalog.part.none')
            : `${table.default_partition}${(table.default_rows ?? 0) > 0 ? ` (${t('instance.catalog.part.rowsCount', { count: formatCount(table.default_rows ?? 0) })})` : ''}`,
        ),
      },
      cell: ({ row }) => {
        const { default_partition: name, default_rows: rows } = row.original
        if (name === null)
          return <span className="text-muted-foreground">{t('instance.catalog.part.none')}</span>
        if ((rows ?? 0) <= 0) return name
        return (
          <span className="inline-flex items-center gap-1">
            {name}
            <Badge variant="secondary">
              {t('instance.catalog.part.rowsCount', { count: formatCount(rows ?? 0) })}
            </Badge>
          </span>
        )
      },
    },
    {
      id: 'invalid',
      header: t('instance.catalog.part.invalidIndexes'),
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
      header: t('instance.catalog.part.analyzed'),
      accessorFn: (table) => table.analyzed_at,
      meta: {
        width: '9rem',
        truncate: true,
        title: titleOf<PartitionedTable>((table) =>
          table.analyzed_at === null
            ? t('instance.catalog.part.never')
            : formatTimestamp(table.analyzed_at),
        ),
      },
      cell: ({ row }) =>
        row.original.analyzed_at !== null ? (
          formatTimestamp(row.original.analyzed_at)
        ) : row.original.rows >= PARTITION_ANALYZE_ROWS ? (
          <Badge variant="secondary">{t('instance.catalog.part.never')}</Badge>
        ) : (
          <span className="text-muted-foreground">{t('instance.catalog.part.never')}</span>
        ),
    },
    {
      id: 'covered',
      header: t('instance.catalog.part.reach'),
      accessorFn: (table) => table.covered_until,
      meta: {
        help: 'catalog.partitions-reach',
        width: '14rem',
        truncate: true,
        title: titleOf<PartitionedTable>((table) =>
          table.covered_until === null
            ? ''
            : table.covered_until === 'unbounded'
              ? t('instance.catalog.part.noEnd')
              : formatTimestamp(table.covered_until),
        ),
      },
      cell: ({ row }) => coveredCell(row.original, now, t),
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
  const t = useT()
  const columns = useMemo(() => partitionColumns(now, t), [now, t])

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
          <ToolbarNote>{t('instance.catalog.part.note')}</ToolbarNote>
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what={t('instance.catalog.noun.partitionedTables')}
            kept={t('instance.catalog.kept.partitionedTables')}
          />
        </>
      }
      // Ten sortable columns fit 1600 px only with the dense cells (D26).
      dense
      captionId="catalog.partitions"
      ariaLabel={t('instance.catalog.table.partitionedTables')}
      tableId="partitioned-tables"
      toolbar={PARTITION_TOOLBAR}
      columns={columns}
      data={filter.rows}
      emptyState={
        <EmptyState
          title={t('instance.catalog.part.emptyTitle')}
          description={t('instance.catalog.part.emptyDescription')}
        />
      }
      scope={t('instance.catalog.noun.partitionedTables')}
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

function functionColumns(t: Translate): DataTableProps<FunctionStat>['columns'] {
  return [
    {
      accessorKey: 'datname',
      header: t('instance.catalog.common.database'),
      meta: { width: '9rem', truncate: true },
    },
    {
      id: 'function',
      header: t('instance.catalog.common.function'),
      accessorFn: functionName,
      meta: { truncate: true },
    },
    {
      accessorKey: 'language',
      header: t('instance.catalog.fn.language'),
      meta: { width: '9.25rem' },
    },
    {
      accessorKey: 'calls_per_second',
      header: t('instance.catalog.fn.calls'),
      meta: { width: '7.5rem' },
      cell: ({ row }) => formatRate(row.original.calls_per_second),
    },
    {
      accessorKey: 'self_ms_per_second',
      header: t('instance.catalog.fn.ownTime'),
      meta: { width: '11rem', help: 'catalog.function-own-time' },
      cell: ({ row }) => `${formatMilliseconds(row.original.self_ms_per_second)}/s`,
    },
    {
      accessorKey: 'total_ms_per_second',
      header: t('instance.catalog.fn.totalTime'),
      meta: { width: '9.75rem' },
      cell: ({ row }) => `${formatMilliseconds(row.original.total_ms_per_second)}/s`,
    },
    {
      id: 'mean',
      header: t('instance.catalog.fn.meanPerCall'),
      meta: { width: '11rem' },
      accessorFn: (fn) => fn.mean_ms,
      cell: ({ row }) =>
        row.original.mean_ms === null ? (
          <span className="text-muted-foreground">{t('instance.catalog.fn.notCalled')}</span>
        ) : (
          formatMilliseconds(row.original.mean_ms)
        ),
    },
  ]
}

function FunctionsTab({ response }: { response: Schemas['FunctionsResponse'] }) {
  const filter = useDatabaseFilter(response.functions)
  const t = useT()
  const columns = useMemo(() => functionColumns(t), [t])
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
          <ToolbarNote>{t('instance.catalog.fn.note', { minutes })}</ToolbarNote>
          {untracked.length > 0 ? (
            <ToolbarNote status wrap>
              {t('instance.catalog.fn.untracked', { names: untracked.join(', ') })}
            </ToolbarNote>
          ) : null}
        </>
      }
      captionId="catalog.functions"
      ariaLabel={t('instance.catalog.table.functions')}
      tableId="functions"
      toolbar={FUNCTION_TOOLBAR}
      columns={columns}
      data={filter.rows}
      emptyState={
        <EmptyState
          title={t('instance.catalog.fn.emptyTitle')}
          description={t('instance.catalog.fn.emptyDescription')}
        />
      }
      scope={t('instance.catalog.noun.functions')}
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

function securityDefinerColumns(t: Translate): DataTableProps<SecurityDefinerFunction>['columns'] {
  return [
    {
      accessorKey: 'datname',
      header: t('instance.catalog.common.database'),
      meta: { width: '9rem', truncate: true },
    },
    {
      id: 'function',
      header: t('instance.catalog.common.function'),
      accessorFn: functionName,
      meta: { truncate: true, title: titleOf<SecurityDefinerFunction>(functionName) },
      cell: ({ row }) => (
        <span className="inline-flex items-center gap-1">
          {functionName(row.original)}
          {row.original.kind === 'procedure' ? (
            <Badge variant="outline">{t('instance.catalog.sd.procedure')}</Badge>
          ) : null}
        </span>
      ),
    },
    {
      accessorKey: 'owner',
      header: t('instance.catalog.sd.runsAs'),
      meta: {
        width: '12rem',
        truncate: true,
        title: titleOf<SecurityDefinerFunction>((fn) => fn.owner),
      },
      cell: ({ row }) => (
        <span className="inline-flex items-center gap-1">
          {row.original.owner}
          {row.original.owner_superuser ? (
            <Badge variant="secondary">{t('instance.catalog.sd.superuser')}</Badge>
          ) : null}
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
          fn.search_path === null ? t('instance.catalog.sd.callerPath') : fn.search_path || '""',
        ),
      },
      cell: ({ row }) => {
        const fn = row.original
        if (fn.search_path === null) {
          return (
            <Badge variant={securityDefinerExposed(fn) ? 'destructive' : 'secondary'}>
              {t('instance.catalog.sd.callerPath')}
            </Badge>
          )
        }
        return (
          <span className="inline-flex items-center gap-1">
            <code className="text-xs">{fn.search_path === '' ? '""' : fn.search_path}</code>
            {fn.pg_temp_last ? null : (
              <span className="text-muted-foreground text-xs">
                {t('instance.catalog.sd.pgTempFirst')}
              </span>
            )}
          </span>
        )
      },
    },
    {
      id: 'public',
      header: t('instance.catalog.sd.executableBy'),
      meta: { width: '11.5rem' },
      accessorFn: (fn) => (fn.public_execute ? 1 : 0),
      cell: ({ row }) =>
        row.original.public_execute
          ? t('instance.catalog.sd.everyRole')
          : t('instance.catalog.sd.grantedRoles'),
    },
  ]
}

function SecurityDefinerTab({
  response,
}: {
  response: Schemas['SecurityDefinerFunctionsResponse']
}) {
  const filter = useDatabaseFilter(response.functions)
  const t = useT()
  const columns = useMemo(() => securityDefinerColumns(t), [t])

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
          <ToolbarNote>{t('instance.catalog.sd.note')}</ToolbarNote>
          <TruncatedNotice
            truncated={response.truncated_databases}
            database={filter.database}
            what={t('instance.catalog.noun.securityDefiner')}
            kept={t('instance.catalog.kept.securityDefiner')}
          />
        </>
      }
      captionId="catalog.security-definer"
      ariaLabel={t('instance.catalog.table.securityDefiner')}
      tableId="security-definer-functions"
      toolbar={SECURITY_DEFINER_TOOLBAR}
      columns={columns}
      data={filter.rows}
      emptyState={
        <EmptyState
          title={t('instance.catalog.sd.emptyTitle')}
          description={t('instance.catalog.sd.emptyDescription')}
        />
      }
      scope={t('instance.catalog.noun.functions')}
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
  const t = useT()
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
      <section
        aria-busy="true"
        aria-label={t('instance.catalog.tabs.loadingAria', { label })}
        role="status"
      >
        {t('instance.catalog.tabs.loading', { label })}
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
  const t = useT()
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
          {t('instance.catalog.tabs.triggers')}{' '}
          <TabCount count={disabledTriggers} label={t('instance.catalog.triggers.disabled')} />
        </>
      ),
    },
    {
      id: 'sequences',
      label: (
        <>
          {t('instance.catalog.tabs.sequences')}{' '}
          <TabCount count={sequencesAtRisk} label={t('instance.catalog.tab.nearLimit')} />
        </>
      ),
    },
    {
      id: 'partitions',
      label: (
        <>
          {t('instance.catalog.tabs.partitions')}{' '}
          <TabCount count={partitionIssues} label={t('instance.catalog.tab.needAttention')} />
        </>
      ),
    },
    { id: 'functions', label: t('instance.catalog.tabs.functions') },
    {
      id: 'security-definer',
      label: (
        <>
          {t('instance.catalog.tabs.securityDefiner')}{' '}
          <TabCount count={unpinnedFunctions} label={t('instance.catalog.tab.unpinned')} />
        </>
      ),
    },
    {
      id: 'foreign-keys',
      label: (
        <>
          {t('instance.catalog.tabs.foreignKeys')}{' '}
          <TabCount
            count={foreignKeysQuery.data?.foreign_keys.length ?? 0}
            label={t('instance.catalog.tab.reported')}
          />
        </>
      ),
    },
    {
      id: 'extensions',
      label: (
        <>
          {t('instance.catalog.tabs.extensions')}{' '}
          <TabCount count={upgradableExtensions} label={t('instance.catalog.tab.withUpdate')} />
        </>
      ),
    },
    {
      id: 'postgis',
      label: (
        <>
          {t('instance.catalog.tabs.postgis')}{' '}
          <TabCount count={unindexedSpatial} label={t('instance.catalog.tab.unindexed')} />
        </>
      ),
    },
  ]

  return (
    // One object kind per view, in the URL (15.3); Triggers is the default.
    <Tabs onValueChange={showView} value={view}>
      <InstancePanel
        id="catalog"
        title={t('instance.catalog.panel.title')}
        captionId="catalog.section"
        actions={
          <>
            <InstanceViewTabs label={t('instance.catalog.panel.kinds')} views={views} />
            <HelpLink
              anchor="instance-overview#catalog"
              label={t('instance.catalog.panel.title')}
            />
          </>
        }
      >
        <TabsContent value="triggers">
          <CatalogTabState query={triggersQuery} label={t('instance.catalog.noun.triggers')}>
            {(data) => <TriggersTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="sequences">
          <CatalogTabState query={sequencesQuery} label={t('instance.catalog.noun.sequences')}>
            {(data) => <SequencesTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="partitions">
          <CatalogTabState
            query={partitionsQuery}
            label={t('instance.catalog.noun.partitionedTables')}
          >
            {(data) => <PartitionsTab response={data} now={now} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="functions">
          <CatalogTabState query={functionsQuery} label={t('instance.catalog.noun.functions')}>
            {(data) => <FunctionsTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="security-definer">
          <CatalogTabState
            query={securityDefinerQuery}
            label={t('instance.catalog.noun.securityDefiner')}
          >
            {(data) => <SecurityDefinerTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="foreign-keys">
          <CatalogTabState
            query={foreignKeysQuery}
            label={t('instance.catalog.noun.unindexedForeignKeys')}
          >
            {(data) => <UnindexedForeignKeysTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="extensions">
          <CatalogTabState query={extensionsQuery} label={t('instance.catalog.noun.extensions')}>
            {(data) => <ExtensionsTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="postgis">
          <CatalogTabState query={spatialQuery} label={t('instance.catalog.noun.spatialColumns')}>
            {(data) => <PostGISTab spatial={data} extensions={extensionsQuery.data} />}
          </CatalogTabState>
        </TabsContent>
      </InstancePanel>
    </Tabs>
  )
}
