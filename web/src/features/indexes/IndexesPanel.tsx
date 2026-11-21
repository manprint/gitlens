import { useEffect, useMemo, type ReactNode } from 'react'
import { ArrowRight } from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'

import { useAdvisorWindows } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import { HelpLink } from '@/components/layout/HelpLink'
import { InfoHint } from '@/components/layout/InfoHint'
import { MetricTile } from '@/components/layout/MetricTile'
import { SqlText } from '@/components/layout/SqlText'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useFocusMissing } from '@/hooks/useSearch'
import { useT, type Translate } from '@/i18n/react'
import type { CaptionId } from '@/lib/captions'
import { formatBytes, formatCount, formatPercent, formatRate, formatRelative } from '@/lib/format'
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
import { FLAG_GUIDE, flagTitle, guideText, type FlagLabel } from '@/lib/indexGuide'

import { IndexDetail } from './IndexDetail'
import { noIndexStats } from './emptyText'
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
function indexKinds(t: Translate): readonly {
  value: string
  label: string
  matches: (row: IndexRow) => boolean
}[] {
  return [
    { value: 'primary', label: t('indexes.panel.kindPrimary'), matches: (row) => row.primary },
    {
      value: 'unique',
      label: t('indexes.panel.kindUnique'),
      matches: (row) => row.unique && !row.primary,
    },
    {
      value: 'partial',
      label: t('indexes.panel.kindPartial'),
      matches: (row) => row.parsed.predicate !== null,
    },
    {
      value: 'plain',
      label: t('indexes.panel.kindPlain'),
      matches: (row) => !row.unique && !row.primary && row.parsed.predicate === null,
    },
  ]
}

function advisorFlagOptions(t: Translate): readonly { value: RowFilter; label: string }[] {
  return [
    { value: 'attention', label: t('indexes.panel.advisorNeedsAttention') },
    { value: 'unused', label: t('indexes.panel.advisorUnused') },
    { value: 'duplicate', label: t('indexes.panel.advisorDuplicate') },
  ]
}

// The page's Database select scopes the rows already: no database filter (tables inventory).
function indexToolbarSpec(t: Translate): TableToolbarSpec<IndexRow> {
  const kinds = indexKinds(t)
  return {
    searchLabel: t('indexes.panel.searchIndexes'),
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
        label: t('indexes.panel.filterState'),
        kind: 'select',
        options: [
          { value: 'valid', label: t('indexes.panel.stateValid') },
          { value: 'invalid', label: t('indexes.panel.stateInvalid') },
        ],
        accessor: (row) => (matchesFilter(row, 'invalid') ? 'invalid' : 'valid'),
      },
      {
        id: 'kind',
        label: t('indexes.panel.filterKind'),
        kind: 'select',
        options: kinds.map(({ value, label }) => ({ value, label })),
        accessor: (row) => kinds.find((kind) => kind.matches(row))?.value,
        predicate: (row, value) =>
          kinds.find((kind) => kind.value === value)?.matches(row) ?? false,
      },
      {
        id: 'advisor',
        label: t('indexes.panel.filterAdvisor'),
        kind: 'select',
        options: advisorFlagOptions(t),
        accessor: () => null,
        predicate: (row, value) => matchesFilter(row, value as RowFilter),
      },
    ],
  }
}

function findingsToolbarSpec(t: Translate): TableToolbarSpec<Finding> {
  return {
    searchLabel: t('indexes.panel.searchFindings'),
    searchFields: [
      (finding) => finding.rule_id,
      (finding) => finding.title,
      (finding) => finding.object_name,
      (finding) => finding.datname,
      (finding) => finding.detail,
    ],
    filters: [
      {
        id: 'rule',
        label: t('indexes.panel.filterRule'),
        kind: 'select',
        accessor: (finding) => finding.rule_id,
      },
    ],
  }
}

