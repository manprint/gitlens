import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, type ReactNode } from 'react'

import type { ApiFailure } from '@/api/client'
import { useCreateCommand } from '@/api/commands'
import { qk } from '@/api/keys'
import { useInstanceBloat, useInstanceIndexes, useInstanceTables } from '@/api/queries'
import type { PermTier, Schemas } from '@/api/types'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState, NotPermitted, Unknown } from '@/components/state'
import {
  commandQueueFullMessage,
  describeCommandState,
  isTerminalCommandState,
} from '@/lib/commands'
import { formatBytes, formatCount, formatPercent, formatRate, formatTimestamp } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import type { CaptionId, ColumnHelpId } from '@/lib/captions'

import { DENSE_TABLE } from './denseTable'
import { CaptionedBlock, InstancePanel, InstanceViewTabs, ToolbarNote } from './InstancePanel'
import { RELATION_VIEW_IDS } from './instanceSections'
import { useInstanceView, type InstanceView } from './instanceViews'

const RELATION_LIMIT = 50

type RelationResponse = Schemas['RelationResponse']
type BloatResponse = Schemas['BloatResponse']
type RelationItem = RelationResponse['items'][number]

interface RelationColumn {
  key: string
  label: string
  render: (item: RelationItem) => ReactNode
  /** Absent for action columns, which have nothing to sort by. */
  sortValue?: (item: RelationItem) => string | number | null
  /** Column width from 1600 px (D26). */
  width?: string
  /** A free-text column: one line from 1600 px, the full text in its title. */
  truncate?: (item: RelationItem) => string
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
    width: '7rem',
    truncate: (item) => {
      const latest = latestMaintenance(item, manualKey, autoKey)
      return latest
        ? `${formatTimestamp(latest.iso, 'UTC')} (${latest.auto ? 'auto' : 'manual'})`
        : ''
    },
  }
}

function isTableRow(item: RelationItem): boolean {
  const kind = stringValue(item, 'object_kind')
  return kind ? kind === 'table' : !stringValue(item, 'indexrelname')
}

/** The full text of a cell that is truncated from 1600 px. */
function textOf(value: string | null): string {
  return value ?? ''
}

/** URL prefix of each relation list (tables inventory). */
const RELATION_TABLE_IDS: Record<string, string> = {
  Tables: 'relations-tables',
  Indexes: 'relations-indexes',
  'Table bloat estimates': 'relations-table-bloat',
  'Index bloat estimates': 'relations-index-bloat',
}

// The four lists are already split by relation kind: no kind filter (tables inventory, §8).
const RELATION_TOOLBAR: TableToolbarSpec<RelationItem> = {
  searchFields: [
    (item) => relationName(item),
    (item) => stringValue(item, 'indexrelname'),
    (item) => stringValue(item, 'datname'),
  ],
  filters: [
    {
      id: 'database',
      label: 'Database',
      kind: 'select',
      accessor: (item) => stringValue(item, 'datname'),
    },
    {
      id: 'schema',
      label: 'Schema',
      kind: 'select',
      accessor: (item) => stringValue(item, 'schemaname'),
    },
  ],
}

function relationKey(item: RelationItem): string {
  return ['datname', 'schemaname', 'relname', 'indexrelname', 'object_kind']
    .map((key) => stringValue(item, key) ?? '')
    .join('|')
}

/** The hint of a column, by its key; keys not listed carry none. */
const COLUMN_HELP: Record<string, ColumnHelpId> = {
  relation: 'relations.relation',
  live_tuples: 'relations.live-tuples',
  dead_tuples: 'relations.dead-tuples',
  dead_ratio: 'relations.dead-ratio',
  seq_scan_rate: 'relations.seq-scan-rate',
  idx_scan_rate: 'relations.idx-scan-rate',
  last_vacuum: 'relations.last-vacuum',
  last_analyze: 'relations.last-analyze',
  last_scan: 'relations.last-scan',
  valid: 'relations.valid',
  real_bytes: 'relations.real-size',
  expected_bytes: 'relations.expected-size',
  bloat_bytes: 'relations.bloat',
  bloat_ratio: 'relations.bloat-ratio',
  method: 'relations.method',
}

