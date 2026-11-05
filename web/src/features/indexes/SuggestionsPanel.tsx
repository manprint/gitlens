import { useMemo, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { useIndexSuggestions } from '@/api/queries'
import { CopyButton } from '@/components/layout/CopyButton'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { NativeSelect } from '@/components/ui/native-select'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { formatBytes, formatCount } from '@/lib/format'
import {
  compareSuggestions,
  costText,
  coverageText,
  ESTIMATE_CALLOUT,
  estimateLabel,
  keyText,
  noteLabel,
  readSuggestionWindow,
  statementLink,
  statisticsMissing,
  SUGGESTION_LIMIT,
  SUGGESTION_WINDOWS,
  tableText,
  verdictLabel,
  verdictVariant,
  VERDICT_RULE,
  weightText,
  writesText,
  type IndexSuggestion,
  type SuggestionSortKey,
} from '@/lib/indexSuggestions'
import type { SortDirection } from '@/lib/tableControls'

import { CELL_BADGE, IndexPanel, PanelNote } from './IndexPanel'

const WINDOW_PARAM = 'window'

function compareRows(
  a: IndexSuggestion,
  b: IndexSuggestion,
  key: string,
  direction: SortDirection,
) {
  return compareSuggestions(a, b, key as SuggestionSortKey, direction)
}

const SUGGESTION_TOOLBAR: TableToolbarSpec<IndexSuggestion> = {
  searchLabel: 'Search suggestions',
  searchFields: [
    (row) => row.table,
    (row) => row.schema,
    (row) => keyText(row.columns),
    (row) => row.statements.map((statement) => statement.query_text).join('\n'),
  ],
}

// compareRows orders the rows: a column's sortValue only marks it sortable.
const SORTED_BY_COMPARE = () => null

function WeightBar({ weightPct }: { weightPct: number }) {
  const width = Math.max(0, Math.min(100, weightPct))
  return (
    <span className="inline-flex items-center gap-2">
      <span
        aria-hidden="true"
        className="bg-muted relative h-2 w-16 shrink-0 overflow-hidden rounded-full"
      >
        <span className="bg-primary absolute inset-y-0 left-0" style={{ width: `${width}%` }} />
      </span>
      <span className="tabular-nums">{weightText(weightPct)}</span>
    </span>
  )
}

function suggestionColumns(): SimpleColumn<IndexSuggestion>[] {
  const sortable = (
    id: SuggestionSortKey,
    header: string,
    cell: (row: IndexSuggestion) => ReactNode,
    width: string,
    extra: Partial<SimpleColumn<IndexSuggestion>> = {},
  ): SimpleColumn<IndexSuggestion> => ({
    id,
    header,
    sortValue: SORTED_BY_COMPARE,
    firstDirection: 'desc',
    className: 'whitespace-nowrap',
    width,
    cell,
    ...extra,
  })
  return [
    {
      id: 'table',
      header: 'Table',
      sortValue: SORTED_BY_COMPARE,
      className: 'break-all',
      truncate: tableText,
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
      id: 'key',
      header: 'Key columns',
      sortValue: SORTED_BY_COMPARE,
      className: 'break-all',
      truncate: (row) => keyText(row.columns),
      cell: (row) => <code className="font-mono text-xs">{keyText(row.columns)}</code>,
    },
    sortable('weight', 'Weight %', (row) => <WeightBar weightPct={row.weight_pct} />, '10rem'),
    sortable(
      'cost',
      'Read cost',
      (row) => (
        <span title="An upper bound of the time these statements spend today: an index cannot remove more than all of it.">
          {costText(row.read_cost_ms_per_s)}
          <span className="text-muted-foreground block text-xs min-[1600px]:ml-1.5 min-[1600px]:inline">
            upper bound
          </span>
        </span>
      ),
      '10rem',
    ),
    sortable(
      'size',
      'Est. size',
      (row) => (
        <span title="Estimated from the planner's row count and the column widths.">
          {estimateLabel(row.est_size_bytes)}
        </span>
      ),
      '7rem',
    ),
    sortable(
      'writes',
      'Writes/s',
      (row) => (
        <span title="Inserts, updates and deletes per second of the table, in that order.">
          {writesText(row.write)}
        </span>
      ),
      '13rem',
    ),
    sortable(
      'verdict',
      'Verdict',
      (row) => (
        <Badge className={CELL_BADGE} title={VERDICT_RULE} variant={verdictVariant(row.verdict)}>
          {verdictLabel(row.verdict)}
        </Badge>
      ),
      '8rem',
    ),
    sortable('statements', 'Statements', (row) => formatCount(row.statements.length), '7rem'),
  ]
}

function Detail({ row, instanceId }: { row: IndexSuggestion; instanceId: string }) {
  return (
    <div className="space-y-4 px-4 py-4">
      <div>
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-sm font-medium">Index to review</h3>
          <CopyButton subject="index statement" text={row.create_statement} />
        </div>
        <pre className="bg-muted/50 mt-1 overflow-x-auto rounded-lg border px-3 py-2 font-mono text-xs">
          <code>{row.create_statement}</code>
        </pre>
        <p className="text-muted-foreground mt-1 text-xs">
          pglens never runs this statement. Check it against your own workload first.
        </p>
      </div>

      <dl className="grid gap-x-8 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr_auto_1fr]">
        <dt className="text-muted-foreground">Selectivity</dt>
        <dd>
          {row.selectivity.label} ({row.selectivity.column}
          {row.selectivity.n_distinct === null
            ? ', n_distinct unknown'
            : `, n_distinct ${row.selectivity.n_distinct}`}
          )
        </dd>
        <dt className="text-muted-foreground">Read/write ratio</dt>
        <dd className="tabular-nums">{row.read_write_ratio.toFixed(1)}</dd>
        <dt className="text-muted-foreground">Calls</dt>
        <dd className="tabular-nums">{row.calls_rate.toFixed(2)}/s</dd>
        <dt className="text-muted-foreground">Confidence</dt>
        <dd>{row.confidence}</dd>
        <dt className="text-muted-foreground">Estimated size</dt>
        <dd className="tabular-nums">
          {row.est_size_bytes === null ? 'Unknown' : `~${formatBytes(row.est_size_bytes)}`}
        </dd>
        <dt className="text-muted-foreground">HOT updates</dt>
        <dd className="tabular-nums">{Math.round(row.write.hot_update_ratio * 100)}%</dd>
      </dl>

      {row.overlapping_indexes.length > 0 ? (
        <p className="text-sm">
          Overlaps existing indexes:{' '}
          {row.overlapping_indexes.map((name, index) => (
            <span key={name}>
              {index > 0 ? ', ' : ''}
              <code className="font-mono text-xs">{name}</code>
            </span>
          ))}
          . Check that the new one is not redundant.
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
        <h3 className="text-sm font-medium">Statements that use it</h3>
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
                  {statement.share_pct.toFixed(0)}% of this key&apos;s read cost
                </span>
              </div>
              <pre className="bg-muted/50 mt-1 overflow-x-auto rounded-lg border px-3 py-2 font-mono text-xs whitespace-pre-wrap">
                {statement.query_text}
              </pre>
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}

interface SuggestionsPanelProps {
  instanceId: string
  /** The database picked on the page; empty for all of them. */
  database: string
}

/**
 * Candidate indexes from the statements the agent tracks (D8): every figure is
 * an estimate and the tab only shows, never creates, an index (I-2, I-5).
 */
export function SuggestionsPanel({ instanceId, database }: SuggestionsPanelProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const span = readSuggestionWindow(searchParams.get(WINDOW_PARAM))
  const query = useIndexSuggestions(instanceId, {
    window: span,
    limit: SUGGESTION_LIMIT,
    ...(database === '' ? {} : { database }),
  })
  const response = query.data
  const columns = useMemo(() => suggestionColumns(), [])

  function setWindow(value: string) {
    const next = new URLSearchParams(searchParams)
    if (value === '24h') next.delete(WINDOW_PARAM)
    else next.set(WINDOW_PARAM, value)
    next.delete('suggestions.page')
    setSearchParams(next, { replace: true })
  }

  if (query.isPending && response === undefined) {
    return (
      <div aria-busy="true" aria-label="Loading suggestions" role="status">
        Loading suggestions…
      </div>
    )
  }
  if (query.error && response === undefined) {
    return (
      <ErrorState
        endpoint="index suggestions"
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (response === undefined) return null

  const coverageTitle = coverageText(response.coverage)
  const missing = statisticsMissing(response)
  const suggestions = response.suggestions
  const strong = suggestions.filter((row) => row.verdict === 'strong').length

  const windowControl = (
    <>
      <label className="sr-only" htmlFor="suggestion-window">
        Analysis window
      </label>
      <NativeSelect
        className="w-28 min-[1600px]:h-8"
        id="suggestion-window"
        onChange={(event) => setWindow(event.target.value)}
        value={span}
      >
        {SUGGESTION_WINDOWS.map((option) => (
          <option key={option} value={option}>
            Last {option}
          </option>
        ))}
      </NativeSelect>
      <InfoHint id="indexes.suggestions" label="Index suggestions" />
    </>
  )

  return (
    <IndexPanel
      actions={windowControl}
      description="Columns your statements filter or sort on that no index leads with, ranked by the share of statement time they involve."
      notes={
        <>
          <PanelNote title={ESTIMATE_CALLOUT}>{ESTIMATE_CALLOUT}</PanelNote>
          {response.notes.map((note) => (
            <PanelNote key={note} title={noteLabel(note)}>
              {noteLabel(note)}
            </PanelNote>
          ))}
        </>
      }
      stats={[
        { label: 'Suggestions', value: formatCount(suggestions.length) },
        { label: 'Strong', value: formatCount(strong) },
        {
          label: 'Statements analysed',
          value: formatCount(response.coverage.statements_analysed),
          unit: `of ${formatCount(response.coverage.statements_considered)}`,
        },
      ]}
      title="Index suggestions"
      titleId="index-suggestions-title"
    >
      <p
        className="text-muted-foreground border-b px-5 py-2 text-sm min-[1600px]:truncate min-[1600px]:py-0 min-[1600px]:text-xs"
        data-testid="suggestion-coverage"
        title={coverageTitle}
      >
        {coverageTitle}
      </p>
      {suggestions.length === 0 ? (
        missing ? (
          <EmptyState
            action={
              <Link
                className="text-primary text-sm underline-offset-2 hover:underline"
                to={`/instances/${encodeURIComponent(instanceId)}`}
              >
                Check the server&apos;s readiness
              </Link>
            }
            description="No statement ran in this window, or pg_stat_statements is not collected for this server."
            title="Statement statistics not collected"
          />
        ) : (
          <EmptyState
            description="Every column the analysed statements use is already indexed, or the tables are too small to benefit."
            title="No suggestions found"
          />
        )
      ) : (
        <SimpleTable
          ariaLabel="Index suggestions"
          columns={columns}
          compare={compareRows}
          defaultSort={{ id: 'weight', desc: true }}
          detailLabel={(row, expanded) =>
            `${expanded ? 'Hide' : 'Show'} details of ${row.table} (${keyText(row.columns)})`
          }
          emptyState={null}
          renderDetail={(row) => <Detail instanceId={instanceId} row={row} />}
          rowKey={(row) => row.id}
          rows={suggestions}
          scope="suggestions"
          tableId="suggestions"
          toolbar={SUGGESTION_TOOLBAR}
        />
      )}
    </IndexPanel>
  )
}