function gapsToolbarSpec(t: Translate): TableToolbarSpec<ForeignKeyGap> {
  return {
    searchLabel: t('indexes.panel.searchForeignKeys'),
    searchFields: [
      (gap) => gap.table,
      (gap) => gap.constraint,
      (gap) => gap.columns,
      (gap) => gap.referenced,
      (gap) => gap.datname,
    ],
    filters: [
      {
        id: 'severity',
        label: t('indexes.panel.filterSeverity'),
        kind: 'select',
        accessor: (gap) => gap.finding.severity,
      },
    ],
  }
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
  t: Translate,
  showDatabase: boolean,
  now: Date,
  unusedWindow: string,
): SimpleColumn<IndexRow>[] {
  return [
    {
      id: 'name',
      header: t('indexes.panel.colIndex'),
      sortValue: (row) => sortValue(row, 'name'),
      className: 'font-medium break-all max-[1599px]:min-w-48',
      width: '16rem',
      truncate: (row) => row.name,
      cell: (row) => row.name,
    },
    {
      id: 'table',
      header: t('indexes.panel.colTable'),
      sortValue: (row) => sortValue(row, 'table'),
      className: 'break-all max-[1599px]:min-w-48',
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
      header: t('indexes.panel.colColumns'),
      truncate: (row) => (row.parsed.columns.length === 0 ? '—' : summariseColumns(row.parsed)),
      cell: (row) => (
        <code className="font-mono text-xs break-words">
          {row.parsed.columns.length === 0 ? '—' : summariseColumns(row.parsed)}
        </code>
      ),
    },
    {
      id: 'method',
      header: t('indexes.panel.colMethod'),
      width: '5rem',
      cell: (row) => row.parsed.method ?? '—',
    },
    {
      id: 'flags',
      header: t('indexes.panel.colFlags'),
      help: 'indexes.flags-column',
      width: '15rem',
      cell: (row) => <RowBadges row={row} unusedWindow={unusedWindow} />,
    },
    {
      id: 'bytes',
      header: t('indexes.panel.colSize'),
      sortValue: (row) => sortValue(row, 'bytes'),
      firstDirection: 'desc',
      className: 'whitespace-nowrap',
      width: '6rem',
      cell: (row) => (row.bytes === null ? t('indexes.panel.unknown') : formatBytes(row.bytes)),
    },
    {
      id: 'scanRate',
      header: t('indexes.panel.colScans'),
      help: 'indexes.scans',
      sortValue: (row) => sortValue(row, 'scanRate'),
      firstDirection: 'desc',
      className: 'whitespace-nowrap',
      width: '9rem',
      cell: (row) => scansText(t, row),
    },
    {
      id: 'cache',
      header: t('indexes.panel.colCacheHit'),
      help: 'indexes.cache-hit',
      className: 'whitespace-nowrap',
      width: '6.5rem',
      cell: (row) => (row.cacheHitRatio === null ? '—' : formatPercent(row.cacheHitRatio, 1)),
    },
    {
      id: 'lastScan',
      header: t('indexes.panel.colLastScan'),
      help: 'indexes.last-scan',
      sortValue: (row) => sortValue(row, 'lastScan'),
      firstDirection: 'desc',
      className: 'whitespace-nowrap',
      width: '12rem',
      truncate: (row) => lastScanText(t, row, now),
      cell: (row) => lastScanText(t, row, now),
    },
  ]
}

/**
 * The scan rate. Without a rate the statistics can still say the index was never
 * scanned since the reset (the Flags column says "never scanned"), so say that
 * here instead of "Unknown".
 */
function scansText(t: Translate, row: IndexRow): string {
  if (row.scanRate !== null) return formatRate(row.scanRate)
  return row.lastScanStatus === 'never_since_reset'
    ? t('indexes.panel.scansNoneSinceReset')
    : t('indexes.panel.unknown')
}

function lastScanText(t: Translate, row: IndexRow, now: Date): string {
  switch (row.lastScanStatus) {
    case 'scanned':
      return row.lastScanAt === null
        ? t('indexes.panel.unknown')
        : formatRelative(row.lastScanAt, now)
    case 'never_since_reset':
      return t('indexes.panel.lastScanNever')
    case 'not_available':
      return t('indexes.panel.notAvailable')
  }
}

/**
 * How the advisor reaches an unused verdict (D22b, D22c): the configured
 * window without a scan in the pglens history or, on PostgreSQL 16 and later,
 * from the statistics alone (no scan since a reset at least that old, or a
 * last scan older than the window), without waiting for pglens history.
 */
function unusedRuleText(t: Translate, unusedWindow: string): string {
  return t('indexes.panel.unusedRule', { window: unusedWindow })
}

/**
 * The "never scanned" hint. The advisor never judges an index of
 * UNUSED_INDEX_MIN_BYTES or less (nor a primary key or a unique index, which
 * get no such flag), so for a small one the hint promises no verdict.
 */