function SortableRelationTable({
  caption,
  captionId,
  items,
  columns,
}: {
  caption: string
  captionId: CaptionId
  items: RelationItem[]
  columns: RelationColumn[]
}) {
  const simpleColumns = useMemo(
    () =>
      columns.map<SimpleColumn<RelationItem>>((column, index) => ({
        id: column.key,
        header: column.label,
        ...(column.sortValue ? { sortValue: column.sortValue } : {}),
        rowHeader: index === 0,
        className: index === 0 ? 'font-normal' : '',
        cell: column.render,
        ...(COLUMN_HELP[column.key] === undefined ? {} : { help: COLUMN_HELP[column.key] }),
        ...(column.width === undefined ? {} : { width: column.width }),
        ...(column.truncate === undefined ? {} : { truncate: column.truncate }),
      })),
    [columns],
  )
  const firstKey = columns[0]?.key
  // The first column, descending, as before the URL held the order.
  return (
    <SimpleTable
      ariaLabel={caption}
      captionId={captionId}
      columns={simpleColumns}
      emptyState={null}
      headerToggleName={(column) => `Sort ${caption} by ${column.header}`}
      rowKey={relationKey}
      rows={items}
      scope="relations"
      tableId={
        RELATION_TABLE_IDS[caption] ?? `relations-${caption.toLowerCase().replaceAll(' ', '-')}`
      }
      toolbar={RELATION_TOOLBAR}
      // Eleven sortable columns fit 1600 px only with the dense cells (D26).
      className={DENSE_TABLE}
      {...(firstKey === undefined ? {} : { defaultSort: { id: firstKey, desc: true } })}
    />
  )
}

const RELATION_COLUMN: RelationColumn = {
  key: 'relation',
  label: 'Relation',
  render: relationCell,
  sortValue: (item) => relationName(item),
  truncate: (item) => textOf(relationName(item)),
}

// The budget is shared across databases, so the same schema and table name
// can appear once per database; the database tells the rows apart.
const DATABASE_COLUMN: RelationColumn = {
  key: 'database',
  label: 'Database',
  render: (item) => textCell(item, 'datname'),
  sortValue: (item) => stringValue(item, 'datname'),
  width: '6.5rem',
  truncate: (item) => textOf(stringValue(item, 'datname')),
}

const OBSERVED_COLUMN: RelationColumn = {
  key: 'observed',
  label: 'Observed',
  render: timestampCell,
  sortValue: (item) => stringValue(item, 'ts'),
  width: '11.5rem',
  truncate: (item) => {
    const value = stringValue(item, 'ts')
    return value ? formatTimestamp(value, 'UTC') : ''
  },
}

