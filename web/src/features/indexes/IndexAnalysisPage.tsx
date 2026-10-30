import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { Database, ListTree, Server, TriangleAlert } from 'lucide-react'

import { useClusters, useInstanceIndexes } from '@/api/queries'
import { useFindings } from '@/api/queries'
import type { Cluster, Schemas } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { MetricTile } from '@/components/layout/MetricTile'
import { PageHeader } from '@/components/layout/PageHeader'
import { SqlText } from '@/components/layout/SqlText'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { NativeSelect } from '@/components/ui/native-select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { formatBytes, formatCount, formatPercent, formatRate, formatRelative } from '@/lib/format'
import {
  buildIndexRows,
  foreignKeyGaps,
  groupByTable,
  indexFindings,
  INDEX_RULES,
  matchesFilter,
  needsAttention,
  summariseIndexes,
  type IndexRow,
  type RowFilter,
} from '@/lib/indexAnalysis'
import { summariseColumns } from '@/lib/indexDefinition'
import { chooseInstance, readLastInstance } from '@/lib/lastInstance'

import { CleanupPanel, type SiblingServer } from './CleanupPanel'
import { DeadRowsPanel } from './DeadRowsPanel'

/** The most indexes the API returns for one server. */
export const INDEX_LIMIT = 500

type Finding = Schemas['Finding']

/** Kinds of an index for the Kind filter: a partial unique index is both unique and partial. */
const INDEX_KINDS: readonly {
  value: string
  label: string
  matches: (row: IndexRow) => boolean
}[] = [
  { value: 'primary', label: 'Primary', matches: (row) => row.primary },
  { value: 'unique', label: 'Unique', matches: (row) => row.unique && !row.primary },
  { value: 'partial', label: 'Partial', matches: (row) => row.parsed.predicate !== null },
  {
    value: 'plain',
    label: 'Plain',
    matches: (row) => !row.unique && !row.primary && row.parsed.predicate === null,
  },
]

const ADVISOR_FLAGS: readonly { value: RowFilter; label: string }[] = [
  { value: 'attention', label: 'Needs attention' },
  { value: 'unused', label: 'Unused candidates' },
  { value: 'duplicate', label: 'Duplicate / redundant' },
]

// The page's Database select scopes the rows already: no database filter (tables inventory).
const INDEX_TOOLBAR: TableToolbarSpec<IndexRow> = {
  searchLabel: 'Search indexes',
  searchFields: [
    (row) => row.name,
    (row) => row.table,
    (row) => row.schema,
    (row) => row.datname,
    (row) => row.parsed.method,
    (row) => row.parsed.columns.map((column) => column.text).join(', '),
    (row) => row.parsed.include.map((column) => column.text).join(', '),
    (row) => row.definition,
    // The advisor's reason: the title of each finding the index carries.
    (row) =>
      [row.unusedFinding, row.redundantFinding, row.invalidFinding, row.divergenceFinding]
        .map((finding) => finding?.title ?? '')
        .join(' '),
  ],
  filters: [
    {
      id: 'state',
      label: 'State',
      kind: 'select',
      options: [
        { value: 'valid', label: 'Valid' },
        { value: 'invalid', label: 'Invalid' },
      ],
      accessor: (row) => (matchesFilter(row, 'invalid') ? 'invalid' : 'valid'),
    },
    {
      id: 'kind',
      label: 'Kind',
      kind: 'select',
      options: INDEX_KINDS.map(({ value, label }) => ({ value, label })),
      accessor: (row) => INDEX_KINDS.find((kind) => kind.matches(row))?.value,
      predicate: (row, value) =>
        INDEX_KINDS.find((kind) => kind.value === value)?.matches(row) ?? false,
    },
    {
      id: 'advisor',
      label: 'Advisor flag',
      kind: 'select',
      options: ADVISOR_FLAGS,
      accessor: () => null,
      predicate: (row, value) => matchesFilter(row, value as RowFilter),
    },
  ],
}

type SortKey = 'name' | 'table' | 'bytes' | 'scanRate' | 'lastScan'

export const TABS = [
  { value: 'indexes', label: 'Indexes' },
  { value: 'cleanup', label: 'Cleanup' },
  { value: 'dead-rows', label: 'Dead rows' },
] as const

type TabValue = (typeof TABS)[number]['value']

