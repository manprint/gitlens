import { useMemo, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { useIndexSuggestions, useInstanceTables } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { CopyButton } from '@/components/layout/CopyButton'
import { HelpLink } from '@/components/layout/HelpLink'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { NativeSelect } from '@/components/ui/native-select'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { useT, type Translate } from '@/i18n/react'
import { formatBytes, formatCount, formatDecimal, formatRate } from '@/lib/format'
import {
  costText,
  coverageText,
  estimateCallout,
  estimateLabel,
  noteLabel,
  readSuggestionWindow,
  statementLink,
  statisticsMissing,
  SUGGESTION_LIMIT,
  SUGGESTION_WINDOWS,
  verdictLabel,
  verdictVariant,
  verdictRule,
  weightText,
  type IndexSuggestion,
} from '@/lib/indexSuggestions'
import {
  buildMissingIndexes,
  missingIndexScript,
  RECOMMENDATION_HINT,
  RECOMMENDATION_LABEL,
  RECOMMENDATION_ORDER,
  SOURCE_HINT,
  SOURCE_LABEL,
  summariseMissing,
  type MissingIndex,
  type MissingSource,
  type Recommendation,
} from '@/lib/missingIndexes'
import type { SortDirection } from '@/lib/tableControls'

import { CELL_BADGE, IndexPanel, PanelNote } from './IndexPanel'

type Finding = Schemas['Finding']

const WINDOW_PARAM = 'window'
/** The tables read for the parent deletes and the full scans. */
const TABLE_LIMIT = 500

const RECOMMENDATION_VARIANT: Readonly<
  Record<Recommendation, 'warning' | 'secondary' | 'outline' | 'ghost'>
> = {
  create: 'warning',
  test: 'secondary',
  check: 'outline',
  skip: 'ghost',
}

const SOURCE_ORDER: readonly MissingSource[] = ['queries', 'foreign-keys', 'spatial', 'scans']

function missingToolbar(t: Translate): TableToolbarSpec<MissingIndex> {
  return {
    searchLabel: t('indexes.missing.searchMissing'),
    searchFields: [
      (row) => row.database,
      (row) => `${row.schema}.${row.table}`,
      (row) => row.columns,
      (row) => row.why,
      (row) => row.suggestion?.statements.map((statement) => statement.query_text).join('\n') ?? '',
    ],
    filters: [
      {
        id: 'recommendation',
        label: t('indexes.missing.recommendation'),
        kind: 'select',
        options: RECOMMENDATION_ORDER.map((value) => ({
          value,
          label: RECOMMENDATION_LABEL[value],
        })),
        accessor: (row) => row.recommendation,
      },
      {
        id: 'source',
        label: t('indexes.missing.foundBy'),
        kind: 'select',
        options: SOURCE_ORDER.map((value) => ({ value, label: SOURCE_LABEL[value] })),
        accessor: (row) => row.source,
      },
    ],
  }
}

function tableName(row: MissingIndex): string {
  return `${row.database} · ${row.schema}.${row.table}`
}

function compareRows(a: MissingIndex, b: MissingIndex, key: string, direction: SortDirection) {
  const sign = direction === 'asc' ? 1 : -1
  const byRecommendation =
    RECOMMENDATION_ORDER.indexOf(a.recommendation) - RECOMMENDATION_ORDER.indexOf(b.recommendation)
  const primary =
    key === 'source'
      ? SOURCE_ORDER.indexOf(a.source) - SOURCE_ORDER.indexOf(b.source)
      : key === 'table'
        ? tableName(a).localeCompare(tableName(b))
        : byRecommendation
  // Inside a group the strongest evidence stays first, whatever the direction.
  return sign * primary || byRecommendation || b.weight - a.weight || a.key.localeCompare(b.key)
}

// compareRows orders the rows: a column's sortValue only marks it sortable.
const SORTED_BY_COMPARE = () => null

function missingColumns(t: Translate): SimpleColumn<MissingIndex>[] {
  return [
    {
      id: 'recommendation',
      header: t('indexes.missing.recommendation'),
      help: 'indexes.missing-recommendation',
      sortValue: SORTED_BY_COMPARE,
      className: 'whitespace-nowrap',
      width: '9.5rem',
      cell: (row) => (
        <Badge
          className={CELL_BADGE}
          title={RECOMMENDATION_HINT[row.recommendation]}
          variant={RECOMMENDATION_VARIANT[row.recommendation]}
        >
          {RECOMMENDATION_LABEL[row.recommendation]}
        </Badge>
      ),
    },
    {
      id: 'source',
      header: t('indexes.missing.foundBy'),
      help: 'indexes.missing-source',
      sortValue: SORTED_BY_COMPARE,
      className: 'whitespace-nowrap',
      width: '9.5rem',
      truncate: (row) => SOURCE_LABEL[row.source],
      cell: (row) => <span title={SOURCE_HINT[row.source]}>{SOURCE_LABEL[row.source]}</span>,
    },
    {
      id: 'table',
      header: t('indexes.missing.table'),
      sortValue: SORTED_BY_COMPARE,
      className: 'break-all max-[1599px]:min-w-48',
      width: '18rem',
      truncate: tableName,
      cell: (row) => (
        <>
          <span className="text-muted-foreground">
            {row.database} · {row.schema}.
          </span>
          <span className="font-medium">{row.table}</span>
        </>
      ),
    },
    {
      id: 'columns',
      header: t('indexes.missing.columns'),
      help: 'indexes.missing-columns',
      className: 'break-all max-[1599px]:min-w-48',
      width: '12rem',
      truncate: (row) => row.columns,
      cell: (row) => <code className="font-mono text-xs">{row.columns}</code>,
    },
    {
      id: 'why',
      header: t('indexes.missing.why'),
      help: 'indexes.missing-why',
      truncate: (row) => row.why,
      cell: (row) => row.why,
    },
    {
      id: 'command',
      header: t('indexes.missing.command'),
      help: 'indexes.missing-command',
      className: 'whitespace-nowrap',
      width: '7rem',
      cell: (row) =>
        row.command === null ? (
          <span
            className="text-muted-foreground text-xs"
            title={t('indexes.missing.findColumnsHint')}
          >
            {t('indexes.missing.seeSteps')}
          </span>
        ) : (
          <CopyButton subject={t('indexes.missing.createStatement')} text={row.command} />
        ),
    },
  ]
}

function SqlBlock({ sql, subject }: { sql: string; subject: string }) {
  return (
    <div className="relative">
      <pre className="bg-muted/50 overflow-x-auto rounded-lg border px-3 py-2 pr-20 font-mono text-xs whitespace-pre-wrap">
        <code>{sql}</code>
      </pre>
      <div className="absolute top-1 right-1">
        <CopyButton subject={subject} text={sql} />
      </div>
    </div>
  )
}

type WriteKey =
  'indexes.missing.writeInserts' | 'indexes.missing.writeUpdates' | 'indexes.missing.writeDeletes'

function rateWords(t: Translate, rate: number, key: WriteKey): string {
  const figure = rate > 0 && rate < 0.01 ? '<0.01' : formatRate(rate).replace(/\/s$/, '')
  return t(key, { figure })
}

function confidenceText(t: Translate, confidence: IndexSuggestion['confidence']): string {
  if (confidence === 'high') return t('indexes.missing.confidenceHigh')
  return confidence === 'medium'
    ? t('indexes.missing.confidenceMedium')
    : t('indexes.missing.confidenceLow')
}

function stepStatement(t: Translate, title: string): string {
  return t('indexes.missing.stepStatement', { step: title.toLowerCase() })
}

/** The statement analysis behind a row found in the statements. */
function SuggestionEvidence({ row, instanceId }: { row: IndexSuggestion; instanceId: string }) {
  const t = useT()

  const write = row.write
  return (
    <section aria-label={t('indexes.missing.statementAnalysis')} className="space-y-3">
      <h3 className="text-sm font-medium">{t('indexes.missing.statementAnalysis')}</h3>
      <dl className="grid gap-x-8 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr_auto_1fr]">
        <dt className="text-muted-foreground">{t('indexes.missing.readCost')}</dt>
        <dd className="tabular-nums">
          {costText(row.read_cost_ms_per_s)}{' '}
          <span className="text-muted-foreground text-xs">{t('indexes.missing.readCostHint')}</span>
        </dd>
        <dt className="text-muted-foreground">{t('indexes.missing.weight')}</dt>
        <dd className="tabular-nums">
          {weightText(row.weight_pct)} {t('indexes.missing.ofStatementTime')}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.missing.verdict')}</dt>
        <dd className="flex items-center gap-1.5">
          <Badge className={CELL_BADGE} title={verdictRule()} variant={verdictVariant(row.verdict)}>
            {verdictLabel(row.verdict)}
          </Badge>
          <InfoHint id="indexes.verdict" label={t('indexes.missing.verdictHint')} />
        </dd>
        <dt className="text-muted-foreground">{t('indexes.missing.confidence')}</dt>
        <dd>{confidenceText(t, row.confidence)}</dd>
        <dt className="text-muted-foreground">{t('indexes.missing.calls')}</dt>
        <dd className="tabular-nums">
          {formatDecimal(row.calls_rate, 2)}
          {t('indexes.missing.perSecond')}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.missing.readWriteRatio')}</dt>
        <dd className="tabular-nums">{formatDecimal(row.read_write_ratio, 1)}</dd>
        <dt className="text-muted-foreground">{t('indexes.missing.estimatedSize')}</dt>
        <dd className="tabular-nums">{estimateLabel(row.est_size_bytes)}</dd>
        <dt className="text-muted-foreground">{t('indexes.missing.tableWrites')}</dt>
        <dd className="tabular-nums">
          {[
            rateWords(t, write.insert_rate, 'indexes.missing.writeInserts'),
            rateWords(t, write.update_rate, 'indexes.missing.writeUpdates'),
            rateWords(t, write.delete_rate, 'indexes.missing.writeDeletes'),
          ].join(' · ')}
        </dd>
        <dt className="text-muted-foreground">{t('indexes.missing.selectivity')}</dt>
        <dd>
          {row.selectivity.label} ({row.selectivity.column}
          {row.selectivity.n_distinct === null
            ? t('indexes.missing.nDistinctUnknown')
            : t('indexes.missing.nDistinct', { value: String(row.selectivity.n_distinct) })}
          )
        </dd>
        <dt className="text-muted-foreground">{t('indexes.missing.hotUpdates')}</dt>
        <dd className="tabular-nums">{Math.round(write.hot_update_ratio * 100)}%</dd>
      </dl>

      {row.overlapping_indexes.length > 0 ? (
        <p className="text-sm">
          {t('indexes.missing.overlapsIndexes')}{' '}
          {row.overlapping_indexes.map((name, index) => (
            <span key={name}>
              {index > 0 ? ', ' : ''}
              <code className="font-mono text-xs">{name}</code>
            </span>
          ))}
          {t('indexes.missing.overlapsCheck')}
        </p>
      ) : null}

      {row.notes.length > 0 ? (
        <ul className="space-y-1 text-sm">
          {row.notes.map((note) => (
            <li key={note} className="border-warning/40 bg-warning/10 rounded-lg border px-3 py-2">
              {noteLabel(note)}
            </li>
          ))}
        </ul>
      ) : null}

      <div>
        <h4 className="text-sm font-medium">{t('indexes.missing.statementsUsing')}</h4>
        <ul className="mt-1 space-y-2">
          {row.statements.map((statement) => (
            <li key={statement.queryid} className="text-sm">
              <div className="flex flex-wrap items-center gap-x-3">
                <Link
                  className="text-primary font-mono text-xs underline-offset-2 hover:underline"
                  to={statementLink(instanceId, statement.queryid)}
                >
                  {statement.queryid}
                </Link>
                <span className="text-muted-foreground text-xs tabular-nums">
                  {formatDecimal(statement.share_pct, 0)}
                  {t('indexes.missing.shareOfKeyCost')}
                </span>
              </div>
              <pre className="bg-muted/50 mt-1 overflow-x-auto rounded-lg border px-3 py-2 font-mono text-xs whitespace-pre-wrap">
                {statement.query_text}
              </pre>
            </li>
          ))}
        </ul>
      </div>
    </section>
  )
}

function Detail({ row, instanceId }: { row: MissingIndex; instanceId: string }) {
  const t = useT()

  return (
    <div className="space-y-4 px-4 py-4">
      <div className="space-y-1 text-sm">
        <p>
          <span className="font-medium">{RECOMMENDATION_LABEL[row.recommendation]}:</span>{' '}
          {RECOMMENDATION_HINT[row.recommendation]}
        </p>
        <p>
          <span className="font-medium">
            {t('indexes.missing.foundBy')} {SOURCE_LABEL[row.source].toLowerCase()}:
          </span>{' '}
          {SOURCE_HINT[row.source]}
        </p>
        <p>{row.why}</p>
        {row.tableBytes === null ? null : (
          <p className="text-muted-foreground text-xs">
            {t('indexes.missing.tableSize')} {formatBytes(row.tableBytes)}
          </p>
        )}
      </div>

      <section aria-label={t('indexes.missing.whatToDo')} className="space-y-3">
        <div className="flex items-center gap-3">
          <h3 className="text-sm font-medium">{t('indexes.missing.whatToDo')}</h3>
          <HelpLink anchor="indexes#missing-act" label={t('indexes.missing.howToAct')} />
        </div>
        <ol className="space-y-3">
          {row.steps.map((step, index) => (
            <li key={step.title} className="space-y-1">
              <p className="text-sm">
                <span className="font-medium">
                  {index + 1}. {step.title}.
                </span>{' '}
                {step.text}
              </p>
              {step.sql === undefined ? null : (
                <SqlBlock sql={step.sql} subject={stepStatement(t, step.title)} />
              )}
            </li>
          ))}
        </ol>
        <p className="text-muted-foreground text-xs">{t('indexes.missing.neverRuns')}</p>
      </section>

      {row.suggestion === undefined ? null : (
        <SuggestionEvidence instanceId={instanceId} row={row.suggestion} />
      )}
      {row.finding === undefined ? null : (
        <p className="text-muted-foreground text-xs">
          {t('indexes.missing.advisorFinding')}{' '}
          <code className="font-mono">{row.finding.rule_id}</code>
          {row.finding.detail
            ? t('indexes.missing.findingDetail', { detail: row.finding.detail })
            : ''}
        </p>
      )}
    </div>
  )
}

interface MissingIndexesPanelProps {
  instanceId: string
  /** The server name, for the script header. */
  serverLabel: string
  /** The database picked on the page; empty for all of them. */
  database: string
  /** The findings of the server (the page loads them once). */
  findings: readonly Finding[]
  findingsPending: boolean
}

/**
 * Where an index is missing, why, and the statement to create it: the
 * statements' suggestions, the advisor's unindexed foreign keys and spatial
 * columns, and the tables read by full scans, in one list. pglens never runs
 * any of it (I-2, I-5).
 */
export function MissingIndexesPanel({
  instanceId,
  serverLabel,
  database,
  findings,
  findingsPending,
}: MissingIndexesPanelProps) {
  const t = useT()

  const [searchParams, setSearchParams] = useSearchParams()
  const span = readSuggestionWindow(searchParams.get(WINDOW_PARAM))
  const query = useIndexSuggestions(instanceId, {
    window: span,
    limit: SUGGESTION_LIMIT,
    ...(database === '' ? {} : { database }),
  })
  const bySize = useInstanceTables(instanceId, { limit: TABLE_LIMIT, order_by: 'size' })
  const byScans = useInstanceTables(instanceId, { limit: TABLE_LIMIT, order_by: 'seq_scan' })
  const response = query.data
  const columns = useMemo(() => missingColumns(t), [t])
  const toolbar = useMemo(() => missingToolbar(t), [t])

  const rows = useMemo(() => {
    const seen = new Set<string>()
    const tables = [...(bySize.data?.items ?? []), ...(byScans.data?.items ?? [])].filter(
      (item) => {
        const key = `${String(item.datname)}.${String(item.schemaname)}.${String(item.relname)}`
        if (seen.has(key)) return false
        seen.add(key)
        return true
      },
    )
    return buildMissingIndexes({
      suggestions: response?.suggestions ?? [],
      findings,
      tables,
      database,
    })
  }, [response, findings, bySize.data, byScans.data, database])

  function setWindow(value: string) {
    const next = new URLSearchParams(searchParams)
    if (value === '24h') next.delete(WINDOW_PARAM)
    else next.set(WINDOW_PARAM, value)
    next.delete('missing.page')
    setSearchParams(next, { replace: true })
  }

  if (query.isPending && response === undefined) {
    return (
      <div aria-busy="true" aria-label={t('indexes.missing.loadingMissingLabel')} role="status">
        {t('indexes.missing.loadingMissing')}
      </div>
    )
  }
  if (query.error && response === undefined) {
    return (
      <ErrorState
        endpoint={t('indexes.missing.endpointSuggestions')}
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (response === undefined) return null

  const summary = summariseMissing(rows)
  const coverageTitle = coverageText(response.coverage)
  const statementsMissing = statisticsMissing(response)
  const tablesFailed = bySize.error !== null || byScans.error !== null
  const script = missingIndexScript(rows, serverLabel)

  const actions: ReactNode = (
    <>
      <label className="sr-only" htmlFor="suggestion-window">
        {t('indexes.missing.analysisWindow')}
      </label>
      <NativeSelect
        className="w-28 min-[1600px]:h-8"
        id="suggestion-window"
        onChange={(event) => setWindow(event.target.value)}
        title={t('indexes.missing.windowHint')}
        value={span}
      >
        {SUGGESTION_WINDOWS.map((option) => (
          <option key={option} value={option}>
            {t('indexes.missing.windowOption', { window: option })}
          </option>
        ))}
      </NativeSelect>
      <span className="inline-flex items-center gap-1 text-xs font-medium whitespace-nowrap">
        {t('indexes.missing.script')}
        <CopyButton subject={t('indexes.missing.missingScript')} text={script} />
      </span>
      <HelpLink anchor="indexes#missing" label={t('indexes.missing.missingIndexes')} />
    </>
  )

  return (
    <IndexPanel
      actions={actions}
      captionId="indexes.missing"
      description={t('indexes.missing.panelDescription')}
      notes={
        <>
          <PanelNote title={estimateCallout()}>{estimateCallout()}</PanelNote>
          {response.notes.map((note) => (
            <PanelNote key={note} title={noteLabel(note)}>
              {noteLabel(note)}
            </PanelNote>
          ))}
          {tablesFailed ? (
            <PanelNote title={t('indexes.missing.tableStatsFailedHint')} tone="warning">
              {t('indexes.missing.tableStatsFailed')}
            </PanelNote>
          ) : null}
        </>
      }
      stats={[
        {
          label: t('indexes.missing.statCreate'),
          value: formatCount(summary.byRecommendation.create),
          unit: 'indexes',
          detail: t('indexes.missing.strongEvidence'),
        },
        {
          label: t('indexes.missing.statTestFirst'),
          value: formatCount(summary.byRecommendation.test),
          unit: 'indexes',
          detail: t('indexes.missing.likelyToHelp'),
        },
        {
          label: t('indexes.missing.statCheckWorkload'),
          value: formatCount(summary.byRecommendation.check),
          unit: 'indexes',
          detail: t('indexes.missing.ofListed', { total: formatCount(summary.total) }),
        },
      ]}
      title={t('indexes.missing.missingIndexes')}
      titleId="missing-indexes-title"
    >
      <p
        className="text-muted-foreground border-b px-5 py-2 text-sm min-[1600px]:py-1 min-[1600px]:text-xs"
        data-testid="suggestion-coverage"
        title={coverageTitle}
      >
        {t('indexes.missing.statementsLabel')} {coverageTitle}
        {findingsPending ? t('indexes.missing.loadingForeignKeys') : ''}
      </p>
      {rows.length === 0 ? (
        statementsMissing ? (
          <EmptyState
            action={
              <Link
                className="text-primary text-sm underline-offset-2 hover:underline"
                to={`/instances/${encodeURIComponent(instanceId)}`}
              >
                {t('indexes.missing.checkReadiness')}
              </Link>
            }
            description={t('indexes.missing.noStatementsDesc')}
            title={t('indexes.missing.statementStatsMissing')}
          />
        ) : (
          <EmptyState
            description={t('indexes.missing.noMissingDesc')}
            title={t('indexes.missing.noMissingTitle')}
          />
        )
      ) : (
        <SimpleTable
          ariaLabel={t('indexes.missing.missingCandidates')}
          captionId="indexes.missing-table"
          columns={columns}
          compare={compareRows}
          defaultSort={{ id: 'recommendation', desc: false }}
          detailLabel={(row, expanded) =>
            expanded
              ? t('indexes.missing.hideDetails', { table: row.table, columns: row.columns })
              : t('indexes.missing.showDetails', { table: row.table, columns: row.columns })
          }
          emptyState={null}
          renderDetail={(row) => <Detail instanceId={instanceId} row={row} />}
          rowKey={(row) => row.key}
          rows={rows}
          scope="missing indexes"
          tableId="missing"
          toolbar={toolbar}
        />
      )}
    </IndexPanel>
  )
}
