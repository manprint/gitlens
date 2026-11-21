import { useMemo, useState } from 'react'

import { usePlanStats, usePlans } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { HelpLink } from '@/components/layout/HelpLink'
import { InfoHint } from '@/components/layout/InfoHint'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { Degraded } from '@/components/state/Degraded'
import { ErrorState } from '@/components/state/ErrorState'
import { Badge } from '@/components/ui/badge'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { type Translate, useT } from '@/i18n/react'
import { formatCount, formatDecimal, formatMilliseconds, formatTimestamp } from '@/lib/format'
import {
  changeRatio,
  diffPlans,
  parsePlan,
  type NodeChange,
  type ParsedPlan,
  type PlanDiffRow,
  type PlanNodeView,
} from '@/lib/planTree'

import { PlanStatsCompare } from './PlanCompare'
import { AutoPlanSetup, PlanSamples } from './PlanSamples'
import { PlanStats } from './PlanStats'
import { PlanView } from './PlanView'

type Plan = Schemas['Plan']

interface PlanHistoryProps {
  queryid: number | string
  instanceId?: string
  datname?: string
}

interface Capture {
  key: string
  plan: Plan
  parsed: ParsedPlan | null
  /** Short form of the shape hash, the same for every capture of one shape. */
  shape: string
}

function planKey(plan: Plan): string {
  return String(plan.plan_id)
}

function sortPlans(plans: Plan[]): Plan[] {
  return [...plans].sort((left, right) => {
    const capturedDifference = Date.parse(right.captured_at) - Date.parse(left.captured_at)
    if (capturedDifference !== 0) return capturedDifference
    return String(right.plan_id).localeCompare(String(left.plan_id), undefined, { numeric: true })
  })
}

function kindOf(plan: Plan): string {
  return plan.analyzed ? 'ANALYZE' : 'EXPLAIN'
}

function topOperation(parsed: ParsedPlan | null, t: Translate): string {
  const root = parsed?.nodes[0]
  if (!root) return '—'
  return root.object
    ? t('queries.planHistory.operationOn', { title: root.title, object: root.object })
    : root.title
}

function captureLabel(capture: Capture, t: Translate): string {
  return t('queries.planHistory.captureLabel', {
    time: formatTimestamp(capture.plan.captured_at),
    kind: kindOf(capture.plan),
    shape: capture.shape,
  })
}

function captureToolbar(t: Translate): TableToolbarSpec<Capture> {
  return {
    searchFields: [(row) => row.shape, (row) => topOperation(row.parsed, t)],
    filters: [
      {
        id: 'kind',
        label: t('queries.common.kind'),
        kind: 'select',
        options: [
          { value: 'EXPLAIN', label: 'EXPLAIN' },
          { value: 'ANALYZE', label: 'ANALYZE' },
        ],
        accessor: (row) => kindOf(row.plan),
      },
    ],
  }
}

function ratioText(left: number | null, right: number | null): string | null {
  const ratio = changeRatio(left, right)
  if (ratio === null || Math.abs(ratio - 1) < 0.1) return null
  return ratio > 1 ? `+${Math.round((ratio - 1) * 100)}%` : `−${Math.round((1 - ratio) * 100)}%`
}

function Figure({
  left,
  right,
  format,
}: {
  left: number | null
  right: number | null
  format: (value: number) => string
}) {
  if (left === null && right === null) return <span className="text-muted-foreground">—</span>
  const delta = ratioText(left, right)
  return (
    <span className="whitespace-nowrap tabular-nums">
      {left === null ? '—' : format(left)} → {right === null ? '—' : format(right)}
      {delta && (
        <span
          className={`ml-1 text-xs font-medium ${delta.startsWith('+') ? 'text-destructive' : 'text-success'}`}
        >
          {delta}
        </span>
      )}
    </span>
  )
}

const CHANGE_KEY = {
  same: 'queries.planHistory.changeSame',
  changed: 'queries.planHistory.changeChanged',
  added: 'queries.planHistory.changeAdded',
  removed: 'queries.planHistory.changeRemoved',
} as const satisfies Record<NodeChange, string>

