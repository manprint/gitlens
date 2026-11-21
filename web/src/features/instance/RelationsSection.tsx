import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, type ReactNode } from 'react'

import type { ApiFailure } from '@/api/client'
import { useCreateCommand } from '@/api/commands'
import { qk } from '@/api/keys'
import { useInstanceBloat, useInstanceIndexes, useInstanceTables } from '@/api/queries'
import type { PermTier, Schemas } from '@/api/types'
import { HelpLink } from '@/components/layout/HelpLink'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState, NotPermitted, Unknown } from '@/components/state'
import {
  commandQueueFullMessage,
  describeCommandState,
  isTerminalCommandState,
} from '@/lib/commands'
import {
  formatBytes,
  formatCount,
  formatPercent,
  formatRate,
  formatTimestamp,
  TIMESTAMP_COLUMN_WIDTH,
} from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent } from '@/components/ui/tabs'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
import type { CaptionId, ColumnHelpId } from '@/lib/captions'
import { StandbyStatsNote } from '@/components/StandbyStatsNote'

import { AutovacuumActivity } from './AutovacuumRuns'
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
  labelKey: MessageKey
  render: (item: RelationItem, t: Translate) => ReactNode
  /** Absent for action columns, which have nothing to sort by. */
  sortValue?: (item: RelationItem) => string | number | null
  /** Column width from 1600 px (D26). */
  width?: string
  /** A free-text column: one line from 1600 px, the full text in its title. */
  truncate?: (item: RelationItem, t: Translate) => string
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