function booleanColumn(key: string, label: string, field: string, width: string): RelationColumn {
  return {
    key,
    label,
    width,
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
    width: '7rem',
  },
  {
    key: 'dead_tuples',
    label: 'Dead tuples',
    render: (item) => countCell(item, 'n_dead_tup'),
    sortValue: (item) => numberValue(item, 'n_dead_tup'),
    width: '7rem',
  },
  {
    key: 'dead_ratio',
    label: 'Dead ratio',
    render: (item) => percentCell(item, 'dead_ratio'),
    sortValue: (item) => numberValue(item, 'dead_ratio'),
    width: '7rem',
  },
  {
    key: 'size',
    label: 'Size',
    render: (item) => bytesCell(item, 'total_bytes'),
    sortValue: (item) => numberValue(item, 'total_bytes'),
    width: '5.75rem',
  },
  {
    key: 'seq_scan_rate',
    label: 'Sequential scans',
    render: (item) => rateCell(item, 'seq_scan_rate'),
    sortValue: (item) => numberValue(item, 'seq_scan_rate'),
    width: '9.5rem',
  },
  {
    key: 'idx_scan_rate',
    label: 'Index scans',
    render: (item) => rateCell(item, 'idx_scan_rate'),
    sortValue: (item) => numberValue(item, 'idx_scan_rate'),
    width: '7.75rem',
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

/**
 * The index's last scan since the statistics reset, from the API's last_scan_status.
 * "never since reset" is an index with no scan since the reset (PostgreSQL 16 and
 * later); "not available" means the server has no last scan for it (PostgreSQL 15).
 */
const LAST_SCAN_STATUS_TEXT: Record<string, { text: string; title: string }> = {
  never_since_reset: {
    text: 'never since reset',
    title: 'No scan of this index since the statistics were reset.',
  },
  not_available: {
    text: 'not available',
    title:
      'PostgreSQL 15 does not report the last scan of an index (pg_stat_user_indexes.last_idx_scan).',
  },
}

function lastScanCell(item: RelationItem): ReactNode {
  const at = timeValue(item, 'last_scan_at')
  if (at) {
    return (
      <span
        className="min-[1600px]:block min-[1600px]:truncate"
        title={`Last scan of this index since the statistics were reset: ${formatTimestamp(at.iso, 'UTC')}.`}
      >
        {formatTimestamp(at.iso, 'UTC')}
      </span>
    )
  }
  const status = stringValue(item, 'last_scan_status')
  const known = status ? LAST_SCAN_STATUS_TEXT[status] : undefined
  return known ? <span title={known.title}>{known.text}</span> : <Unknown />
}

const INDEX_COLUMNS: RelationColumn[] = [
  RELATION_COLUMN,
  DATABASE_COLUMN,
  {
    key: 'index',
    label: 'Index',
    render: (item) => textCell(item, 'indexrelname'),
    sortValue: (item) => stringValue(item, 'indexrelname'),
    truncate: (item) => textOf(stringValue(item, 'indexrelname')),
  },
  {
    key: 'idx_scan_rate',
    label: 'Index scans',
    render: (item) => rateCell(item, 'idx_scan_rate'),
    sortValue: (item) => numberValue(item, 'idx_scan_rate'),
    width: '9.25rem',
  },
  {
    key: 'last_scan',
    label: 'Last scan',
    render: lastScanCell,
    sortValue: (item) => timeValue(item, 'last_scan_at')?.ms ?? null,
    width: '10rem',
  },
  {
    key: 'size',
    label: 'Size',
    render: (item) => bytesCell(item, 'index_bytes'),
    sortValue: (item) => numberValue(item, 'index_bytes'),
    width: '5.75rem',
  },
  booleanColumn('unique', 'Unique', 'is_unique', '7rem'),
  booleanColumn('primary', 'Primary', 'is_primary', '7.5rem'),
  booleanColumn('valid', 'Valid', 'is_valid', '6.25rem'),
  OBSERVED_COLUMN,
]

// A bloat row is a table or an index of a table. The relation column names the
// table in both cases; the index column names the index of an index row.
const BLOAT_TABLE_COLUMN: RelationColumn = { ...RELATION_COLUMN, label: 'Table' }

const BLOAT_INDEX_COLUMN: RelationColumn = {
  key: 'index',
  label: 'Index',
  render: (item) => textCell(item, 'indexrelname'),
  sortValue: (item) => stringValue(item, 'indexrelname'),
  truncate: (item) => textOf(stringValue(item, 'indexrelname')),
}

const BLOAT_MEASURE_COLUMNS: RelationColumn[] = [
  {
    key: 'real_bytes',
    label: 'Real size',
    render: (item) => bytesCell(item, 'real_bytes'),
    sortValue: (item) => numberValue(item, 'real_bytes'),
    width: '7.5rem',
  },
  {
    key: 'expected_bytes',
    label: 'Expected size',
    render: (item) => bytesCell(item, 'expected_bytes'),
    sortValue: (item) => numberValue(item, 'expected_bytes'),
    width: '10rem',
  },
  {
    key: 'bloat_bytes',
    label: 'Bloat',
    render: (item) => bytesCell(item, 'bloat_bytes'),
    sortValue: (item) => numberValue(item, 'bloat_bytes'),
    width: '6.5rem',
  },
  {
    key: 'bloat_ratio',
    label: 'Bloat ratio',
    render: (item) => percentCell(item, 'bloat_ratio'),
    sortValue: (item) => numberValue(item, 'bloat_ratio'),
    width: '9rem',
  },
  {
    key: 'method',
    label: 'Method',
    render: (item) => textCell(item, 'method'),
    sortValue: (item) => stringValue(item, 'method'),
    width: '7.5rem',
  },
  OBSERVED_COLUMN,
]

const TABLE_BLOAT_COLUMNS: RelationColumn[] = [
  BLOAT_TABLE_COLUMN,
  DATABASE_COLUMN,
  ...BLOAT_MEASURE_COLUMNS,
]

const INDEX_BLOAT_COLUMNS: RelationColumn[] = [
  BLOAT_TABLE_COLUMN,
  BLOAT_INDEX_COLUMN,
  DATABASE_COLUMN,
  ...BLOAT_MEASURE_COLUMNS,
]

/**
 * The agent sends only the top relations of each database, so a list can be
 * incomplete; the sentence is shown whole, once, above the table.
 */
function BudgetNotice({
  response,
  noun,
  configKey,
}: {
  response: RelationResponse
  noun: string
  configKey: string
}) {
  if (!response.truncated) return null
  return (
    <ToolbarNote status wrap>
      {`This list is incomplete: the agent reports only the top ${noun} of each database. Raise the limit with ${configKey}.`}
    </ToolbarNote>
  )
}

interface RelationPanelProps {
  title: string
  endpoint: string
  query: RelationQueryState
  columns: RelationColumn[]
  emptyDescription: string
  captionId: CaptionId
  noun: string
  configKey: string
}

function RelationPanel({
  title,
  endpoint,
  query,
  columns,
  emptyDescription,
  captionId,
  noun,
  configKey,
}: RelationPanelProps) {
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
    return (
      <CaptionedBlock ariaLabel={title} captionId={captionId}>
        <EmptyState title={`No ${title.toLowerCase()}`} description={emptyDescription} />
      </CaptionedBlock>
    )
  }
  return (
    <div className="space-y-3">
      <BudgetNotice response={query.data} noun={noun} configKey={configKey} />
      <SortableRelationTable
        caption={title}
        captionId={captionId}
        items={query.data.items}
        columns={columns}
      />
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
      void queryClient.invalidateQueries({
        queryKey: qk.instanceBloat(instanceId, RELATION_LIMIT, 'table'),
      })
    }
  }, [instanceId, queryClient, state])

  if (!isTableRow(item) || !schema || !relation) {
    return <Unknown reason="pgstattuple measures tables only." />
  }
  const name = `${schema}.${relation}`
  const running = command.isPending || (state !== undefined && !isTerminalCommandState(state))
  return (
    <div className="space-y-1">
      <Button
        className="min-[1600px]:h-5"
        size="xs"
        variant="outline"
        type="button"

        aria-label={`Measure exactly: ${name} with pgstattuple`}
        disabled={!permitted || running}
        onClick={() =>
          command.mutate({
            kind: 'pgstattuple',
            args: { schema, relation, ...(datname ? { datname } : {}) },
          })
        }
      >
        Measure exactly
      </Button>
      {command.command.data ? (
        <p role="status" className="text-xs">
          {describeCommandState(command.command.data.state, command.command.data.error)}
        </p>
      ) : null}
      {command.error ? (
        <p role="alert" className="text-destructive text-xs">
          Could not submit the measurement:{' '}
          {commandQueueFullMessage(command.error.failure) ?? command.error.message}
        </p>
      ) : null}
    </div>
  )
}