const CHANGE_STYLE: Record<NodeChange, string> = {
  same: 'text-muted-foreground',
  changed: 'bg-warning/20',
  added: 'bg-success/15 text-success',
  removed: 'bg-destructive/10 text-destructive',
}

function operationText(node: PlanNodeView | undefined, t: Translate): string {
  if (!node) return '—'
  return node.object
    ? t('queries.planHistory.operationOn', { title: node.title, object: node.object })
    : node.title
}

function comparisonToolbar(t: Translate): TableToolbarSpec<PlanDiffRow> {
  return {
    searchFields: [(row) => operationText(row.left, t), (row) => operationText(row.right, t)],
    filters: [
      {
        id: 'difference',
        label: t('queries.planHistory.difference'),
        kind: 'select',
        options: [
          { value: 'differs', label: t('queries.planHistory.differs') },
          { value: 'same', label: t('queries.planHistory.changeSame') },
        ],
        accessor: (row) => (row.change === 'same' ? 'same' : 'differs'),
      },
    ],
  }
}

/** One side of a plan comparison: its label, parsed plan and shape hash. */
export interface ComparedPlan {
  label: string
  parsed: ParsedPlan | null
  hash: string
}

function comparedCapture(capture: Capture, t: Translate): ComparedPlan {
  return { label: captureLabel(capture, t), parsed: capture.parsed, hash: capture.plan.plan_hash }
}

/**
 * Two plans node by node. The plan history compares an older capture with a
 * newer one; the tuning workbook compares two variants under its own headings.
 */