const DEFAULT_TAB: TabValue = 'indexes'

function readTab(value: string | null): TabValue {
  return TABS.find((tab) => tab.value === value)?.value ?? DEFAULT_TAB
}

interface InstanceOption {
  id: string
  label: string
  up: boolean
}

function instanceOptions(clusters: readonly Cluster[]): InstanceOption[] {
  return clusters.flatMap((cluster) =>
    cluster.instances.map((instance) => ({
      id: instance.instance_id,
      label: `${instance.addr}:${instance.port}${cluster.name ? ` · ${cluster.name}` : ''}`,
      up: instance.up,
    })),
  )
}

function siblingServers(clusters: readonly Cluster[], instanceId: string): SiblingServer[] {
  const cluster = clusters.find((candidate) =>
    candidate.instances.some((instance) => instance.instance_id === instanceId),
  )
  return (cluster?.instances ?? [])
    .filter((instance) => instance.instance_id !== instanceId)
    .map((instance) => ({ id: instance.instance_id, label: `${instance.addr}:${instance.port}` }))
}

function sortValue(row: IndexRow, key: SortKey): string | number | null {
  switch (key) {
    case 'name':
      return row.name.toLowerCase()
    case 'table':
      return `${row.schema}.${row.table}`.toLowerCase()
    case 'bytes':
      return row.bytes
    case 'scanRate':
      return row.scanRate
    case 'lastScan':
      return row.lastScanAt === null ? null : Date.parse(row.lastScanAt)
  }
}

/** The inventory columns; size, scans and last scan read best largest or newest first. */
function indexColumns(showDatabase: boolean, now: Date): SimpleColumn<IndexRow>[] {
  return [
    {
      id: 'name',
      header: 'Index',
      sortValue: (row) => sortValue(row, 'name'),
      className: 'font-medium break-all',
      cell: (row) => row.name,
    },
    {
      id: 'table',
      header: 'Table',
      sortValue: (row) => sortValue(row, 'table'),
      className: 'break-all',
      cell: (row) => (
        <>
          <span className="text-muted-foreground">
            {showDatabase ? `${row.datname} · ` : ''}
            {row.schema}.
          </span>
          {row.table}
        </>
      ),
    },
    {
      id: 'columns',
      header: 'Columns',
      cell: (row) => (
        <code className="font-mono text-xs break-words">
          {row.parsed.columns.length === 0 ? '—' : summariseColumns(row.parsed)}
        </code>
      ),
    },
    { id: 'method', header: 'Method', cell: (row) => row.parsed.method ?? '—' },
    { id: 'flags', header: 'Flags', cell: (row) => <RowBadges row={row} /> },
    {
      id: 'bytes',
      header: 'Size',
      sortValue: (row) => sortValue(row, 'bytes'),
      firstDirection: 'desc',
      className: 'whitespace-nowrap',
      cell: (row) => (row.bytes === null ? 'Unknown' : formatBytes(row.bytes)),
    },
    {
      id: 'scanRate',
      header: 'Scans',
      sortValue: (row) => sortValue(row, 'scanRate'),
      firstDirection: 'desc',
      className: 'whitespace-nowrap',
      cell: (row) => (row.scanRate === null ? 'Unknown' : formatRate(row.scanRate)),
    },
    {
      id: 'cache',
      header: 'Cache hit',
      className: 'whitespace-nowrap',
      cell: (row) => (row.cacheHitRatio === null ? '—' : formatPercent(row.cacheHitRatio, 1)),
    },
    {
      id: 'lastScan',
      header: 'Last scan',
      sortValue: (row) => sortValue(row, 'lastScan'),
      firstDirection: 'desc',
      className: 'whitespace-nowrap',
      cell: (row) => lastScanText(row, now),
    },
  ]
}

function lastScanText(row: IndexRow, now: Date): string {
  switch (row.lastScanStatus) {
    case 'scanned':
      return row.lastScanAt === null ? 'Unknown' : formatRelative(row.lastScanAt, now)
    case 'never_since_reset':
      return 'Never since stats reset'
    case 'not_available':
      return 'Not available'
  }
}