function tableBloatColumns(instanceId: string, permitted: boolean): RelationColumn[] {
  return [
    ...TABLE_BLOAT_COLUMNS,
    {
      key: 'exact',
      label: 'Exact measurement',
      width: '10rem',
      render: (item) => (
        <ExactMeasurement instanceId={instanceId} item={item} permitted={permitted} />
      ),
    },
  ]
}

/**
 * The tables the estimate could not read. Their bloat is unknown, not zero:
 * the monitoring role cannot see their column statistics in pg_stats, either
 * because a column is not readable by it or because row security hides rows.
 */
function SkippedTablesNotice({ response }: { response: BloatResponse | undefined }) {
  if (!response || response.skipped_tables < 1) return null
  const named = response.skipped_relations
  const more = response.skipped_tables - named.length
  const skipped = `${formatCount(response.skipped_tables)} ${
    response.skipped_tables === 1 ? 'table has' : 'tables have'
  } no bloat estimate: the monitoring role cannot read a column of them, or row security hides rows from it. Their bloat is unknown, not zero.`
  const largest =
    named.length > 0
      ? `Largest: ${named.map((t) => `${t.datname}: ${t.schemaname}.${t.relname}`).join(', ')}${
          more > 0 ? ` and ${formatCount(more)} more` : ''
        }.`
      : null
  // From 1600 px the notice is one truncated line beside the exact-measurement note (D26).
  return (
    <div
      className="text-muted-foreground space-y-1 text-sm min-[1600px]:min-w-0 min-[1600px]:flex-1 min-[1600px]:space-y-0 min-[1600px]:truncate min-[1600px]:text-xs"
      title={largest === null ? skipped : `${skipped} ${largest}`}
    >
      <p className="min-[1600px]:inline">{skipped}</p>
      {largest === null ? null : (
        <>
          {' '}
          <p className="min-[1600px]:inline">{largest}</p>
        </>
      )}
    </div>
  )
}

const EXACT_BLOAT_NOTE =
  'An exact measurement runs pgstattuple on the agent in a read-only transaction. It reads the whole table (bounded at 50 seconds) and needs the extension to be installed already in the database that holds the table: pglens never installs extensions, and the agent rejects the command when it is missing there.'

function ExactBloatNotice({ currentTier }: { currentTier: PermTier }) {
  return (
    <div className="text-muted-foreground space-y-2 text-sm min-[1600px]:flex min-[1600px]:min-w-0 min-[1600px]:flex-1 min-[1600px]:items-center min-[1600px]:gap-3 min-[1600px]:space-y-0 min-[1600px]:text-xs">
      <p className="min-[1600px]:line-clamp-2 min-[1600px]:min-w-0" title={EXACT_BLOAT_NOTE}>
        An exact measurement runs <code>pgstattuple</code> on the agent in a read-only transaction.
        It reads the whole table (bounded at 50 seconds) and needs the extension to be installed
        already in the database that holds the table: pglens never installs extensions, and the
        agent rejects the command when it is missing there.
      </p>
      {currentTier === 'T0' ? (
        <div className="min-[1600px]:shrink-0">
          <NotPermitted required="T1" current={currentTier} />
        </div>
      ) : null}
    </div>
  )
}

