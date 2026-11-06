import { useEffect, useMemo, type ReactNode } from 'react'
import { ArrowRight } from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'

import { useAdvisorWindows } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import { InfoHint } from '@/components/layout/InfoHint'
import { MetricTile } from '@/components/layout/MetricTile'
import { SqlText } from '@/components/layout/SqlText'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useFocusMissing } from '@/hooks/useSearch'
import type { CaptionId } from '@/lib/captions'
import {
  formatBytes,
  formatCount,
  formatCountOf,
  formatPercent,
  formatRate,
  formatRelative,
} from '@/lib/format'
import {
  foreignKeyGaps,
  groupByTable,
  indexFindings,
  INDEX_RULES,
  matchesFilter,
  needsAttention,
  summariseIndexes,
  unusedCheckStatus,
  type ForeignKeyGap,
  type IndexRow,
  type RowFilter,
} from '@/lib/indexAnalysis'
import { UNUSED_INDEX_MIN_BYTES, windowPhrase } from '@/lib/indexCleanup'
import { summariseColumns } from '@/lib/indexDefinition'

import { IndexDetail } from './IndexDetail'
import { NO_INDEX_STATS } from './emptyText'
import { CELL_BADGE, IndexPanel, PanelNote, type PanelStat } from './IndexPanel'
import {
  DEFAULT_INDEX_VIEW,
  INDEX_VIEW_PARAM,
  INDEX_VIEWS,
  readIndexView,
  type IndexView,
} from './indexViews'

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

const FINDINGS_TOOLBAR: TableToolbarSpec<Finding> = {
  searchLabel: 'Search index findings',
  searchFields: [
    (finding) => finding.rule_id,
    (finding) => finding.title,
    (finding) => finding.object_name,
    (finding) => finding.datname,
    (finding) => finding.detail,
  ],
  filters: [{ id: 'rule', label: 'Rule', kind: 'select', accessor: (finding) => finding.rule_id }],
}

const GAPS_TOOLBAR: TableToolbarSpec<ForeignKeyGap> = {
  searchLabel: 'Search foreign keys',
  searchFields: [
    (gap) => gap.table,
    (gap) => gap.constraint,
    (gap) => gap.columns,
    (gap) => gap.referenced,
    (gap) => gap.datname,
  ],
  filters: [
    { id: 'severity', label: 'Severity', kind: 'select', accessor: (gap) => gap.finding.severity },
  ],
}

type SortKey = 'name' | 'table' | 'bytes' | 'scanRate' | 'lastScan'

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

/**
 * The inventory columns; size, scans and last scan read best largest or newest
 * first. From 1600 px every row is one line: widths, and free text truncated (D26).
 */
function indexColumns(
  showDatabase: boolean,
  now: Date,
  unusedWindow: string,
): SimpleColumn<IndexRow>[] {
  return [
    {
      id: 'name',
      header: 'Index',
      sortValue: (row) => sortValue(row, 'name'),
      className: 'font-medium break-all',
      width: '16rem',
      truncate: (row) => row.name,
      cell: (row) => row.name,
    },
    {
      id: 'table',
      header: 'Table',
      sortValue: (row) => sortValue(row, 'table'),
      className: 'break-all',
      width: '16rem',
      truncate: (row) => `${showDatabase ? `${row.datname} · ` : ''}${row.schema}.${row.table}`,
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
      truncate: (row) => (row.parsed.columns.length === 0 ? '—' : summariseColumns(row.parsed)),
      cell: (row) => (
        <code className="font-mono text-xs break-words">
          {row.parsed.columns.length === 0 ? '—' : summariseColumns(row.parsed)}
        </code>
      ),
    },
    { id: 'method', header: 'Method', width: '5rem', cell: (row) => row.parsed.method ?? '—' },
    {
      id: 'flags',
      header: 'Flags',
      help: 'indexes.flags-column',
      width: '15rem',
      cell: (row) => <RowBadges row={row} unusedWindow={unusedWindow} />,
    },
    {
      id: 'bytes',
      header: 'Size',
      sortValue: (row) => sortValue(row, 'bytes'),
      firstDirection: 'desc',
      className: 'whitespace-nowrap',
      width: '6rem',
      cell: (row) => (row.bytes === null ? 'Unknown' : formatBytes(row.bytes)),
    },
    {
      id: 'scanRate',
      header: 'Scans',
      help: 'indexes.scans',
      sortValue: (row) => sortValue(row, 'scanRate'),
      firstDirection: 'desc',
      className: 'whitespace-nowrap',
      width: '9rem',
      cell: (row) => scansText(row),
    },
    {
      id: 'cache',
      header: 'Cache hit',
      help: 'indexes.cache-hit',
      className: 'whitespace-nowrap',
      width: '6.5rem',
      cell: (row) => (row.cacheHitRatio === null ? '—' : formatPercent(row.cacheHitRatio, 1)),
    },
    {
      id: 'lastScan',
      header: 'Last scan',
      help: 'indexes.last-scan',
      sortValue: (row) => sortValue(row, 'lastScan'),
      firstDirection: 'desc',
      className: 'whitespace-nowrap',
      width: '12rem',
      truncate: (row) => lastScanText(row, now),
      cell: (row) => lastScanText(row, now),
    },
  ]
}