export function PlanComparison({
  older,
  newer,
  olderHeading,
  newerHeading,
}: {
  older: ComparedPlan
  newer: ComparedPlan
  olderHeading?: string
  newerHeading?: string
}) {
  const t = useT()
  const toolbar = useMemo(() => comparisonToolbar(t), [t])
  const left = older.parsed
  const right = newer.parsed
  const rows = useMemo(() => (left && right ? diffPlans(left, right) : []), [left, right])
  const bothAnalyzed = Boolean(left?.analyzed && right?.analyzed)
  const columns = useMemo<SimpleColumn<PlanDiffRow>[]>(() => {
    const list: SimpleColumn<PlanDiffRow>[] = [
      {
        id: 'operation',
        header: t('queries.common.operation'),
        rowHeader: true,
        className: 'font-normal',
        cell: (row) => {
          const changedText =
            row.change === 'changed' && operationText(row.left, t) !== operationText(row.right, t)
          return (
            <div style={{ paddingLeft: `${row.depth * 16}px` }}>
              {changedText ? (
                <>
                  <span className="text-muted-foreground line-through">
                    {operationText(row.left, t)}
                  </span>
                  <span className="block font-medium">{operationText(row.right, t)}</span>
                </>
              ) : (
                <span className="font-medium">{operationText(row.right ?? row.left, t)}</span>
              )}
            </div>
          )
        },
      },
      {
        id: 'change',
        header: t('queries.planHistory.change'),
        sortValue: (row) => t(CHANGE_KEY[row.change]),
        cell: (row) => (
          <span
            className={`rounded px-1.5 py-0.5 text-xs font-medium ${CHANGE_STYLE[row.change]}`}
            data-different={row.change === 'same' ? undefined : 'true'}
            data-testid={row.change === 'same' ? undefined : 'plan-diff'}
          >
            {t(CHANGE_KEY[row.change])}
          </span>
        ),
      },
      {
        id: 'cost',
        header: t('queries.common.totalCost'),
        sortValue: (row) => row.right?.totalCost ?? row.left?.totalCost ?? null,
        cell: (row) => (
          <Figure
            left={row.left?.totalCost ?? null}
            right={row.right?.totalCost ?? null}
            format={(value) => formatDecimal(value, 2)}
          />
        ),
      },
      {
        id: 'rows',
        header: bothAnalyzed ? t('queries.common.rows') : t('queries.common.rowsEstimated'),
        cell: (row) => (
          <Figure
            left={(bothAnalyzed ? row.left?.actualRows : row.left?.estimatedRows) ?? null}
            right={(bothAnalyzed ? row.right?.actualRows : row.right?.estimatedRows) ?? null}
            format={(value) => formatCount(Math.round(value))}
          />
        ),
      },
    ]
    if (bothAnalyzed) {
      list.push({
        id: 'time',
        header: t('queries.common.timeInNode'),
        cell: (row) => (
          <Figure
            left={row.left?.exclusiveMs ?? null}
            right={row.right?.exclusiveMs ?? null}
            format={(value) => formatMilliseconds(value)}
          />
        ),
      })
    }
    return list
  }, [bothAnalyzed, t])

  const changed = rows.filter((row) => row.change !== 'same').length
  const sameShape = older.hash === newer.hash

  return (
    <section aria-labelledby="plan-comparison-title" className="space-y-3">
      <div className="flex items-center gap-1">
        <h3 id="plan-comparison-title" className="font-medium">
          {t('queries.planHistory.comparisonTitle')}
        </h3>
        <InfoHint id="queries.plan-comparison" label={t('queries.planHistory.comparisonTitle')} />
      </div>
      <dl
        aria-label={t('queries.planHistory.comparedCaptures')}
        className="grid grid-cols-[auto_1fr] gap-x-3 text-sm"
      >
        <dt className="text-muted-foreground">{olderHeading ?? t('queries.planHistory.older')}</dt>
        <dd>{older.label}</dd>
        <dt className="text-muted-foreground">{newerHeading ?? t('queries.planHistory.newer')}</dt>
        <dd>{newer.label}</dd>
      </dl>
      <p role="note" className="text-sm font-medium">
        {sameShape
          ? t('queries.planHistory.shapeSame')
          : t('queries.planHistory.shapeDiffers', { changed })}
      </p>
      <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-3">
        <div>
          <dt className="text-muted-foreground text-xs">{t('queries.common.totalCost')}</dt>
          <dd>
            <Figure
              left={left?.totalCost ?? null}
              right={right?.totalCost ?? null}
              format={(value) => formatDecimal(value, 2)}
            />
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground text-xs">{t('queries.common.executionTime')}</dt>
          <dd>
            <Figure
              left={left?.executionMs ?? null}
              right={right?.executionMs ?? null}
              format={(value) => formatMilliseconds(value)}
            />
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground text-xs">{t('queries.common.rows')}</dt>
          <dd>
            <Figure
              left={left?.rows ?? null}
              right={right?.rows ?? null}
              format={(value) => formatCount(Math.round(value))}
            />
          </dd>
        </div>
      </dl>
      {rows.length === 0 ? (
        <p className="text-muted-foreground text-sm">{t('queries.planHistory.noOperations')}</p>
      ) : (
        <SimpleTable
          ariaLabel={t('queries.planHistory.comparisonTitle')}
          captionId="queries.plan-comparison"
          caption={t('queries.planHistory.comparisonCaption')}
          columns={columns}
          emptyState={null}
          rowKey={(row) => row.id}
          rows={rows}
          scope={t('queries.planHistory.scopeOperations')}
          tableId="plan-comparison"
          toolbar={toolbar}
        />
      )}
    </section>
  )
}