type RelationViewId = (typeof RELATION_VIEW_IDS)[number]

/** One relation list per view (15.3); Tables is the default. */
const RELATION_VIEWS: readonly InstanceView<RelationViewId>[] = [
  { id: 'tables', label: 'Tables' },
  { id: 'indexes', label: 'Indexes' },
  { id: 'table-bloat', label: 'Table bloat' },
  { id: 'index-bloat', label: 'Index bloat' },
]

export function RelationsSection({
  instanceId,
  currentTier,
}: {
  instanceId: string
  currentTier: PermTier
}) {
  const tablesQuery = useInstanceTables(instanceId, { limit: RELATION_LIMIT })
  const indexesQuery = useInstanceIndexes(instanceId, { limit: RELATION_LIMIT })
  const bloatTablesQuery = useInstanceBloat(instanceId, {
    limit: RELATION_LIMIT,
    object_kind: 'table',
  })
  const bloatIndexesQuery = useInstanceBloat(instanceId, {
    limit: RELATION_LIMIT,
    object_kind: 'index',
  })
  const permitted = currentTier !== 'T0'
  const bloatTableColumns = useMemo(
    () => tableBloatColumns(instanceId, permitted),
    [instanceId, permitted],
  )

  const [view = 'tables', showView] = useInstanceView(RELATION_VIEWS)
  const panels: Record<RelationViewId, RelationPanelProps> = {
    tables: {
      title: 'Tables',
      endpoint: 'table relations',
      query: tablesQuery,
      columns: TABLE_COLUMNS,
      emptyDescription: 'No table statistics arrived from the agent in the last 15 minutes.',
      captionId: 'relations.tables',
      noun: 'tables',
      configKey: 'checks.table_stats.top_n',
    },
    indexes: {
      title: 'Indexes',
      endpoint: 'index relations',
      query: indexesQuery,
      columns: INDEX_COLUMNS,
      emptyDescription:
        'No index usage statistics arrived from the agent in the last 15 minutes (index_stats check). Index bloat estimates cover a longer window and can list indexes anyway.',
      captionId: 'relations.indexes',
      noun: 'indexes',
      configKey: 'checks.index_stats.top_n',
    },
    'table-bloat': {
      title: 'Table bloat estimates',
      endpoint: 'table bloat estimates',
      query: bloatTablesQuery,
      columns: bloatTableColumns,
      emptyDescription: 'No table bloat estimate was reported in the last 13 hours.',
      captionId: 'relations.table-bloat',
      noun: 'tables',
      configKey: 'checks.bloat_estimate.top_n',
    },
    'index-bloat': {
      title: 'Index bloat estimates',
      endpoint: 'index bloat estimates',
      query: bloatIndexesQuery,
      columns: INDEX_BLOAT_COLUMNS,
      emptyDescription: 'No index bloat estimate was reported in the last 13 hours.',
      captionId: 'relations.index-bloat',
      noun: 'indexes',
      configKey: 'checks.bloat_estimate.top_n',
    },
  }

  return (
    <Tabs onValueChange={showView} value={view}>
      <InstancePanel
        title="Tables and indexes"
        captionId="relations.section"
        id="instance-relations"
        actions={<InstanceViewTabs label="Relation lists" views={RELATION_VIEWS} />}
      >
        <TabsContent value="tables">
          <RelationPanel {...panels.tables} />
        </TabsContent>
        <TabsContent value="indexes">
          <RelationPanel {...panels.indexes} />
        </TabsContent>
        <TabsContent value="table-bloat">
          <div className="space-y-3 min-[1600px]:space-y-1.5">
            {/* From 1600 px both notes share one line above the table (D26). */}
            <div className="space-y-3 min-[1600px]:flex min-[1600px]:items-center min-[1600px]:gap-4 min-[1600px]:space-y-0">
              <SkippedTablesNotice response={bloatTablesQuery.data} />
              <ExactBloatNotice currentTier={currentTier} />
            </div>
            <RelationPanel {...panels['table-bloat']} />
          </div>
        </TabsContent>
        <TabsContent value="index-bloat">
          <RelationPanel {...panels['index-bloat']} />
        </TabsContent>
      </InstancePanel>
    </Tabs>
  )
}