function RowBadges({ row }: { row: IndexRow }) {
  return (
    <div className="flex flex-wrap gap-1">
      {row.primary ? <Badge variant="secondary">primary</Badge> : null}
      {row.unique && !row.primary ? <Badge variant="secondary">unique</Badge> : null}
      {row.parsed.predicate !== null ? <Badge variant="outline">partial</Badge> : null}
      {row.parsed.columns.some((column) => column.expression) ? (
        <Badge variant="outline">expression</Badge>
      ) : null}
      {row.parsed.include.length > 0 ? <Badge variant="outline">include</Badge> : null}
      {!row.valid || row.invalidFinding !== null ? (
        <Badge variant="destructive">invalid</Badge>
      ) : null}
      {row.unusedFinding !== null ? <Badge variant="warning">unused</Badge> : null}
      {row.duplicateOf.length > 0 ? <Badge variant="warning">duplicate</Badge> : null}
      {row.redundantFinding !== null ? <Badge variant="warning">redundant prefix</Badge> : null}
      {row.divergenceFinding !== null ? <Badge variant="warning">diverges</Badge> : null}
      {row.unusedFinding === null &&
      row.lastScanStatus === 'never_since_reset' &&
      !row.primary &&
      !row.unique ? (
        <Badge
          title="No scan since the statistics were reset. The advisor waits for 7 days of history before it calls an index unused."
          variant="outline"
        >
          never scanned
        </Badge>
      ) : null}
    </div>
  )
}