/**
 * The scan rate. Without a rate the statistics can still say the index was never
 * scanned since the reset (the Flags column says "never scanned"), so say that
 * here instead of "Unknown".
 */
function scansText(row: IndexRow): string {
  if (row.scanRate !== null) return formatRate(row.scanRate)
  return row.lastScanStatus === 'never_since_reset' ? 'None since reset' : 'Unknown'
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

/**
 * How the advisor reaches an unused verdict (D22b, D22c): the configured
 * window without a scan in the pglens history or, on PostgreSQL 16 and later,
 * from the statistics alone (no scan since a reset at least that old, or a
 * last scan older than the window), without waiting for pglens history.
 */
function unusedRuleText(unusedWindow: string): string {
  return `The advisor calls an index unused after ${unusedWindow} without a scan: from the pglens history or, on PostgreSQL 16 and later, from the statistics when there is no scan since a reset that long ago or the last scan is older.`
}

/**
 * The "never scanned" hint. The advisor never judges an index of
 * UNUSED_INDEX_MIN_BYTES or less (nor a primary key or a unique index, which
 * get no such flag), so for a small one the hint promises no verdict.
 */
function neverScannedHint(row: IndexRow, unusedWindow: string): string {
  const counter = 'No scan since the statistics were reset.'
  if (row.bytes !== null && row.bytes <= UNUSED_INDEX_MIN_BYTES) {
    return `${counter} The advisor never judges indexes of ${formatBytes(UNUSED_INDEX_MIN_BYTES)} or less, so this is the raw counter, not a verdict.`
  }
  return `${counter} ${unusedRuleText(unusedWindow)}`
}

/**
 * What an unused-index check that waiting does not settle means, from the
 * advisor's reason (`internal/advisor/rules_relation.go`); a reason this page
 * does not know is shown as the advisor wrote it.
 */
function blockedReasonText(reason: string): string {
  if (reason.includes('longer than the kept index metrics history')) {
    return `The ${reason.replace(/^the /, '')}: the pglens history never covers the window, so waiting does not settle the check, though on PostgreSQL 16 and later the statistics still can. Shorten the window or keep index metrics longer.`
  }
  if (reason.startsWith('standby scans unknown')) {
    return 'Standby scans unknown: no standby of the cluster has sent a snapshot, so a scan there cannot be ruled out. The check concludes once the standbys report.'
  }
  if (reason === '') return 'The advisor gave no reason.'
  return `${reason.charAt(0).toUpperCase()}${reason.slice(1)}.`
}

/**
 * The flags of an index, by name, for the badge strip's `title`;
 * `unusedWindow` names the unused-index window in the "never scanned" hint.
 */
function rowFlags(
  row: IndexRow,
  unusedWindow: string,
): { label: string; variant: BadgeVariant; title?: string }[] {
  const flags: { label: string; variant: BadgeVariant; title?: string }[] = []
  if (row.primary) flags.push({ label: 'primary', variant: 'secondary' })
  if (row.unique && !row.primary) flags.push({ label: 'unique', variant: 'secondary' })
  if (row.parsed.predicate !== null) flags.push({ label: 'partial', variant: 'outline' })
  if (row.parsed.columns.some((column) => column.expression)) {
    flags.push({ label: 'expression', variant: 'outline' })
  }
  if (row.parsed.include.length > 0) flags.push({ label: 'include', variant: 'outline' })
  if (!row.valid || row.invalidFinding !== null) {
    flags.push({ label: 'invalid', variant: 'destructive' })
  }
  if (row.unusedFinding !== null) flags.push({ label: 'unused', variant: 'warning' })
  if (row.duplicateOf.length > 0) flags.push({ label: 'duplicate', variant: 'warning' })
  if (row.redundantFinding !== null) flags.push({ label: 'redundant prefix', variant: 'warning' })
  if (row.divergenceFinding !== null) flags.push({ label: 'diverges', variant: 'warning' })
  if (
    row.unusedFinding === null &&
    row.lastScanStatus === 'never_since_reset' &&
    !row.primary &&
    !row.unique
  ) {
    flags.push({
      label: 'never scanned',
      variant: 'outline',
      title: neverScannedHint(row, unusedWindow),
    })
  }
  return flags
}

type BadgeVariant = 'secondary' | 'outline' | 'destructive' | 'warning'

/** The badges wrap below 1600 px; from 1600 px they stay on one line, the list in the `title`. */
function RowBadges({ row, unusedWindow }: { row: IndexRow; unusedWindow: string }) {
  const flags = rowFlags(row, unusedWindow)
  return (
    <div
      // From 1600 px one line high: a badge that does not fit wraps out of sight whole,
      // never cut in half; the title lists every flag.
      className="flex flex-wrap gap-1 min-[1600px]:max-h-5 min-[1600px]:overflow-hidden"
      title={flags.length === 0 ? undefined : flags.map((flag) => flag.label).join(', ')}
    >
      {flags.map((flag) => (
        <Badge
          key={flag.label}
          className={CELL_BADGE}
          variant={flag.variant}
          {...(flag.title === undefined ? {} : { title: flag.title })}
        >
          {flag.label}
        </Badge>
      ))}
    </div>
  )
}

function Detail({ row, instanceId }: { row: IndexRow; instanceId: string }) {
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

      <IndexDetail instanceId={instanceId} row={row} />

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
  captionId,
  entries,
}: {
  title: string
  captionId: CaptionId
  entries: readonly { label: string; count: number; bytes: number }[]
}) {
  return (
    <section
      aria-label={title}
      className="bg-card shadow-card rounded-xl border p-4"
      data-caption-id={captionId}
      data-card=""
    >
      <h3 className="text-muted-foreground flex items-center gap-1 text-xs font-semibold tracking-wide uppercase">
        {title}
        <InfoHint id={captionId} label={title} />
      </h3>
      <ul className="mt-3 space-y-1.5 text-sm">
        {entries.map((entry) => (
          <li key={entry.label} className="flex items-baseline justify-between gap-3">
            <span className="truncate font-medium">{entry.label}</span>
            <span className="text-muted-foreground shrink-0 tabular-nums">
              {formatCount(entry.count)} {entry.count === 1 ? 'index' : 'indexes'} ·{' '}
              {formatBytes(entry.bytes)}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}

const FINDING_COLUMNS: readonly SimpleColumn<Finding>[] = [
  {
    id: 'rule',
    header: 'Rule',
    sortValue: (finding) => finding.rule_id,
    width: '13rem',
    cell: (finding) => (
      <Badge
        className={CELL_BADGE}
        variant={finding.severity === 'critical' ? 'destructive' : 'warning'}
      >
        {finding.rule_id}
      </Badge>
    ),
  },
  {
    id: 'database',
    header: 'Database',
    sortValue: (finding) => finding.datname ?? null,
    width: '9rem',
    truncate: (finding) => finding.datname ?? '—',
    cell: (finding) => finding.datname ?? '—',
  },
  {
    id: 'object',
    header: 'Object',
    sortValue: (finding) => finding.object_name ?? null,
    width: '18rem',
    truncate: (finding) => finding.object_name ?? '—',
    cell: (finding) => <code className="font-mono text-xs">{finding.object_name ?? '—'}</code>,
  },
  {
    id: 'title',
    header: 'Finding',
    className: 'font-medium',
    truncate: (finding) => finding.title,
    cell: (finding) => finding.title,
  },
]

function FindingDetail({ finding }: { finding: Finding }) {
  return (
    <div className="space-y-1 px-4 py-2 text-sm">
      {finding.detail ? <p className="text-muted-foreground">{finding.detail}</p> : null}
      {finding.remediation ? <p>{finding.remediation}</p> : null}
      {!finding.detail && !finding.remediation ? (
        <p className="text-muted-foreground">The advisor gave no further detail.</p>
      ) : null}
    </div>
  )
}

const GAP_COLUMNS: readonly SimpleColumn<ForeignKeyGap>[] = [
  {
    id: 'severity',
    header: 'Severity',
    help: 'indexes.fk-severity-column',
    sortValue: (gap) => gap.finding.severity,
    width: '7rem',
    cell: (gap) => (
      <Badge
        className={CELL_BADGE}
        variant={gap.finding.severity === 'info' ? 'outline' : 'warning'}
      >
        {gap.finding.severity}
      </Badge>
    ),
  },
  {
    id: 'database',
    header: 'Database',
    sortValue: (gap) => gap.datname,
    width: '9rem',
    truncate: (gap) => gap.datname || '—',
    cell: (gap) => gap.datname || '—',
  },
  {
    id: 'table',
    header: 'Table',
    sortValue: (gap) => gap.table,
    width: '16rem',
    truncate: (gap) => gap.table,
    cell: (gap) => <code className="font-mono">{gap.table}</code>,
  },
  {
    id: 'columns',
    header: 'Columns',
    help: 'indexes.fk-columns',
    width: '14rem',
    truncate: (gap) => gap.columns,
    cell: (gap) => <code className="font-mono text-xs">({gap.columns})</code>,
  },
  {
    id: 'referenced',
    header: 'References',
    help: 'indexes.fk-references',
    sortValue: (gap) => gap.referenced,
    width: '16rem',
    truncate: (gap) => gap.referenced,
    cell: (gap) => gap.referenced,
  },
  {
    id: 'remediation',
    header: 'Remediation',
    truncate: (gap) => gap.finding.remediation ?? '',
    cell: (gap) => <span className="text-muted-foreground">{gap.finding.remediation ?? '—'}</span>,
  },
]

/** The sentence that gives a tile's figure its noun; a zero says nothing was found (D31). */
function explained(count: number, what: string): string {
  return count === 0 ? `none found: ${what}` : what
}

const VIEW_CAPTION: Record<IndexView, CaptionId> = {
  inventory: 'indexes.inventory',
  summary: 'indexes.summary',
  findings: 'indexes.findings',
  'foreign-keys': 'indexes.foreign-keys',
}

/** The badges of a view that need a word of explanation, behind an ⓘ in the header. */
const VIEW_BADGE_HINT: Record<IndexView, ReactNode> = {
  inventory: <InfoHint id="indexes.flags" label="index flags" />,
  summary: null,
  findings: <InfoHint id="indexes.finding-rule" label="rule badge" />,
  'foreign-keys': <InfoHint id="indexes.fk-severity" label="foreign key severity" />,
}

interface IndexesPanelProps {
  instanceId: string
  /** The indexes in scope: the page's database filter already applied. */
  rows: readonly IndexRow[]
  /** How many indexes the agent reported for the server. */
  reported: number
  /** The agent or the page cut the list. */
  incomplete: boolean
  /** The most indexes the page asks for. */
  limit: number
  findings: readonly Finding[]
  findingsPending: boolean
  /** The database picked on the page; empty for all of them. */
  database: string
  dataUpdatedAt: number
  /** Why the inventory is empty, when it is. */
  emptyDescription?: string
  /** The key of a row to expand and give focus to (a search hit, D20). */
  focusKey?: string | undefined
  /** Called once the row is shown (true) or when it is not in the list (false). */
  onFocusDone?: (found: boolean) => void
}

/**
 * The Indexes tab: one view at a time (15.3) — the inventory table, the
 * summary, the advisor findings or the foreign keys without an index — under
 * one header with the scope's figures.
 */
export function IndexesPanel({
  instanceId,
  rows,
  reported,
  incomplete,
  limit,
  findings,
  findingsPending,
  database,
  dataUpdatedAt,
  emptyDescription = NO_INDEX_STATS,
  focusKey,
  onFocusDone,
}: IndexesPanelProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const view = readIndexView(searchParams.get(INDEX_VIEW_PARAM))
  // A search hit is a row of the inventory (D20): that view opens to show it.
  useEffect(() => {
    if (focusKey === undefined || view === DEFAULT_INDEX_VIEW) return
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current)
        next.delete(INDEX_VIEW_PARAM)
        return next
      },
      { replace: true },
    )
  }, [focusKey, setSearchParams, view])
  useFocusMissing(focusKey, onFocusDone, rows.length === 0)

  const summary = useMemo(() => summariseIndexes(rows), [rows])
  const tables = useMemo(() => groupByTable(rows).slice(0, 8), [rows])
  const scopedFindings = useMemo(
    () =>
      indexFindings(findings).filter(
        (finding) =>
          finding.state === 'open' && (database === '' || (finding.datname ?? '') === database),
      ),
    [findings, database],
  )
  const listedFindings = useMemo(
    () =>
      scopedFindings
        .filter((finding) => finding.rule_id !== INDEX_RULES.bloat)
        .sort((a, b) => a.rule_id.localeCompare(b.rule_id)),
    [scopedFindings],
  )
  const gaps = useMemo(
    () => foreignKeyGaps(findings).filter((gap) => database === '' || gap.datname === database),
    [findings, database],
  )
  const counts = useMemo(() => {
    const count = (rule: string) =>
      scopedFindings.filter((finding) => finding.rule_id === rule).length
    // A duplicate finding is a group of indexes with identical definitions (two at least).
    const duplicateIndexes = scopedFindings
      .filter((finding) => finding.rule_id === INDEX_RULES.duplicate)
      .reduce((sum, finding) => {
        const names = finding.evidence?.indexes
        return sum + (Array.isArray(names) ? names.length : 2)
      }, 0)
    return {
      unused: count(INDEX_RULES.unused),
      duplicate: count(INDEX_RULES.duplicate),
      duplicateIndexes,
      redundant: count(INDEX_RULES.redundantPrefix),
      invalid: count(INDEX_RULES.invalid),
      divergence: count(INDEX_RULES.divergence),
    }
  }, [scopedFindings])
  const attention = rows.filter(needsAttention).length
  // index.unused waits until the pglens history covers the configured window, or
  // on PostgreSQL 16 and later until the statistics settle it (D22b, D22c). Until
  // then the advisor reports the check as degraded, and the page says so instead
  // of showing a clean zero. Only a short history is pending: a window longer
  // than the kept history or unknown standby scans are not settled by waiting,
  // and have their own note.
  const unusedCheck = useMemo(() => unusedCheckStatus(findings, database), [findings, database])
  const unusedPending = unusedCheck.pending
  const unusedBlocked = unusedCheck.blocked.reduce((sum, group) => sum + group.count, 0)
  // Relative times are measured from when the data was fetched, so a render stays pure.
  const showDatabase = summary.byDatabase.length > 1 && database === ''
  // The configured unused-index window names the length in the rule texts
  // below: they state the rule as it stands now, which a finding's evidence
  // (the window of its evaluation) may predate. Until it loads, or if it cannot
  // be read, the texts name no length.
  const windowsQuery = useAdvisorWindows()
  const configuredWindow =
    windowsQuery.data?.find((window) => window.key === 'window.unused_index')?.seconds ?? null
  const unusedWindow = windowPhrase(configuredWindow)
  const columns = useMemo(
    () => indexColumns(showDatabase, new Date(dataUpdatedAt), unusedWindow),
    [showDatabase, dataUpdatedAt, unusedWindow],
  )

  function setView(value: string) {
    const next = new URLSearchParams(searchParams)
    const picked = readIndexView(value)
    if (picked === DEFAULT_INDEX_VIEW) next.delete(INDEX_VIEW_PARAM)
    else next.set(INDEX_VIEW_PARAM, picked)
    setSearchParams(next, { replace: true })
  }

  const stats: PanelStat[] = [
    { label: 'Indexes reported', value: formatCount(summary.total), unit: 'indexes' },
    { label: 'Total size', value: formatBytes(summary.bytes) },
    {
      label: 'Indexes needing attention',
      value: formatCount(attention),
      unit: 'indexes',
      detail: `of ${formatCount(summary.total)} ${summary.total === 1 ? 'index' : 'indexes'} reported`,
    },
    {
      label: 'Never-scanned indexes',
      value: formatCount(summary.neverScanned),
      unit: 'indexes',
      ...(summary.neverScanned > 0
        ? { detail: `${formatBytes(summary.neverScannedBytes)} in size` }
        : {}),
    },
  ]

  const incompleteText = `The list is incomplete. The agent reports only its busiest and largest indexes per database (checks.index_stats.top_n, 50 by default)${reported >= limit ? `, and this page shows at most ${limit}` : ''}. The counts cover the ${formatCountOf(reported, 'index', 'indexes')} reported, not every index of the server. Raise top_n in the agent configuration to see more.`
  const pendingHeadline = `Unused-index check pending for ${formatCount(unusedPending)} ${unusedPending === 1 ? 'index' : 'indexes'}.`
  const pendingText = `${unusedRuleText(unusedWindow)} Until the advisor has a verdict, "never scanned" is the raw statistic: no scan since the statistics were last reset.`
  const blockedHeadline = `Unused-index check inconclusive for ${formatCount(unusedBlocked)} ${unusedBlocked === 1 ? 'index' : 'indexes'}.`
  const blockedText = unusedCheck.blocked
    .map((group) =>
      unusedCheck.blocked.length === 1
        ? blockedReasonText(group.reason)
        : `${formatCount(group.count)}: ${blockedReasonText(group.reason)}`,
    )
    .join(' ')
  const notes =
    incomplete || unusedPending > 0 || unusedBlocked > 0 ? (
      <>
        {incomplete ? (
          <PanelNote title={incompleteText} tone="warning">
            <strong>The list is incomplete.</strong> The agent reports only its busiest and largest
            indexes per database (<code>checks.index_stats.top_n</code>, 50 by default)
            {reported >= limit ? `, and this page shows at most ${limit}` : ''}. The counts cover
            the {formatCountOf(reported, 'index', 'indexes')} reported, not every index of the
            server. Raise <code>top_n</code> in the agent configuration to see more.
          </PanelNote>
        ) : null}
        {unusedPending > 0 ? (
          <PanelNote title={`${pendingHeadline} ${pendingText}`}>
            <strong>{pendingHeadline}</strong> {pendingText}
          </PanelNote>
        ) : null}
        {unusedBlocked > 0 ? (
          <PanelNote title={`${blockedHeadline} ${blockedText}`}>
            <strong>{blockedHeadline}</strong> {blockedText}
          </PanelNote>
        ) : null}
      </>
    ) : null

  const heading: Record<IndexView, { title: string; description: string }> = {
    inventory: {
      title: 'Index inventory',
      description: `${formatCountOf(rows.length, 'index', 'indexes')}. Scan figures are per-second rates.`,
    },
    summary: {
      title: 'Index summary',
      description:
        'What the indexes in scope are made of: kinds, access methods, databases and top tables.',
    },
    findings: {
      // Short titles keep the header line within 1920 px beside the figures and the actions.
      title: 'Index findings',
      description: `${formatCount(counts.unused)} unused · ${formatCount(counts.duplicate)} duplicate · ${formatCount(counts.redundant)} redundant · ${formatCount(counts.invalid)} invalid · ${formatCount(counts.divergence)} diverging · ${formatCount(gaps.length)} foreign keys`,
    },
    'foreign-keys': {
      title: 'Unindexed foreign keys',
      description: 'Deleting or re-keying a referenced row scans the referencing table.',
    },
  }

  const counted: Partial<Record<IndexView, number>> = {
    findings: listedFindings.length,
    'foreign-keys': gaps.length,
  }

  const content: Record<IndexView, () => ReactNode> = {
    inventory: () =>
      rows.length === 0 ? (
        <EmptyState description={emptyDescription} title="No indexes to show" />
      ) : (
        <SimpleTable
          ariaLabel="Indexes"
          captionId="indexes.inventory-table"
          columns={columns}
          defaultSort={{ id: 'bytes', desc: true }}
          detailLabel={(row, expanded) => `${expanded ? 'Hide' : 'Show'} details of ${row.name}`}
          emptyState={null}
          renderDetail={(row) => <Detail instanceId={instanceId} row={row} />}
          rowKey={(row) => row.key}
          rows={rows}
          scope="indexes"
          tableId="indexes"
          toolbar={INDEX_TOOLBAR}
          focusKey={focusKey}
          {...(onFocusDone === undefined ? {} : { onFocusDone })}
        />
      ),
    summary: () => (
      <div className="space-y-6 min-[1600px]:space-y-4">
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <MetricTile
            captionId="indexes.summary-primary"
            detail={explained(summary.primary, 'indexes that back a primary key')}
            label="Primary key indexes"
            unit="indexes"
            value={formatCount(summary.primary)}
          />
          <MetricTile
            captionId="indexes.summary-unique"
            detail={explained(summary.unique, 'unique indexes that are not a primary key')}
            label="Unique indexes"
            unit="indexes"
            value={formatCount(summary.unique)}
          />
          <MetricTile
            captionId="indexes.summary-partial"
            detail={explained(
              summary.partial,
              'indexes that cover only the rows a predicate selects',
            )}
            label="Partial indexes"
            unit="indexes"
            value={formatCount(summary.partial)}
          />
          <MetricTile
            captionId="indexes.summary-expression"
            detail={explained(summary.expression, 'indexes with a key built from an expression')}
            label="Expression indexes"
            unit="indexes"
            value={formatCount(summary.expression)}
          />
          <MetricTile
            captionId="indexes.summary-composite"
            detail={explained(summary.composite, 'indexes with more than one key column')}
            label="Composite indexes"
            unit="indexes"
            value={formatCount(summary.composite)}
          />
          <MetricTile
            captionId="indexes.summary-invalid"
            detail={explained(summary.invalid, 'indexes PostgreSQL marks invalid')}
            label="Invalid indexes"
            unit="indexes"
            value={formatCount(summary.invalid)}
            {...(summary.invalid > 0
              ? { status: { tone: 'crit' as const, text: 'Rebuild or drop' } }
              : {})}
          />
          <MetricTile
            captionId="index.duplicate"
            detail={
              findingsPending
                ? 'the advisor findings are loading'
                : counts.duplicate === 0
                  ? explained(0, 'groups of indexes with identical definitions')
                  : `in ${formatCount(counts.duplicate)} ${counts.duplicate === 1 ? 'group' : 'groups'} with identical definitions`
            }
            label="Exact duplicate indexes"
            unit="indexes"
            value={findingsPending ? null : formatCount(counts.duplicateIndexes)}
            {...(!findingsPending && counts.duplicate > 0
              ? { status: { tone: 'warn' as const, text: 'Review before dropping' } }
              : {})}
          />
          <MetricTile
            captionId="index.redundant-prefix"
            detail={
              findingsPending
                ? 'the advisor findings are loading'
                : explained(counts.redundant, 'indexes whose columns lead a longer index')
            }
            label="Redundant prefix indexes"
            unit="indexes"
            value={findingsPending ? null : formatCount(counts.redundant)}
            {...(!findingsPending && counts.redundant > 0
              ? { status: { tone: 'neutral' as const, text: 'Check the longer index first' } }
              : {})}
          />
          <MetricTile
            captionId="indexes.foreign-keys"
            detail={
              findingsPending
                ? 'the advisor findings are loading'
                : explained(gaps.length, 'foreign keys that no index leads with')
            }
            label="Foreign keys without an index"
            unit="foreign keys"
            value={findingsPending ? null : formatCount(gaps.length)}
            {...(!findingsPending && gaps.length > 0
              ? { status: { tone: 'warn' as const, text: 'Deletes scan the referencing table' } }
              : {})}
          />
        </div>
        {summary.total > 0 ? (
          <div className="grid gap-4 lg:grid-cols-3">
            <Breakdown
              captionId="indexes.breakdown-method"
              title="By access method"
              entries={summary.byMethod}
            />
            <Breakdown
              captionId="indexes.breakdown-database"
              title="By database"
              entries={summary.byDatabase}
            />
            <Breakdown
              captionId="indexes.breakdown-tables"
              title="Tables with the most indexes"
              entries={tables.map((group) => ({
                label: `${group.schema}.${group.table}`,
                count: group.indexes.length,
                bytes: group.bytes,
              }))}
            />
          </div>
        ) : null}
      </div>
    ),
    findings: () =>
      listedFindings.length === 0 ? (
        <EmptyState
          description={
            findingsPending
              ? 'The advisor findings are loading.'
              : 'The advisor has no open finding on these indexes.'
          }
          title="No open findings"
        />
      ) : (
        <SimpleTable
          ariaLabel="Index findings by index"
          captionId="indexes.findings-table"
          columns={FINDING_COLUMNS}
          detailLabel={(finding, expanded) =>
            `${expanded ? 'Hide' : 'Show'} the detail of ${finding.title}`
          }
          emptyState={null}
          renderDetail={(finding) => <FindingDetail finding={finding} />}
          rowKey={(finding) => finding.finding_id}
          rows={listedFindings}
          scope="findings"
          tableId="index-findings"
          toolbar={FINDINGS_TOOLBAR}
        />
      ),
    'foreign-keys': () =>
      gaps.length === 0 ? (
        <EmptyState
          description={
            findingsPending
              ? 'The advisor findings are loading.'
              : 'The advisor found no foreign key without a covering index.'
          }
          title="No missing foreign-key index"
        />
      ) : (
        <SimpleTable
          ariaLabel="Foreign keys without an index"
          captionId="indexes.foreign-keys-table"
          columns={GAP_COLUMNS}
          emptyState={null}
          rowKey={(gap) => gap.finding.finding_id}
          rows={gaps}
          scope="foreign keys"
          tableId="fk-gaps"
          toolbar={GAPS_TOOLBAR}
        />
      ),
  }

  return (
    <Tabs onValueChange={setView} value={view}>
      <IndexPanel
        actions={
          <>
            {view === 'findings' ? (
              // From 1600 px "Open" leaves the line and stays in the link's name, so
              // the findings view's header line still fits at 1920 px (bug-HDR).
              <Link
                className="text-primary text-sm font-medium whitespace-nowrap hover:underline"
                data-testid="index-findings-link"
                to="/findings"
              >
                <span className="min-[1600px]:sr-only">Open</span> Findings
                <ArrowRight
                  aria-hidden="true"
                  className="ml-0.5 inline size-3.5 align-[-0.125em]"
                />
              </Link>
            ) : null}
            {VIEW_BADGE_HINT[view]}
            <TabsList aria-label="Index views" className="min-[1600px]:h-8!">
              {INDEX_VIEWS.map((option) => (
                <TabsTrigger key={option.id} className="min-[1600px]:py-0.5" value={option.id}>
                  {option.label}
                  {counted[option.id] === undefined ? null : (
                    <span className="text-muted-foreground text-xs tabular-nums">
                      {formatCount(counted[option.id] ?? 0)}
                    </span>
                  )}
                </TabsTrigger>
              ))}
            </TabsList>
          </>
        }
        captionId={VIEW_CAPTION[view]}
        description={heading[view].description}
        notes={notes}
        stats={stats}
        title={heading[view].title}
        titleId="index-panel-title"
      >
        {INDEX_VIEWS.map((option) => (
          <TabsContent key={option.id} value={option.id}>
            {content[option.id]()}
          </TabsContent>
        ))}
      </IndexPanel>
    </Tabs>
  )
}