function neverScannedHint(t: Translate, row: IndexRow, unusedWindow: string): string {
  const counter = t('indexes.panel.neverCounter')
  if (row.bytes !== null && row.bytes <= UNUSED_INDEX_MIN_BYTES) {
    const small = t('indexes.panel.neverSmallIndex', {
      size: formatBytes(UNUSED_INDEX_MIN_BYTES),
    })
    return `${counter} ${small}`
  }
  return `${counter} ${unusedRuleText(t, unusedWindow)}`
}

/**
 * What an unused-index check that waiting does not settle means, from the
 * advisor's reason (`internal/advisor/rules_relation.go`); a reason this page
 * does not know is shown as the advisor wrote it.
 */
function blockedReasonText(t: Translate, reason: string): string {
  if (reason.includes('longer than the kept index metrics history')) {
    return t('indexes.panel.blockedWindow', { reason: reason.replace(/^the /, '') })
  }
  if (reason.startsWith('standby scans unknown')) return t('indexes.panel.blockedStandby')
  if (reason === '') return t('indexes.panel.blockedNoReason')
  return `${reason.charAt(0).toUpperCase()}${reason.slice(1)}.`
}

interface RowFlag {
  label: FlagLabel
  variant: BadgeVariant
  /** What the flag means for this index and what to do, for the badge tooltip. */
  title: string
}

/**
 * The flags of an index, problems first so that they stay visible on the one
 * line the strip has from 1600 px; `unusedWindow` names the unused-index
 * window in the "never scanned" hint.
 */
function rowFlags(t: Translate, row: IndexRow, unusedWindow: string): RowFlag[] {
  const flag = (label: FlagLabel, variant: BadgeVariant): RowFlag => ({
    label,
    variant,
    title: flagTitle(label),
  })
  const flags: RowFlag[] = []
  if (!row.valid || row.invalidFinding !== null) flags.push(flag('invalid', 'destructive'))
  if (row.unusedFinding !== null) flags.push(flag('unused', 'warning'))
  if (row.duplicateOf.length > 0) flags.push(flag('duplicate', 'warning'))
  if (row.redundantFinding !== null) flags.push(flag('redundant prefix', 'warning'))
  if (row.divergenceFinding !== null) flags.push(flag('diverges', 'warning'))
  if (
    row.unusedFinding === null &&
    row.lastScanStatus === 'never_since_reset' &&
    !row.primary &&
    !row.unique
  ) {
    flags.push({
      // FlagLabel token, not display text: the badge shows flagText().
      // eslint-disable-next-line pglens/no-literal-string
      label: 'never scanned',
      variant: 'outline',
      title: `${neverScannedHint(t, row, unusedWindow)} ${t('indexes.panel.whatToDo')} ${guideText(FLAG_GUIDE['never scanned'].action)}`,
    })
  }
  if (row.primary) flags.push(flag('primary', 'secondary'))
  if (row.unique && !row.primary) flags.push(flag('unique', 'secondary'))
  if (row.parsed.predicate !== null) flags.push(flag('partial', 'outline'))
  if (row.parsed.columns.some((column) => column.expression))
    flags.push(flag('expression', 'outline'))
  if (row.parsed.include.length > 0) flags.push(flag('include', 'outline'))
  return flags
}

function flagText(t: Translate, label: FlagLabel): string {
  switch (label) {
    case 'invalid':
      return t('indexes.panel.flagInvalid')
    case 'unused':
      return t('indexes.panel.flagUnused')
    case 'duplicate':
      return t('indexes.panel.flagDuplicate')
    case 'redundant prefix':
      return t('indexes.panel.flagRedundantPrefix')
    case 'diverges':
      return t('indexes.panel.flagDiverges')
    case 'never scanned':
      return t('indexes.panel.flagNeverScanned')
    case 'primary':
      return t('indexes.panel.flagPrimary')
    case 'unique':
      return t('indexes.panel.flagUnique')
    case 'partial':
      return t('indexes.panel.flagPartial')
    case 'expression':
      return t('indexes.panel.flagExpression')
    case 'include':
      return t('indexes.panel.flagInclude')
  }
}

function severityText(t: Translate, severity: string): string {
  if (severity === 'critical') return t('indexes.panel.severityCritical')
  if (severity === 'warning') return t('indexes.panel.severityWarning')
  if (severity === 'info') return t('indexes.panel.severityInfo')
  return severity
}

type BadgeVariant = 'secondary' | 'outline' | 'destructive' | 'warning'