function Detail({ row }: { row: IndexRow }) {
  const findings = [
    row.invalidFinding,
    row.unusedFinding,
    row.redundantFinding,
    row.divergenceFinding,
  ].filter((finding): finding is Finding => finding !== null)
  return (
    <div className="space-y-4 px-4 py-4">
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-2">
          <h4 className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
            Columns
          </h4>
          {row.parsed.columns.length === 0 ? (
            <p className="text-muted-foreground text-sm">The definition could not be read.</p>
          ) : (
            <ol className="space-y-1 text-sm">
              {row.parsed.columns.map((column, index) => (
                <li key={`${index}-${column.text}`} className="flex flex-wrap items-baseline gap-2">
                  <span className="text-muted-foreground w-5 text-right tabular-nums">
                    {index + 1}
                  </span>
                  <code className="font-mono">{column.text}</code>
                  {column.expression ? <Badge variant="outline">expression</Badge> : null}
                  {column.modifiers ? (
                    <span className="text-muted-foreground text-xs">{column.modifiers}</span>
                  ) : null}
                </li>
              ))}
            </ol>
          )}
          {row.parsed.include.length > 0 ? (
            <p className="text-sm">
              <span className="text-muted-foreground">Included: </span>
              <code className="font-mono">
                {row.parsed.include.map((column) => column.text).join(', ')}
              </code>
            </p>
          ) : null}
        </div>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
          <dt className="text-muted-foreground">Access method</dt>
          <dd>{row.parsed.method ?? 'unknown'}</dd>
          <dt className="text-muted-foreground">Predicate</dt>
          <dd>
            {row.parsed.predicate === null ? (
              'none (covers every row)'
            ) : (
              <code className="font-mono">{row.parsed.predicate}</code>
            )}
          </dd>
          {row.parsed.options ? (
            <>
              <dt className="text-muted-foreground">Storage options</dt>
              <dd>
                <code className="font-mono">{row.parsed.options}</code>
              </dd>
            </>
          ) : null}
          <dt className="text-muted-foreground">Tuples read</dt>
          <dd>{row.tupleReadRate === null ? 'Unknown' : `${formatRate(row.tupleReadRate)}`}</dd>
          <dt className="text-muted-foreground">Tuples fetched</dt>
          <dd>{row.tupleFetchRate === null ? 'Unknown' : `${formatRate(row.tupleFetchRate)}`}</dd>
          {row.duplicateOf.length > 0 ? (
            <>
              <dt className="text-muted-foreground">Same definition as</dt>
              <dd>
                <code className="font-mono">{row.duplicateOf.join(', ')}</code>
              </dd>
            </>
          ) : null}
        </dl>
      </div>

      {row.definition ? (
        <div className="bg-muted/40 rounded-lg border">
          <div className="px-3 py-2 break-words whitespace-pre-wrap">
            <SqlText sql={row.definition} />
          </div>
          <div className="flex justify-end border-t px-1.5 py-0.5">
            <CopyButton subject="index definition" text={row.definition} />
          </div>
        </div>
      ) : null}

      {findings.length > 0 ? (
        <ul className="space-y-2">
          {findings.map((finding) => (
            <li
              key={finding.finding_id}
              className="border-warning/40 bg-warning/10 rounded-lg border px-3 py-2 text-sm"
            >
              <strong>{finding.title}.</strong> {finding.detail}
              {finding.remediation ? (
                <p className="text-muted-foreground mt-1">{finding.remediation}</p>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

function Breakdown({
  title,
  entries,
}: {
  title: string
  entries: readonly { label: string; count: number; bytes: number }[]
}) {
  return (
    <section aria-label={title} className="bg-card shadow-card rounded-xl border p-4">
      <h3 className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
        {title}
      </h3>
      <ul className="mt-3 space-y-1.5 text-sm">
        {entries.map((entry) => (
          <li key={entry.label} className="flex items-baseline justify-between gap-3">
            <span className="truncate font-medium">{entry.label}</span>
            <span className="text-muted-foreground shrink-0 tabular-nums">
              {formatCount(entry.count)} · {formatBytes(entry.bytes)}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}

interface AnalysisProps {
  controls: React.ReactNode
  instanceId: string
  /** The other servers of the same cluster. */
  siblings: readonly SiblingServer[]
}

function InstanceIndexes({ controls, instanceId, siblings }: AnalysisProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const database = searchParams.get('db') ?? ''
  const tab = readTab(searchParams.get('tab'))

  const indexesQuery = useInstanceIndexes(instanceId, { limit: INDEX_LIMIT })
  const findingsQuery = useFindings({ instance_id: instanceId, limit: 1000 })
  const response = indexesQuery.data
  const findings = useMemo(() => findingsQuery.data ?? [], [findingsQuery.data])

  const allRows = useMemo(
    () => buildIndexRows(response?.items ?? [], findings),
    [response?.items, findings],
  )
  const databases = useMemo(() => [...new Set(allRows.map((row) => row.datname))].sort(), [allRows])
  const scoped = useMemo(
    () => (database === '' ? allRows : allRows.filter((row) => row.datname === database)),
    [allRows, database],
  )
  const summary = useMemo(() => summariseIndexes(scoped), [scoped])
  const tables = useMemo(() => groupByTable(scoped).slice(0, 8), [scoped])
  const actionable = useMemo(
    () => indexFindings(findings).filter((finding) => finding.state === 'open'),
    [findings],
  )
  const scopedFindings = useMemo(
    () => actionable.filter((finding) => database === '' || (finding.datname ?? '') === database),
    [actionable, database],
  )
  const gaps = useMemo(
    () => foreignKeyGaps(findings).filter((gap) => database === '' || gap.datname === database),
    [findings, database],
  )
  const counts = useMemo(() => {
    const count = (rule: string) =>
      scopedFindings.filter((finding) => finding.rule_id === rule).length
    return {
      unused: count(INDEX_RULES.unused),
      duplicate: count(INDEX_RULES.duplicate),
      redundant: count(INDEX_RULES.redundantPrefix),
      invalid: count(INDEX_RULES.invalid),
      divergence: count(INDEX_RULES.divergence),
    }
  }, [scopedFindings])
  const attention = scoped.filter(needsAttention).length
  // index.unused waits for 7 days of usage history. Until then the advisor reports the
  // check as degraded, and the page says so instead of showing a clean zero.
  const unusedPending = useMemo(
    () =>
      indexFindings(findings).filter(
        (finding) =>
          finding.rule_id === INDEX_RULES.unused &&
          finding.state === 'degraded' &&
          (database === '' || (finding.datname ?? '') === database),
      ).length,
    [findings, database],
  )
  // Relative times are measured from when the data was fetched, so a render stays pure.
  const showDatabase = summary.byDatabase.length > 1 && database === ''
  const dataUpdatedAt = indexesQuery.dataUpdatedAt
  const columns = useMemo(
    () => indexColumns(showDatabase, new Date(dataUpdatedAt)),
    [showDatabase, dataUpdatedAt],
  )

  function setParam(key: 'db', value: string) {
    const next = new URLSearchParams(searchParams)
    if (value && value !== 'all') next.set(key, value)
    else next.delete(key)
    // Another database is a new row set: every table starts on its first page (D23).
    for (const table of ['indexes', 'cleanup', 'dead-rows']) next.delete(`${table}.page`)
    setSearchParams(next, { replace: true })
  }

  function setTab(value: string) {
    const next = new URLSearchParams(searchParams)
    if (value === DEFAULT_TAB) next.delete('tab')
    else next.set('tab', value)
    setSearchParams(next, { replace: true })
  }

  return (
    <div className="space-y-6">
      <div className="bg-card shadow-card flex flex-wrap items-end gap-x-6 gap-y-4 rounded-xl border p-4">
        {controls}
        <div className="space-y-1.5">
          <label className="text-muted-foreground text-xs font-medium" htmlFor="index-db">
            Database
          </label>
          <div className="relative">
            <Database
              aria-hidden="true"
              className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
            />
            <NativeSelect
              className="w-56 max-w-full pr-3 pl-8"
              id="index-db"
              onChange={(event) => setParam('db', event.target.value)}
              value={database}
            >
              <option value="">All databases</option>
              {databases.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
              {database !== '' && !databases.includes(database) ? (
                <option value={database}>{database}</option>
              ) : null}
            </NativeSelect>
          </div>
        </div>
        <div className="ml-auto self-center">
          <FreshnessBadge dataUpdatedAt={indexesQuery.dataUpdatedAt} policy="instance" />
        </div>
      </div>

      <Tabs onValueChange={setTab} value={tab}>
        <TabsList aria-label="Index management sections" variant="line">
          {TABS.map((option) => (
            <TabsTrigger key={option.value} value={option.value}>
              {option.label}
            </TabsTrigger>
          ))}
        </TabsList>
        <TabsContent className="space-y-6" value="indexes">
          {indexesQuery.isPending && response === undefined ? (
            <div aria-busy="true" aria-label="Loading indexes" role="status">
              Loading indexes…
            </div>
          ) : indexesQuery.error && response === undefined ? (
            <ErrorState
              endpoint="indexes"
              failure={indexesQuery.error}
              onRetry={() => void indexesQuery.refetch()}
            />
          ) : (
            <>
              {response?.truncated || allRows.length >= INDEX_LIMIT ? (
                <div
                  className="border-warning/40 bg-warning/10 flex items-start gap-2 rounded-lg border px-4 py-3 text-sm"
                  role="status"
                >
                  <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
                  <p>
                    <strong>The list is incomplete.</strong> The agent reports only its busiest and
                    largest indexes per database (<code>checks.index_stats.top_n</code>, 50 by
                    default)
                    {allRows.length >= INDEX_LIMIT
                      ? `, and this page shows at most ${INDEX_LIMIT}`
                      : ''}
                    . The counts below cover the {formatCount(allRows.length)} indexes reported, not
                    every index of the server. Raise <code>top_n</code> in the agent configuration
                    to see more.
                  </p>
                </div>
              ) : null}

              {unusedPending > 0 ? (
                <div className="bg-muted/40 rounded-lg border px-4 py-3 text-sm" role="status">
                  <strong>
                    Unused-index check pending for {formatCount(unusedPending)}{' '}
                    {unusedPending === 1 ? 'index' : 'indexes'}.
                  </strong>{' '}
                  The advisor needs 7 days of usage history before it calls an index unused. Until
                  then, &quot;never scanned&quot; below is the raw statistic: no scan since the
                  statistics were last reset.
                </div>
              ) : null}

              <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                <MetricTile label="Indexes" value={formatCount(summary.total)} unit="reported" />
                <MetricTile label="Total size" value={formatBytes(summary.bytes)} />
                <MetricTile
                  label="Need attention"
                  value={formatCount(attention)}
                  unit={`of ${formatCount(summary.total)}`}
                />
                <MetricTile
                  label="Never scanned"
                  value={formatCount(summary.neverScanned)}
                  unit={
                    summary.neverScanned > 0 ? formatBytes(summary.neverScannedBytes) : undefined
                  }
                />
                <MetricTile label="Primary keys" value={formatCount(summary.primary)} />
                <MetricTile label="Unique" value={formatCount(summary.unique)} />
                <MetricTile label="Partial" value={formatCount(summary.partial)} />
                <MetricTile label="Expression" value={formatCount(summary.expression)} />
                <MetricTile label="Composite" value={formatCount(summary.composite)} />
                <MetricTile label="Invalid" value={formatCount(summary.invalid)} />
                <MetricTile
                  label="Duplicate groups"
                  value={formatCount(counts.duplicate)}
                  unit={`${formatCount(counts.redundant)} redundant prefix`}
                />
                <MetricTile
                  label="Foreign keys w/o index"
                  value={formatCount(gaps.length)}
                  unit={findingsQuery.isPending ? 'loading…' : undefined}
                />
              </div>

              {scopedFindings.length === 0 && gaps.length === 0 ? null : (
                <section
                  aria-labelledby="index-attention-title"
                  className="bg-card shadow-card overflow-hidden rounded-xl border"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2 border-b px-5 py-4">
                    <div>
                      <h2 className="text-base font-semibold" id="index-attention-title">
                        Advisor findings on indexes
                      </h2>
                      <p className="text-muted-foreground mt-0.5 text-sm">
                        {formatCount(counts.unused)} unused · {formatCount(counts.duplicate)}{' '}
                        duplicate groups · {formatCount(counts.redundant)} redundant prefixes ·{' '}
                        {formatCount(counts.invalid)} invalid · {formatCount(counts.divergence)}{' '}
                        diverging · {formatCount(gaps.length)} foreign keys without an index
                      </p>
                    </div>
                    <Link
                      className="text-primary text-sm font-medium hover:underline"
                      to="/findings"
                    >
                      Open Findings
                    </Link>
                  </div>
                  <ul className="divide-y">
                    {[...scopedFindings]
                      .filter((finding) => finding.rule_id !== INDEX_RULES.bloat)
                      .sort((a, b) => a.rule_id.localeCompare(b.rule_id))
                      .map((finding) => (
                        <li key={finding.finding_id} className="space-y-1 px-5 py-3 text-sm">
                          <div className="flex flex-wrap items-center gap-2">
                            <Badge
                              variant={finding.severity === 'critical' ? 'destructive' : 'warning'}
                            >
                              {finding.rule_id}
                            </Badge>
                            {finding.datname ? (
                              <Badge variant="outline">
                                <Database aria-hidden="true" />
                                {finding.datname}
                              </Badge>
                            ) : null}
                            <span className="font-medium">{finding.title}</span>
                          </div>
                          {finding.detail ? (
                            <p className="text-muted-foreground">{finding.detail}</p>
                          ) : null}
                          {finding.remediation ? <p>{finding.remediation}</p> : null}
                        </li>
                      ))}
                  </ul>
                </section>
              )}

              {summary.total > 0 ? (
                <div className="grid gap-4 lg:grid-cols-3">
                  <Breakdown title="By access method" entries={summary.byMethod} />
                  <Breakdown title="By database" entries={summary.byDatabase} />
                  <Breakdown
                    title="Tables with the most indexes"
                    entries={tables.map((group) => ({
                      label: `${group.schema}.${group.table}`,
                      count: group.indexes.length,
                      bytes: group.bytes,
                    }))}
                  />
                </div>
              ) : null}

              <section
                aria-labelledby="index-inventory-title"
                className="bg-card shadow-card overflow-hidden rounded-xl border"
              >
                <div className="space-y-3 border-b px-5 py-4">
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                      <h2 className="text-base font-semibold" id="index-inventory-title">
                        Index inventory
                      </h2>
                      <p className="text-muted-foreground mt-0.5 text-sm">
                        {formatCount(scoped.length)} indexes. Scan figures are per-second rates over
                        the last collection interval.
                      </p>
                    </div>
                  </div>
                </div>

                <div className="p-5">
                  {scoped.length === 0 ? (
                    <EmptyState
                      description="The agent has not reported index statistics for this server yet."
                      title="No indexes to show"
                    />
                  ) : (
                    <SimpleTable
                      ariaLabel="Indexes"
                      columns={columns}
                      defaultSort={{ id: 'bytes', desc: true }}
                      detailLabel={(row, expanded) =>
                        `${expanded ? 'Hide' : 'Show'} details of ${row.name}`
                      }
                      emptyState={null}
                      renderDetail={(row) => <Detail row={row} />}
                      rowKey={(row) => row.key}
                      rows={scoped}
                      scope="indexes"
                      tableId="indexes"
                      toolbar={INDEX_TOOLBAR}
                    />
                  )}
                </div>
              </section>

              {gaps.length > 0 ? (
                <section
                  aria-labelledby="index-fk-title"
                  className="bg-card shadow-card overflow-hidden rounded-xl border"
                >
                  <div className="border-b px-5 py-4">
                    <h2 className="text-base font-semibold" id="index-fk-title">
                      Foreign keys without a covering index
                    </h2>
                    <p className="text-muted-foreground mt-0.5 text-sm">
                      Deleting or re-keying a referenced row scans the referencing table. These are
                      indexes that are missing, so they do not appear in the inventory.
                    </p>
                  </div>
                  <ul className="divide-y">
                    {gaps.map((gap) => (
                      <li key={gap.finding.finding_id} className="space-y-1 px-5 py-3 text-sm">
                        <div className="flex flex-wrap items-center gap-2">
                          <Badge variant={gap.finding.severity === 'info' ? 'outline' : 'warning'}>
                            {gap.finding.severity}
                          </Badge>
                          {gap.datname ? <Badge variant="outline">{gap.datname}</Badge> : null}
                          <code className="font-mono">{gap.table}</code>
                          <span className="text-muted-foreground">
                            ({gap.columns}) → {gap.referenced}
                          </span>
                        </div>
                        {gap.finding.remediation ? (
                          <p className="text-muted-foreground">{gap.finding.remediation}</p>
                        ) : null}
                      </li>
                    ))}
                  </ul>
                </section>
              ) : null}
            </>
          )}
        </TabsContent>
        <TabsContent value="cleanup">
          {indexesQuery.isPending && response === undefined ? (
            <div aria-busy="true" aria-label="Loading indexes" role="status">
              Loading indexes…
            </div>
          ) : indexesQuery.error && response === undefined ? (
            <ErrorState
              endpoint="indexes"
              failure={indexesQuery.error}
              onRetry={() => void indexesQuery.refetch()}
            />
          ) : (
            <CleanupPanel instanceId={instanceId} rows={scoped} siblings={siblings} />
          )}
        </TabsContent>
        <TabsContent value="dead-rows">
          <DeadRowsPanel database={database} findings={findings} instanceId={instanceId} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

export function IndexAnalysisPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const clustersQuery = useClusters()
  const options = useMemo(() => instanceOptions(clustersQuery.data ?? []), [clustersQuery.data])
  const instanceId = chooseInstance(
    clustersQuery.data ?? [],
    searchParams.get('instance'),
    readLastInstance(),
  )

  function selectInstance(value: string) {
    const next = new URLSearchParams(searchParams)
    next.set('instance', value)
    // A database belongs to one instance, so switching server clears it.
    next.delete('db')
    setSearchParams(next, { replace: true })
  }

  return (
    <div className="space-y-6">
      <PageHeader
        eyebrow="Index management"
        icon={ListTree}
        subtitle={
          <>
            Every index the agent reports for a server: how many there are, which columns they
            cover, how big they are and how they are used, with the advisor&apos;s findings on
            unused, duplicate, redundant and invalid indexes and on foreign keys that lack one.
          </>
        }
        title="Index analysis"
      />

      {clustersQuery.error && clustersQuery.data === undefined ? (
        <ErrorState
          endpoint="clusters"
          failure={clustersQuery.error}
          onRetry={() => void clustersQuery.refetch()}
        />
      ) : clustersQuery.isPending ? (
        <div aria-busy="true" aria-label="Loading servers" role="status">
          Loading servers…
        </div>
      ) : instanceId === null ? (
        <EmptyState
          description="Set up the pglens agent on a PostgreSQL host to start collecting index statistics."
          title="No monitored servers yet"
        />
      ) : (
        <InstanceIndexes
          key={instanceId}
          controls={
            <div className="space-y-1.5">
              <label className="text-muted-foreground text-xs font-medium" htmlFor="index-instance">
                Server
              </label>
              <div className="relative">
                <Server
                  aria-hidden="true"
                  className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
                />
                <NativeSelect
                  className="w-72 max-w-full pr-3 pl-8"
                  id="index-instance"
                  onChange={(event) => selectInstance(event.target.value)}
                  value={instanceId}
                >
                  {options.map((option) => (
                    <option key={option.id} value={option.id}>
                      {option.label}
                      {option.up ? '' : ' (down)'}
                    </option>
                  ))}
                </NativeSelect>
              </div>
            </div>
          }
          instanceId={instanceId}
          siblings={siblingServers(clustersQuery.data ?? [], instanceId)}
        />
      )}
    </div>
  )
}

export default IndexAnalysisPage