export function PlanHistory({ queryid, instanceId, datname }: PlanHistoryProps) {
  const t = useT()
  const toolbar = useMemo(() => captureToolbar(t), [t])
  const params = {
    queryid,
    ...(instanceId === undefined ? {} : { instance_id: instanceId }),
    ...(datname === undefined ? {} : { datname }),
    limit: 20,
  }
  const plansQuery = usePlans(params)
  const statsQuery = usePlanStats(
    {
      instance_id: instanceId ?? '',
      queryid: String(queryid),
      ...(datname === undefined ? {} : { database: datname }),
    },
    { enabled: instanceId !== undefined },
  )
  const stats = statsQuery.data as Schemas['PlanStats'] | undefined
  const response = plansQuery.data as Schemas['PlansResponse'] | undefined
  const captures = useMemo<Capture[]>(
    () =>
      sortPlans(response?.plans ?? []).map((plan) => ({
        key: planKey(plan),
        plan,
        parsed: parsePlan(plan.plan),
        shape: plan.plan_hash.slice(0, 8),
      })),
    [response?.plans],
  )
  const [selectedKeys, setSelectedKeys] = useState<string[]>([])
  const selected = captures.filter((capture) => selectedKeys.includes(capture.key))

  const toggle = (capture: Capture) => {
    setSelectedKeys((current) => {
      if (current.includes(capture.key)) return current.filter((key) => key !== capture.key)
      return [...current, capture.key].slice(-2)
    })
  }
  const selectedHashes = selected.map((capture) => capture.plan.plan_hash)
  const captureOf = (hash: string) => captures.find((capture) => capture.plan.plan_hash === hash)
  const toggleHash = (hash: string) => {
    if (selectedHashes.includes(hash)) {
      setSelectedKeys((current) =>
        current.filter((key) => captures.find((c) => c.key === key)?.plan.plan_hash !== hash),
      )
      return
    }
    const capture = captureOf(hash)
    if (capture) toggle(capture)
  }

  const columns: SimpleColumn<Capture>[] = [
    {
      id: 'select',
      header: t('queries.common.select'),
      srOnlyHeader: true,
      cell: (row) => (
        <input
          type="checkbox"
          aria-label={t('queries.planHistory.selectCaptured', {
            time: formatTimestamp(row.plan.captured_at),
          })}
          checked={selectedKeys.includes(row.key)}
          onChange={() => toggle(row)}
        />
      ),
    },
    {
      id: 'captured',
      header: t('queries.planHistory.captured'),
      rowHeader: true,
      sortValue: (row) => Date.parse(row.plan.captured_at),
      cell: (row) => formatTimestamp(row.plan.captured_at),
    },
    {
      id: 'kind',
      header: t('queries.common.kind'),
      sortValue: (row) => kindOf(row.plan),
      cell: (row) => <Badge variant="outline">{kindOf(row.plan)}</Badge>,
    },
    {
      id: 'source',
      header: t('queries.planHistory.source'),
      sortValue: (row) => row.plan.source ?? 'command',
      cell: (row) => (
        <Badge variant={row.plan.source === 'auto_explain' ? 'secondary' : 'outline'}>
          {row.plan.source === 'auto_explain'
            ? t('queries.planHistory.sourceAuto')
            : t('queries.planHistory.sourceCommand')}
        </Badge>
      ),
    },
    {
      id: 'shape',
      header: t('queries.planHistory.shape'),
      sortValue: (row) => row.shape,
      cell: (row) => (
        <span className="flex items-center gap-1.5">
          <code className="font-mono text-xs" title={row.plan.plan_hash}>
            {row.shape}
          </code>
          {row.plan.changed && (
            <span className="bg-warning/20 rounded px-1.5 text-xs font-medium">
              {t('queries.planHistory.newShape')}
            </span>
          )}
        </span>
      ),
    },
    {
      id: 'cost',
      header: t('queries.common.totalCost'),
      sortValue: (row) => row.parsed?.totalCost ?? null,
      cell: (row) => (row.parsed?.totalCost == null ? '—' : formatDecimal(row.parsed.totalCost, 2)),
    },
    {
      id: 'time',
      header: t('queries.common.executionTime'),
      sortValue: (row) => row.parsed?.executionMs ?? null,
      cell: (row) =>
        row.parsed?.executionMs === null || row.parsed?.executionMs === undefined
          ? '—'
          : formatMilliseconds(row.parsed.executionMs),
    },
    {
      id: 'rows',
      header: t('queries.common.rows'),
      sortValue: (row) => row.parsed?.rows ?? null,
      cell: (row) =>
        row.parsed?.rows === null || row.parsed?.rows === undefined
          ? '—'
          : row.parsed.analyzed
            ? formatCount(Math.round(row.parsed.rows))
            : t('queries.planHistory.rowsEstimated', {
                rows: formatCount(Math.round(row.parsed.rows)),
              }),
    },
    {
      id: 'top',
      header: t('queries.planHistory.topOperation'),
      cell: (row) => topOperation(row.parsed, t),
    },
  ]

  if (plansQuery.isPending && response === undefined) {
    return <div role="status">{t('queries.planHistory.loading')}</div>
  }

  if (plansQuery.error && response === undefined) {
    if (plansQuery.error.kind === 'not_found') {
      return <Degraded reason={t('queries.planHistory.evicted', { queryid: String(queryid) })} />
    }
    return (
      <ErrorState
        endpoint={t('queries.planHistory.endpoint')}
        failure={plansQuery.error}
        onRetry={() => void plansQuery.refetch()}
      />
    )
  }

  const shapes =
    response?.total_shapes ?? new Set(captures.map((capture) => capture.plan.plan_hash)).size
  const [first, second] = selected
  const pair =
    first && second
      ? Date.parse(first.plan.captured_at) <= Date.parse(second.plan.captured_at)
        ? { older: first, newer: second }
        : { older: second, newer: first }
      : null

  const statsBlock = stats ? (
    <PlanStats
      stats={stats}
      selectedHashes={selectedHashes}
      canTick={(hash) => captureOf(hash) !== undefined}
      onToggle={toggleHash}
    />
  ) : null
  const olderStats = pair && stats?.plans.find((p) => p.plan_hash === pair.older.plan.plan_hash)
  const newerStats = pair && stats?.plans.find((p) => p.plan_hash === pair.newer.plan.plan_hash)

  return (
    <section
      aria-labelledby="plan-history-title"
      className="bg-card shadow-card space-y-4 rounded-xl border p-5"
      data-card=""
      data-caption-id="queries.plan-history"
    >
      <div>
        <div className="flex items-center gap-1">
          <h2 id="plan-history-title" className="text-base font-semibold">
            {t('queries.planHistory.title')}
          </h2>
          <InfoHint id="queries.plan-history" label={t('queries.planHistory.title')} />
          <HelpLink
            anchor="query-performance#plan-history"
            label={t('queries.planHistory.title')}
          />
        </div>
        <p className="text-muted-foreground mt-1 text-sm">{t('queries.planHistory.intro')}</p>
      </div>

      {captures.length === 0 ? (
        <div className="space-y-2">
          <p role="status" className="text-muted-foreground text-sm">
            {t('queries.planHistory.noHistory')}
          </p>
          <AutoPlanSetup />
          {statsBlock}
        </div>
      ) : (
        <>
          <p className="text-sm">
            {t('queries.planHistory.summary', {
              shown: formatCount(captures.length),
              captureCount: captures.length,
              shownShapes: formatCount(shapes),
              shapeCount: shapes,
            })}
          </p>
          {statsBlock}
          <SimpleTable
            ariaLabel={t('queries.planHistory.entriesLabel')}
            captionId="queries.plan-captures"
            caption={t('queries.planHistory.capturedCaption')}
            columns={columns}
            emptyState={null}
            rowKey={(row) => row.key}
            rows={captures}
            scope={t('queries.planHistory.scopePlans')}
            tableId="plan-history"
            toolbar={toolbar}
          />

          {selected.length === 0 && (
            <p role="status" className="text-muted-foreground text-sm">
              {t('queries.planHistory.selectCapture')}
            </p>
          )}
          {selected.length === 1 &&
            first &&
            (first.parsed ? (
              <PlanView
                plan={first.parsed}
                title={t('queries.planHistory.planCaptured', {
                  time: formatTimestamp(first.plan.captured_at),
                })}
              />
            ) : (
              <p className="text-muted-foreground text-sm">
                {t('queries.planHistory.noOperationsCapture')}
              </p>
            ))}
          {selected.length === 1 && first && instanceId !== undefined && (
            <PlanSamples
              instanceId={instanceId}
              queryid={String(queryid)}
              planHash={first.plan.plan_hash}
              {...(datname === undefined ? {} : { datname })}
            />
          )}
          {pair && (
            <PlanComparison
              older={comparedCapture(pair.older, t)}
              newer={comparedCapture(pair.newer, t)}
            />
          )}
          {olderStats && newerStats && olderStats !== newerStats && (
            <PlanStatsCompare older={olderStats} newer={newerStats} />
          )}
        </>
      )}
    </section>
  )
}

export default PlanHistory