/** The badges wrap below 1600 px; from 1600 px they stay on one line, the list in the `title`. */
function RowBadges({ row, unusedWindow }: { row: IndexRow; unusedWindow: string }) {
  const t = useT()
  const flags = rowFlags(t, row, unusedWindow)
  return (
    <div
      // From 1600 px one line high: a badge that does not fit wraps out of sight whole,
      // never cut in half; the title lists every flag.
      className="flex flex-wrap gap-1 min-[1600px]:max-h-5 min-[1600px]:overflow-hidden"
      title={
        flags.length === 0
          ? undefined
          : t('indexes.panel.flagsTitle', {
              flags: flags.map((flag) => flagText(t, flag.label)).join(', '),
            })
      }
    >
      {flags.map((flag) => (
        <Badge key={flag.label} className={CELL_BADGE} title={flag.title} variant={flag.variant}>
          {flagText(t, flag.label)}
        </Badge>
      ))}
    </div>
  )
}

/** Every flag of the row with its meaning and the action, in the row details. */
function FlagNotes({ row, unusedWindow }: { row: IndexRow; unusedWindow: string }) {
  const t = useT()
  const flags = rowFlags(t, row, unusedWindow)
  if (flags.length === 0) return null
  return (
    <section aria-label={t('indexes.panel.flagsSection')} className="space-y-2">
      <div className="flex items-center gap-3">
        <h4 className="text-muted-foreground text-xs font-semibold tracking-wide uppercase">
          {t('indexes.panel.flagsSection')}
        </h4>
        <HelpLink anchor="indexes#flags" label={t('indexes.panel.indexFlags')} />
      </div>
      <dl className="grid gap-x-4 gap-y-2 text-sm sm:grid-cols-[auto_1fr]">
        {flags.map((flag) => (
          <div key={flag.label} className="contents">
            <dt>
              <Badge className={CELL_BADGE} variant={flag.variant}>
                {flagText(t, flag.label)}
              </Badge>
            </dt>
            <dd>
              {flag.label === 'never scanned'
                ? neverScannedHint(t, row, unusedWindow)
                : guideText(FLAG_GUIDE[flag.label].meaning)}{' '}
              <span className="font-medium">{t('indexes.panel.whatToDo')}</span>{' '}
              {guideText(FLAG_GUIDE[flag.label].action)}
            </dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

function Detail({
  row,
  instanceId,
  unusedWindow,
}: {
  row: IndexRow
  instanceId: string
  unusedWindow: string
}) {
  const t = useT()

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
            {t('indexes.panel.colColumns')}
          </h4>
          {row.parsed.columns.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              {t('indexes.panel.definitionUnreadable')}
            </p>
          ) : (
            <ol className="space-y-1 text-sm">
              {row.parsed.columns.map((column, index) => (
                <li key={`${index}-${column.text}`} className="flex flex-wrap items-baseline gap-2">
                  <span className="text-muted-foreground w-5 text-right tabular-nums">
                    {index + 1}
                  </span>
                  <code className="font-mono">{column.text}</code>
                  {column.expression ? (
                    <Badge variant="outline">{t('indexes.panel.flagExpression')}</Badge>
                  ) : null}
                  {column.modifiers ? (
                    <span className="text-muted-foreground text-xs">{column.modifiers}</span>
                  ) : null}
                </li>
              ))}
            </ol>
          )}
          {row.parsed.include.length > 0 ? (
            <p className="text-sm">
              <span className="text-muted-foreground">{t('indexes.panel.included')} </span>
              <code className="font-mono">
                {row.parsed.include.map((column) => column.text).join(', ')}
              </code>
            </p>
          ) : null}
        </div>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
          <dt className="text-muted-foreground">{t('indexes.panel.accessMethod')}</dt>
          <dd>{row.parsed.method ?? t('indexes.panel.unknownMethod')}</dd>
          <dt className="text-muted-foreground">{t('indexes.panel.predicate')}</dt>
          <dd>
            {row.parsed.predicate === null ? (
              t('indexes.panel.noneCoversEveryRow')
            ) : (
              <code className="font-mono">{row.parsed.predicate}</code>
            )}
          </dd>
          {row.parsed.options ? (
            <>
              <dt className="text-muted-foreground">{t('indexes.panel.storageOptions')}</dt>
              <dd>
                <code className="font-mono">{row.parsed.options}</code>
              </dd>
            </>
          ) : null}
          <dt className="text-muted-foreground">{t('indexes.panel.tuplesRead')}</dt>
          <dd>
            {row.tupleReadRate === null
              ? t('indexes.panel.unknown')
              : `${formatRate(row.tupleReadRate)}`}
          </dd>
          <dt className="text-muted-foreground">{t('indexes.panel.tuplesFetched')}</dt>
          <dd>
            {row.tupleFetchRate === null
              ? t('indexes.panel.unknown')
              : `${formatRate(row.tupleFetchRate)}`}
          </dd>
          {row.duplicateOf.length > 0 ? (
            <>
              <dt className="text-muted-foreground">{t('indexes.panel.sameDefinitionAs')}</dt>
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
            <CopyButton subject={t('indexes.panel.indexDefinition')} text={row.definition} />
          </div>
        </div>
      ) : null}

      <FlagNotes row={row} unusedWindow={unusedWindow} />

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
  const t = useT()
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
              {t('indexes.panel.indexCount', {
                count: entry.count,
                shown: formatCount(entry.count),
              })}{' '}
              · {formatBytes(entry.bytes)}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}

function findingColumns(t: Translate): readonly SimpleColumn<Finding>[] {
  return [
    {
      id: 'rule',
      header: t('indexes.panel.filterRule'),
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
      header: t('indexes.panel.colDatabase'),
      sortValue: (finding) => finding.datname ?? null,
      width: '9rem',
      truncate: (finding) => finding.datname ?? '—',
      cell: (finding) => finding.datname ?? '—',
    },
    {
      id: 'object',
      header: t('indexes.panel.colObject'),
      sortValue: (finding) => finding.object_name ?? null,
      width: '18rem',
      truncate: (finding) => finding.object_name ?? '—',
      cell: (finding) => <code className="font-mono text-xs">{finding.object_name ?? '—'}</code>,
    },
    {
      id: 'title',
      header: t('indexes.panel.colFinding'),
      className: 'font-medium',
      truncate: (finding) => finding.title,
      cell: (finding) => finding.title,
    },
  ]
}

function FindingDetail({ finding }: { finding: Finding }) {
  const t = useT()

  return (
    <div className="space-y-1 px-4 py-2 text-sm">
      {finding.detail ? <p className="text-muted-foreground">{finding.detail}</p> : null}
      {finding.remediation ? <p>{finding.remediation}</p> : null}
      {!finding.detail && !finding.remediation ? (
        <p className="text-muted-foreground">{t('indexes.panel.noFurtherDetail')}</p>
      ) : null}
    </div>
  )
}

function gapColumns(t: Translate): readonly SimpleColumn<ForeignKeyGap>[] {
  return [
    {
      id: 'severity',
      header: t('indexes.panel.filterSeverity'),
      help: 'indexes.fk-severity-column',
      sortValue: (gap) => gap.finding.severity,
      width: '7rem',
      cell: (gap) => (
        <Badge
          className={CELL_BADGE}
          variant={gap.finding.severity === 'info' ? 'outline' : 'warning'}
        >
          {severityText(t, gap.finding.severity)}
        </Badge>
      ),
    },
    {
      id: 'database',
      header: t('indexes.panel.colDatabase'),
      sortValue: (gap) => gap.datname,
      width: '9rem',
      truncate: (gap) => gap.datname || '—',
      cell: (gap) => gap.datname || '—',
    },
    {
      id: 'table',
      header: t('indexes.panel.colTable'),
      sortValue: (gap) => gap.table,
      width: '16rem',
      truncate: (gap) => gap.table,
      cell: (gap) => <code className="font-mono">{gap.table}</code>,
    },
    {
      id: 'columns',
      header: t('indexes.panel.colColumns'),
      help: 'indexes.fk-columns',
      width: '14rem',
      truncate: (gap) => gap.columns,
      cell: (gap) => <code className="font-mono text-xs">({gap.columns})</code>,
    },
    {
      id: 'referenced',
      header: t('indexes.panel.colReferences'),
      help: 'indexes.fk-references',
      sortValue: (gap) => gap.referenced,
      width: '16rem',
      truncate: (gap) => gap.referenced,
      cell: (gap) => gap.referenced,
    },
    {
      id: 'remediation',
      header: t('indexes.panel.colRemediation'),
      truncate: (gap) => gap.finding.remediation ?? '',
      cell: (gap) => (
        <span className="text-muted-foreground">{gap.finding.remediation ?? '—'}</span>
      ),
    },
  ]
}

/** The sentence that gives a tile's figure its noun; a zero says nothing was found (D31). */
function explained(t: Translate, count: number, what: string): string {
  return count === 0 ? t('indexes.panel.noneFound', { what }) : what
}

const VIEW_CAPTION: Record<IndexView, CaptionId> = {
  inventory: 'indexes.inventory',
  summary: 'indexes.summary',
  findings: 'indexes.findings',
  'foreign-keys': 'indexes.foreign-keys',
}

/** The badges of a view that need a word of explanation, behind an ⓘ in the header. */
function viewBadgeHint(t: Translate): Record<IndexView, ReactNode> {
  return {
    inventory: <InfoHint id="indexes.flags" label={t('indexes.panel.indexFlags')} />,
    summary: null,
    findings: <InfoHint id="indexes.finding-rule" label={t('indexes.panel.ruleBadge')} />,
    'foreign-keys': <InfoHint id="indexes.fk-severity" label={t('indexes.panel.fkSeverity')} />,
  }
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
  emptyDescription = noIndexStats(),
  focusKey,
  onFocusDone,
}: IndexesPanelProps) {
  const t = useT()
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
    () => indexColumns(t, showDatabase, new Date(dataUpdatedAt), unusedWindow),
    [t, showDatabase, dataUpdatedAt, unusedWindow],
  )

  function setView(value: string) {
    const next = new URLSearchParams(searchParams)
    const picked = readIndexView(value)
    if (picked === DEFAULT_INDEX_VIEW) next.delete(INDEX_VIEW_PARAM)
    else next.set(INDEX_VIEW_PARAM, picked)
    setSearchParams(next, { replace: true })
  }

  const stats: PanelStat[] = [
    {
      label: t('indexes.panel.statIndexesReported'),
      value: formatCount(summary.total),
      unit: 'indexes',
    },
    { label: t('indexes.panel.statTotalSize'), value: formatBytes(summary.bytes) },
    {
      label: t('indexes.panel.statNeedAttention'),
      value: formatCount(attention),
      unit: 'indexes',
      detail: t('indexes.panel.statOfReported', {
        count: summary.total,
        shown: formatCount(summary.total),
      }),
    },
    {
      label: t('indexes.panel.statNeverScanned'),
      value: formatCount(summary.neverScanned),
      unit: 'indexes',
      ...(summary.neverScanned > 0
        ? {
            detail: t('indexes.panel.statNeverSize', {
              size: formatBytes(summary.neverScannedBytes),
            }),
          }
        : {}),
    },
  ]

  const incompleteText = `${t('indexes.panel.incompleteLead')} ${t('indexes.panel.incompleteIntro')}checks.index_stats.top_n${t('indexes.panel.incompleteDefault')}${reported >= limit ? t('indexes.panel.incompleteLimit', { limit }) : ''}${t('indexes.panel.incompleteCounts', { count: reported, shown: formatCount(reported) })}top_n${t('indexes.panel.incompleteEnd')}`
  const pendingHeadline = t('indexes.panel.pendingHeadline', {
    count: unusedPending,
    shown: formatCount(unusedPending),
  })
  const pendingText = `${unusedRuleText(t, unusedWindow)} ${t('indexes.panel.pendingVerdict')}`
  const blockedHeadline = t('indexes.panel.blockedHeadline', {
    count: unusedBlocked,
    shown: formatCount(unusedBlocked),
  })
  const blockedText = unusedCheck.blocked
    .map((group) =>
      unusedCheck.blocked.length === 1
        ? blockedReasonText(t, group.reason)
        : `${formatCount(group.count)}: ${blockedReasonText(t, group.reason)}`,
    )
    .join(' ')
  const notes =
    incomplete || unusedPending > 0 || unusedBlocked > 0 ? (
      <>
        {incomplete ? (
          <PanelNote title={incompleteText} tone="warning">
            <strong>{t('indexes.panel.incompleteLead')}</strong>{' '}
            {t('indexes.panel.incompleteIntro')}
            <code>checks.index_stats.top_n</code>
            {t('indexes.panel.incompleteDefault')}
            {reported >= limit ? t('indexes.panel.incompleteLimit', { limit }) : ''}
            {t('indexes.panel.incompleteCounts', { count: reported, shown: formatCount(reported) })}
            <code>top_n</code>
            {t('indexes.panel.incompleteEnd')}
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
      title: t('indexes.panel.headInventory'),
      description: t('indexes.panel.headInventoryDesc', {
        count: rows.length,
        shown: formatCount(rows.length),
      }),
    },
    summary: {
      title: t('indexes.panel.headSummary'),
      description: t('indexes.panel.headSummaryDesc'),
    },
    findings: {
      // Short titles keep the header line within 1920 px beside the figures and the actions.
      title: t('indexes.panel.headFindings'),
      description: t('indexes.panel.headFindingsDesc', {
        unused: formatCount(counts.unused),
        duplicate: formatCount(counts.duplicate),
        redundant: formatCount(counts.redundant),
        invalid: formatCount(counts.invalid),
        diverging: formatCount(counts.divergence),
        foreignKeys: formatCount(gaps.length),
      }),
    },
    'foreign-keys': {
      title: t('indexes.panel.headFk'),
      description: t('indexes.panel.headFkDesc'),
    },
  }

  const counted: Partial<Record<IndexView, number>> = {
    findings: listedFindings.length,
    'foreign-keys': gaps.length,
  }

  const content: Record<IndexView, () => ReactNode> = {
    inventory: () =>
      rows.length === 0 ? (
        <EmptyState description={emptyDescription} title={t('indexes.panel.noIndexesToShow')} />
      ) : (
        <SimpleTable
          ariaLabel={t('indexes.panel.tableIndexes')}
          captionId="indexes.inventory-table"
          columns={columns}
          defaultSort={{ id: 'bytes', desc: true }}
          detailLabel={(row, expanded) =>
            expanded
              ? t('indexes.panel.hideDetails', { name: row.name })
              : t('indexes.panel.showDetails', { name: row.name })
          }
          emptyState={null}
          renderDetail={(row) => (
            <Detail instanceId={instanceId} row={row} unusedWindow={unusedWindow} />
          )}
          rowKey={(row) => row.key}
          rows={rows}
          scope={t('indexes.panel.scopeIndexes')}
          tableId="indexes"
          toolbar={indexToolbarSpec(t)}
          focusKey={focusKey}
          {...(onFocusDone === undefined ? {} : { onFocusDone })}
        />
      ),
    summary: () => (
      <div className="space-y-6 min-[1600px]:space-y-4">
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <MetricTile
            captionId="indexes.summary-primary"
            detail={explained(t, summary.primary, t('indexes.panel.whatPrimary'))}
            label={t('indexes.panel.labelPrimary')}
            unit="indexes"
            value={formatCount(summary.primary)}
          />
          <MetricTile
            captionId="indexes.summary-unique"
            detail={explained(t, summary.unique, t('indexes.panel.whatUnique'))}
            label={t('indexes.panel.labelUnique')}
            unit="indexes"
            value={formatCount(summary.unique)}
          />
          <MetricTile
            captionId="indexes.summary-partial"
            detail={explained(t, summary.partial, t('indexes.panel.whatPartial'))}
            label={t('indexes.panel.labelPartial')}
            unit="indexes"
            value={formatCount(summary.partial)}
          />
          <MetricTile
            captionId="indexes.summary-expression"
            detail={explained(t, summary.expression, t('indexes.panel.whatExpression'))}
            label={t('indexes.panel.labelExpression')}
            unit="indexes"
            value={formatCount(summary.expression)}
          />
          <MetricTile
            captionId="indexes.summary-composite"
            detail={explained(t, summary.composite, t('indexes.panel.whatComposite'))}
            label={t('indexes.panel.labelComposite')}
            unit="indexes"
            value={formatCount(summary.composite)}
          />
          <MetricTile
            captionId="indexes.summary-invalid"
            detail={explained(t, summary.invalid, t('indexes.panel.whatInvalid'))}
            label={t('indexes.panel.labelInvalid')}
            unit="indexes"
            value={formatCount(summary.invalid)}
            {...(summary.invalid > 0
              ? { status: { tone: 'crit' as const, text: t('indexes.panel.statusRebuild') } }
              : {})}
          />
          <MetricTile
            captionId="index.duplicate"
            detail={
              findingsPending
                ? t('indexes.panel.loadingLower')
                : counts.duplicate === 0
                  ? explained(t, 0, t('indexes.panel.whatDuplicate'))
                  : t('indexes.panel.duplicateIn', {
                      count: counts.duplicate,
                      shown: formatCount(counts.duplicate),
                    })
            }
            label={t('indexes.panel.labelDuplicate')}
            unit="indexes"
            value={findingsPending ? null : formatCount(counts.duplicateIndexes)}
            {...(!findingsPending && counts.duplicate > 0
              ? { status: { tone: 'warn' as const, text: t('indexes.panel.statusReview') } }
              : {})}
          />
          <MetricTile
            captionId="index.redundant-prefix"
            detail={
              findingsPending
                ? t('indexes.panel.loadingLower')
                : explained(t, counts.redundant, t('indexes.panel.whatRedundant'))
            }
            label={t('indexes.panel.labelRedundant')}
            unit="indexes"
            value={findingsPending ? null : formatCount(counts.redundant)}
            {...(!findingsPending && counts.redundant > 0
              ? { status: { tone: 'neutral' as const, text: t('indexes.panel.statusCheckLonger') } }
              : {})}
          />
          <MetricTile
            captionId="indexes.foreign-keys"
            detail={
              findingsPending
                ? t('indexes.panel.loadingLower')
                : explained(t, gaps.length, t('indexes.panel.whatForeignKeys'))
            }
            label={t('indexes.panel.labelFkWithout')}
            unit="foreign keys"
            value={findingsPending ? null : formatCount(gaps.length)}
            {...(!findingsPending && gaps.length > 0
              ? { status: { tone: 'warn' as const, text: t('indexes.panel.statusDeletes') } }
              : {})}
          />
        </div>
        {summary.total > 0 ? (
          <div className="grid gap-4 lg:grid-cols-3">
            <Breakdown
              captionId="indexes.breakdown-method"
              title={t('indexes.panel.breakdownMethod')}
              entries={summary.byMethod}
            />
            <Breakdown
              captionId="indexes.breakdown-database"
              title={t('indexes.panel.breakdownDatabase')}
              entries={summary.byDatabase}
            />
            <Breakdown
              captionId="indexes.breakdown-tables"
              title={t('indexes.panel.breakdownTables')}
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
            findingsPending ? t('indexes.panel.loading') : t('indexes.panel.noOpenFinding')
          }
          title={t('indexes.panel.noOpenFindings')}
        />
      ) : (
        <SimpleTable
          ariaLabel={t('indexes.panel.findingsTableLabel')}
          captionId="indexes.findings-table"
          columns={findingColumns(t)}
          detailLabel={(finding, expanded) =>
            expanded
              ? t('indexes.panel.hideFinding', { title: finding.title })
              : t('indexes.panel.showFinding', { title: finding.title })
          }
          emptyState={null}
          renderDetail={(finding) => <FindingDetail finding={finding} />}
          rowKey={(finding) => finding.finding_id}
          rows={listedFindings}
          scope={t('indexes.panel.scopeFindings')}
          tableId="index-findings"
          toolbar={findingsToolbarSpec(t)}
        />
      ),
    'foreign-keys': () =>
      gaps.length === 0 ? (
        <EmptyState
          description={findingsPending ? t('indexes.panel.loading') : t('indexes.panel.fkNone')}
          title={t('indexes.panel.noMissingFk')}
        />
      ) : (
        <SimpleTable
          ariaLabel={t('indexes.panel.labelFkWithout')}
          captionId="indexes.foreign-keys-table"
          columns={gapColumns(t)}
          emptyState={null}
          rowKey={(gap) => gap.finding.finding_id}
          rows={gaps}
          scope={t('indexes.panel.scopeForeignKeys')}
          tableId="fk-gaps"
          toolbar={gapsToolbarSpec(t)}
        />
      ),
  }

  return (
    <Tabs onValueChange={setView} value={view}>
      <IndexPanel
        actions={
          <>
            <HelpLink anchor="indexes#inventory" label={t('indexes.panel.helpIndexesTab')} />
            {view === 'findings' ? (
              // From 1600 px "Open" leaves the line and stays in the link's name, so
              // the findings view's header line still fits at 1920 px (bug-HDR).
              <Link
                className="text-primary text-sm font-medium whitespace-nowrap hover:underline"
                data-testid="index-findings-link"
                to="/findings"
              >
                <span className="min-[1600px]:sr-only">{t('indexes.panel.open')}</span>{' '}
                {t('indexes.panel.findingsLink')}
                <ArrowRight
                  aria-hidden="true"
                  className="ml-0.5 inline size-3.5 align-[-0.125em]"
                />
              </Link>
            ) : null}
            {viewBadgeHint(t)[view]}
            <TabsList aria-label={t('indexes.panel.viewsLabel')} className="min-[1600px]:h-8!">
              {INDEX_VIEWS.map((option) => (
                <TabsTrigger key={option.id} className="min-[1600px]:py-0.5" value={option.id}>
                  {t(option.labelKey)}
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