function relationCell(item: RelationItem, t: Translate): ReactNode {
  return relationName(item) ?? <Unknown reason={t('instance.relations.missing.relationName')} />
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

function booleanCell(item: RelationItem, key: string, t: Translate): ReactNode {
  const value = booleanValue(item, key)
  return value === null ? (
    <Unknown />
  ) : (
    t(value ? 'instance.relations.value.yes' : 'instance.relations.value.no')
  )
}

function rateCell(item: RelationItem, key: string): ReactNode {
  const value = numberValue(item, key)
  return value === null ? <Unknown /> : formatRate(value)
}

function timestampCell(item: RelationItem): ReactNode {
  const value = stringValue(item, 'ts')
  return value ? formatTimestamp(value) : <Unknown />
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

function maintenanceStamp(latest: { iso: string; auto: boolean }, t: Translate): string {
  return t('instance.relations.maintenance.stamp', {
    time: formatTimestamp(latest.iso),
    kind: t(
      latest.auto ? 'instance.relations.maintenance.auto' : 'instance.relations.maintenance.manual',
    ),
  })
}

function maintenanceColumn(
  key: string,
  labelKey: MessageKey,
  manualKey: string,
  autoKey: string,
  neverKey: MessageKey,
): RelationColumn {
  return {
    key,
    labelKey,
    render: (item, t) => {
      const latest = latestMaintenance(item, manualKey, autoKey)
      if (!latest) return <Unknown reason={t(neverKey)} />
      return maintenanceStamp(latest, t)
    },
    sortValue: (item) => latestMaintenance(item, manualKey, autoKey)?.ms ?? null,
    width: '7rem',
    truncate: (item, t) => {
      const latest = latestMaintenance(item, manualKey, autoKey)
      return latest ? maintenanceStamp(latest, t) : ''
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

// The four lists are already split by relation kind: no kind filter (tables inventory, §8).
function relationToolbar(t: Translate): TableToolbarSpec<RelationItem> {
  return {
    searchFields: [
      (item) => relationName(item),
      (item) => stringValue(item, 'indexrelname'),
      (item) => stringValue(item, 'datname'),
    ],
    filters: [
      {
        id: 'database',
        label: t('instance.relations.col.database'),
        kind: 'select',
        accessor: (item) => stringValue(item, 'datname'),
      },
      {
        id: 'schema',
        label: t('instance.relations.filter.schema'),
        kind: 'select',
        accessor: (item) => stringValue(item, 'schemaname'),
      },
    ],
  }
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
  noun,
  tableId,
  captionId,
  items,
  columns,
}: {
  caption: string
  noun: string
  tableId: string
  captionId: CaptionId
  items: RelationItem[]
  columns: RelationColumn[]
}) {
  const t = useT()
  const toolbar = useMemo(() => relationToolbar(t), [t])
  const simpleColumns = useMemo(
    () =>
      columns.map<SimpleColumn<RelationItem>>((column, index) => ({
        id: column.key,
        header: t(column.labelKey),
        ...(column.sortValue ? { sortValue: column.sortValue } : {}),
        rowHeader: index === 0,
        className: index === 0 ? 'font-normal' : '',
        cell: (item) => column.render(item, t),
        ...(COLUMN_HELP[column.key] === undefined ? {} : { help: COLUMN_HELP[column.key] }),
        ...(column.width === undefined ? {} : { width: column.width }),
        ...(column.truncate === undefined
          ? {}
          : { truncate: (item: RelationItem) => column.truncate?.(item, t) ?? '' }),
      })),
    [columns, t],
  )
  const firstKey = columns[0]?.key
  // The first column, descending, as before the URL held the order.
  return (
    <SimpleTable
      ariaLabel={caption}
      captionId={captionId}
      columns={simpleColumns}
      emptyState={null}
      headerToggleName={(column) =>
        t('instance.relations.sortBy', { list: caption, column: column.header })
      }
      rowKey={relationKey}
      rows={items}
      scope={noun}
      tableId={tableId}
      toolbar={toolbar}
      // Eleven sortable columns fit 1600 px only with the dense cells (D26).
      className={DENSE_TABLE}
      {...(firstKey === undefined ? {} : { defaultSort: { id: firstKey, desc: true } })}
    />
  )
}

const RELATION_COLUMN: RelationColumn = {
  key: 'relation',
  labelKey: 'instance.relations.col.relation',
  render: relationCell,
  sortValue: (item) => relationName(item),
  truncate: (item) => textOf(relationName(item)),
}

// The budget is shared across databases, so the same schema and table name
// can appear once per database; the database tells the rows apart.
const DATABASE_COLUMN: RelationColumn = {
  key: 'database',
  labelKey: 'instance.relations.col.database',
  render: (item) => textCell(item, 'datname'),
  sortValue: (item) => stringValue(item, 'datname'),
  width: '6.5rem',
  truncate: (item) => textOf(stringValue(item, 'datname')),
}

const OBSERVED_COLUMN: RelationColumn = {
  key: 'observed',
  labelKey: 'instance.relations.col.observed',
  render: timestampCell,
  sortValue: (item) => stringValue(item, 'ts'),
  width: TIMESTAMP_COLUMN_WIDTH,
  truncate: (item) => {
    const value = stringValue(item, 'ts')
    return value ? formatTimestamp(value) : ''
  },
}

function booleanColumn(
  key: string,
  labelKey: MessageKey,
  field: string,
  width: string,
): RelationColumn {
  return {
    key,
    labelKey,
    width,
    render: (item, t) => booleanCell(item, field, t),
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
    labelKey: 'instance.relations.col.liveTuples',
    render: (item) => countCell(item, 'n_live_tup'),
    sortValue: (item) => numberValue(item, 'n_live_tup'),
    width: '7rem',
  },
  {
    key: 'dead_tuples',
    labelKey: 'instance.relations.col.deadTuples',
    render: (item) => countCell(item, 'n_dead_tup'),
    sortValue: (item) => numberValue(item, 'n_dead_tup'),
    width: '7rem',
  },
  {
    key: 'dead_ratio',
    labelKey: 'instance.relations.col.deadRatio',
    render: (item) => percentCell(item, 'dead_ratio'),
    sortValue: (item) => numberValue(item, 'dead_ratio'),
    width: '7rem',
  },
  {
    key: 'size',
    labelKey: 'instance.relations.col.size',
    render: (item) => bytesCell(item, 'total_bytes'),
    sortValue: (item) => numberValue(item, 'total_bytes'),
    width: '5.75rem',
  },
  {
    key: 'seq_scan_rate',
    labelKey: 'instance.relations.col.seqScans',
    render: (item) => rateCell(item, 'seq_scan_rate'),
    sortValue: (item) => numberValue(item, 'seq_scan_rate'),
    width: '9.5rem',
  },
  {
    key: 'idx_scan_rate',
    labelKey: 'instance.relations.col.indexScans',
    render: (item) => rateCell(item, 'idx_scan_rate'),
    sortValue: (item) => numberValue(item, 'idx_scan_rate'),
    width: '7.75rem',
  },
  maintenanceColumn(
    'last_vacuum',
    'instance.relations.col.lastVacuum',
    'last_vacuum_at',
    'last_autovacuum_at',
    'instance.relations.maintenance.neverVacuumed',
  ),
  maintenanceColumn(
    'last_analyze',
    'instance.relations.col.lastAnalyze',
    'last_analyze_at',
    'last_autoanalyze_at',
    'instance.relations.maintenance.neverAnalysed',
  ),
  OBSERVED_COLUMN,
]

/**
 * The index's last scan since the statistics reset, from the API's last_scan_status.
 * "never since reset" is an index with no scan since the reset (PostgreSQL 16 and
 * later); "not available" means the server has no last scan for it (PostgreSQL 15).
 */
const LAST_SCAN_STATUS_TEXT: Record<string, { textKey: MessageKey; titleKey: MessageKey }> = {
  never_since_reset: {
    textKey: 'instance.relations.lastScan.neverSinceReset',
    titleKey: 'instance.relations.lastScan.neverSinceResetTitle',
  },
  not_available: {
    textKey: 'instance.relations.lastScan.notAvailable',
    titleKey: 'instance.relations.lastScan.notAvailableTitle',
  },
}

function lastScanCell(item: RelationItem, t: Translate): ReactNode {
  const at = timeValue(item, 'last_scan_at')
  if (at) {
    return (
      <span
        className="min-[1600px]:block min-[1600px]:truncate"
        title={t('instance.relations.lastScan.title', { time: formatTimestamp(at.iso) })}
      >
        {formatTimestamp(at.iso)}
      </span>
    )
  }
  const status = stringValue(item, 'last_scan_status')
  const known = status ? LAST_SCAN_STATUS_TEXT[status] : undefined
  return known ? <span title={t(known.titleKey)}>{t(known.textKey)}</span> : <Unknown />
}

const INDEX_COLUMNS: RelationColumn[] = [
  RELATION_COLUMN,
  DATABASE_COLUMN,
  {
    key: 'index',
    labelKey: 'instance.relations.col.index',
    render: (item) => textCell(item, 'indexrelname'),
    sortValue: (item) => stringValue(item, 'indexrelname'),
    truncate: (item) => textOf(stringValue(item, 'indexrelname')),
  },
  {
    key: 'idx_scan_rate',
    labelKey: 'instance.relations.col.indexScans',
    render: (item) => rateCell(item, 'idx_scan_rate'),
    sortValue: (item) => numberValue(item, 'idx_scan_rate'),
    width: '9.25rem',
  },
  {
    key: 'last_scan',
    labelKey: 'instance.relations.col.lastScan',
    render: lastScanCell,
    sortValue: (item) => timeValue(item, 'last_scan_at')?.ms ?? null,
    width: '10rem',
  },
  {
    key: 'size',
    labelKey: 'instance.relations.col.size',
    render: (item) => bytesCell(item, 'index_bytes'),
    sortValue: (item) => numberValue(item, 'index_bytes'),
    width: '5.75rem',
  },
  booleanColumn('unique', 'instance.relations.col.unique', 'is_unique', '7rem'),
  booleanColumn('primary', 'instance.relations.col.primary', 'is_primary', '7.5rem'),
  booleanColumn('valid', 'instance.relations.col.valid', 'is_valid', '6.25rem'),
  OBSERVED_COLUMN,
]

// A bloat row is a table or an index of a table. The relation column names the
// table in both cases; the index column names the index of an index row.
const BLOAT_TABLE_COLUMN: RelationColumn = {
  ...RELATION_COLUMN,
  labelKey: 'instance.relations.col.table',
}

const BLOAT_INDEX_COLUMN: RelationColumn = {
  key: 'index',
  labelKey: 'instance.relations.col.index',
  render: (item) => textCell(item, 'indexrelname'),
  sortValue: (item) => stringValue(item, 'indexrelname'),
  truncate: (item) => textOf(stringValue(item, 'indexrelname')),
}

const BLOAT_MEASURE_COLUMNS: RelationColumn[] = [
  {
    key: 'real_bytes',
    labelKey: 'instance.relations.col.realSize',
    render: (item) => bytesCell(item, 'real_bytes'),
    sortValue: (item) => numberValue(item, 'real_bytes'),
    width: '7.5rem',
  },
  {
    key: 'expected_bytes',
    labelKey: 'instance.relations.col.expectedSize',
    render: (item) => bytesCell(item, 'expected_bytes'),
    sortValue: (item) => numberValue(item, 'expected_bytes'),
    width: '10rem',
  },
  {
    key: 'bloat_bytes',
    labelKey: 'instance.relations.col.bloat',
    render: (item) => bytesCell(item, 'bloat_bytes'),
    sortValue: (item) => numberValue(item, 'bloat_bytes'),
    width: '6.5rem',
  },
  {
    key: 'bloat_ratio',
    labelKey: 'instance.relations.col.bloatRatio',
    render: (item) => percentCell(item, 'bloat_ratio'),
    sortValue: (item) => numberValue(item, 'bloat_ratio'),
    width: '9rem',
  },
  {
    key: 'method',
    labelKey: 'instance.relations.col.method',
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
  const t = useT()
  if (!response.truncated) return null
  return (
    <ToolbarNote status wrap>
      {t('instance.relations.budget', { noun, configKey })}
    </ToolbarNote>
  )
}

interface RelationPanelProps {
  titleKey: MessageKey
  tableId: string
  endpoint: string
  query: RelationQueryState
  columns: RelationColumn[]
  emptyTitleKey: MessageKey
  emptyDescriptionKey: MessageKey
  captionId: CaptionId
  nounKey: MessageKey
  configKey: string
}

function RelationPanel({
  titleKey,
  tableId,
  endpoint,
  query,
  columns,
  emptyTitleKey,
  emptyDescriptionKey,
  captionId,
  nounKey,
  configKey,
}: RelationPanelProps) {
  const t = useT()
  const title = t(titleKey)
  if (query.error && !query.data) {
    return (
      <ErrorState endpoint={endpoint} failure={query.error} onRetry={() => void query.refetch()} />
    )
  }
  if (!query.data) {
    return (
      <div role="status" aria-label={t('instance.relations.loadingAria', { list: title })}>
        {t('instance.relations.loading', { list: title.toLowerCase() })}
      </div>
    )
  }
  if (query.data.items.length === 0) {
    return (
      <CaptionedBlock ariaLabel={title} captionId={captionId}>
        <EmptyState title={t(emptyTitleKey)} description={t(emptyDescriptionKey)} />
      </CaptionedBlock>
    )
  }
  return (
    <div className="space-y-3">
      <BudgetNotice response={query.data} noun={t(nounKey)} configKey={configKey} />
      <SortableRelationTable
        caption={title}
        noun={t(nounKey)}
        tableId={tableId}
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
  const t = useT()
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
    return <Unknown reason={t('instance.relations.exact.tablesOnly')} />
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

        aria-label={t('instance.relations.exact.ariaLabel', { name })}
        disabled={!permitted || running}
        onClick={() =>
          command.mutate({
            kind: 'pgstattuple',
            args: { schema, relation, ...(datname ? { datname } : {}) },
          })
        }
      >
        {t('instance.relations.exact.button')}
      </Button>
      {command.command.data ? (
        <p role="status" className="text-xs">
          {describeCommandState(command.command.data.state, command.command.data.error)}
        </p>
      ) : null}
      {command.error ? (
        <p role="alert" className="text-destructive text-xs">
          {t('instance.relations.exact.submitFailed')}{' '}
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
      labelKey: 'instance.relations.col.exactMeasurement',
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
  const t = useT()
  if (!response || response.skipped_tables < 1) return null
  const named = response.skipped_relations
  const more = response.skipped_tables - named.length
  const skipped = t('instance.relations.skipped.summary', {
    count: response.skipped_tables,
    shown: formatCount(response.skipped_tables),
  })
  const names = named.map((r) => `${r.datname}: ${r.schemaname}.${r.relname}`).join(', ')
  const largest =
    named.length === 0
      ? null
      : more > 0
        ? t('instance.relations.skipped.largestMore', { names, more: formatCount(more) })
        : t('instance.relations.skipped.largest', { names })
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

function ExactBloatNotice({ currentTier }: { currentTier: PermTier }) {
  const t = useT()
  const note = `${t('instance.relations.exactNote.before')} pgstattuple ${t('instance.relations.exactNote.after')}`
  return (
    <div className="text-muted-foreground space-y-2 text-sm min-[1600px]:flex min-[1600px]:min-w-0 min-[1600px]:flex-1 min-[1600px]:items-center min-[1600px]:gap-3 min-[1600px]:space-y-0 min-[1600px]:text-xs">
      <p className="min-[1600px]:line-clamp-2 min-[1600px]:min-w-0" title={note}>
        {t('instance.relations.exactNote.before')} <code>pgstattuple</code>{' '}
        {t('instance.relations.exactNote.after')}
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
function relationViews(t: Translate): readonly InstanceView<RelationViewId>[] {
  return [
    { id: 'tables', label: t('instance.relations.list.tables') },
    { id: 'indexes', label: t('instance.relations.list.indexes') },
    { id: 'table-bloat', label: t('instance.relations.view.tableBloat') },
    { id: 'index-bloat', label: t('instance.relations.view.indexBloat') },
    { id: 'autovacuum', label: t('instance.relations.view.autovacuum') },
  ]
}

export function RelationsSection({
  instanceId,
  currentTier,
  role,
}: {
  instanceId: string
  currentTier: PermTier
  /** The instance role: a standby says its statistics are its own (V-10). */
  role?: string | null | undefined
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

  const t = useT()
  const views = useMemo(() => relationViews(t), [t])
  const [view = 'tables', showView] = useInstanceView(views)
  const panels: Record<Exclude<RelationViewId, 'autovacuum'>, RelationPanelProps> = {
    tables: {
      titleKey: 'instance.relations.list.tables',
      tableId: 'relations-tables',
      endpoint: 'table relations',
      query: tablesQuery,
      columns: TABLE_COLUMNS,
      emptyTitleKey: 'instance.relations.empty.tables',
      emptyDescriptionKey: 'instance.relations.emptyDescription.tables',
      captionId: 'relations.tables',
      nounKey: 'instance.relations.noun.tables',
      configKey: 'checks.table_stats.top_n',
    },
    indexes: {
      titleKey: 'instance.relations.list.indexes',
      tableId: 'relations-indexes',
      endpoint: 'index relations',
      query: indexesQuery,
      columns: INDEX_COLUMNS,
      emptyTitleKey: 'instance.relations.empty.indexes',
      emptyDescriptionKey: 'instance.relations.emptyDescription.indexes',
      captionId: 'relations.indexes',
      nounKey: 'instance.relations.noun.indexes',
      configKey: 'checks.index_stats.top_n',
    },
    'table-bloat': {
      titleKey: 'instance.relations.list.tableBloat',
      tableId: 'relations-table-bloat',
      endpoint: 'table bloat estimates',
      query: bloatTablesQuery,
      columns: bloatTableColumns,
      emptyTitleKey: 'instance.relations.empty.tableBloat',
      emptyDescriptionKey: 'instance.relations.emptyDescription.tableBloat',
      captionId: 'relations.table-bloat',
      nounKey: 'instance.relations.noun.tables',
      configKey: 'checks.bloat_estimate.top_n',
    },
    'index-bloat': {
      titleKey: 'instance.relations.list.indexBloat',
      tableId: 'relations-index-bloat',
      endpoint: 'index bloat estimates',
      query: bloatIndexesQuery,
      columns: INDEX_BLOAT_COLUMNS,
      emptyTitleKey: 'instance.relations.empty.indexBloat',
      emptyDescriptionKey: 'instance.relations.emptyDescription.indexBloat',
      captionId: 'relations.index-bloat',
      nounKey: 'instance.relations.noun.indexes',
      configKey: 'checks.bloat_estimate.top_n',
    },
  }

  return (
    <Tabs onValueChange={showView} value={view}>
      <InstancePanel
        title={t('instance.relations.section')}
        captionId="relations.section"
        id="instance-relations"
        actions={
          <>
            <InstanceViewTabs label={t('instance.relations.viewsLabel')} views={views} />
            <HelpLink
              anchor="instance-overview#relations"
              label={t('instance.relations.section')}
            />
          </>
        }
      >
        <StandbyStatsNote role={role} />
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
        <TabsContent value="autovacuum">
          <AutovacuumActivity instanceId={instanceId} />
        </TabsContent>
      </InstancePanel>
    </Tabs>
  )
}
