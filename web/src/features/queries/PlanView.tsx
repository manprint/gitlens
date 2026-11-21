import { AlertTriangle, ChevronDown, ChevronRight, Info, Siren } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { CopyButton } from '@/components/layout/CopyButton'
import { InfoHint } from '@/components/layout/InfoHint'
import { useT } from '@/i18n/react'
import { formatCount, formatDecimal, formatMilliseconds } from '@/lib/format'
import {
  bufferBytes,
  formatFactor,
  planText,
  type InsightSeverity,
  type ParsedPlan,
  type PlanNodeView,
  type PlanValuesFrom,
  valuesFromLabel,
} from '@/lib/planTree'

const pct = new Intl.NumberFormat('en-US', { style: 'percent', maximumFractionDigits: 0 })

type View = 'tree' | 'text' | 'json'

function Summary({ plan, valuesFrom }: { plan: ParsedPlan; valuesFrom: PlanValuesFrom | null }) {
  const t = useT()
  const items: [string, string][] = [
    [
      t('queries.planView.summaryPlan'),
      plan.analyzed ? 'EXPLAIN ANALYZE' : t('queries.planView.explainEstimates'),
    ],
  ]
  if (valuesFrom) items.push([t('queries.planView.parameters'), valuesFromLabel(valuesFrom)])
  if (plan.executionMs !== null)
    items.push([t('queries.common.executionTime'), formatMilliseconds(plan.executionMs)])
  if (plan.planningMs !== null)
    items.push([t('queries.planView.planningTime'), formatMilliseconds(plan.planningMs)])
  items.push([
    plan.analyzed ? t('queries.planView.rowsReturned') : t('queries.planView.rowsExpected'),
    plan.rows === null ? '—' : formatCount(Math.round(plan.rows)),
  ])
  if (plan.totalCost !== null)
    items.push([t('queries.common.totalCost'), formatDecimal(plan.totalCost, 2)])
  if (plan.sharedHit !== null || plan.sharedRead !== null) {
    items.push([
      t('queries.planView.buffers'),
      t('queries.planView.buffersSummary', {
        cached: bufferBytes(plan.sharedHit),
        read: bufferBytes(plan.sharedRead),
      }),
    ])
  }
  for (const trigger of plan.triggers) {
    items.push([
      t('queries.planView.trigger', { name: trigger.name }),
      formatMilliseconds(trigger.ms),
    ])
  }
  return (
    <dl
      aria-label={t('queries.planView.summaryLabel')}
      className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-3 lg:grid-cols-6"
    >
      {items.map(([label, value]) => (
        <div key={label} className="min-w-0">
          <dt className="text-muted-foreground text-xs">{label}</dt>
          <dd className="font-medium tabular-nums">{value}</dd>
        </div>
      ))}
    </dl>
  )
}

const SEVERITY_STYLE: Record<InsightSeverity, string> = {
  high: 'border-l-destructive bg-destructive/5',
  medium: 'border-l-warning bg-warning/10',
  info: 'border-l-muted-foreground/40 bg-muted/40',
}

function SeverityIcon({ severity }: { severity: InsightSeverity }) {
  const Icon = severity === 'high' ? Siren : severity === 'medium' ? AlertTriangle : Info
  return <Icon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
}

function Insights({ plan, onShow }: { plan: ParsedPlan; onShow: (id: string) => void }) {
  const t = useT()
  if (plan.insights.length === 0) return null
  return (
    <section aria-label={t('queries.planView.insights')} className="space-y-2">
      <h4 className="text-sm font-semibold">{t('queries.planView.insights')}</h4>
      <ul className="space-y-2">
        {plan.insights.map((insight, index) => (
          <li
            key={`${insight.nodeId ?? 'plan'}-${index}`}
            className={`flex gap-2 rounded-md border border-l-4 px-3 py-2 text-sm ${SEVERITY_STYLE[insight.severity]}`}
            data-severity={insight.severity}
          >
            <SeverityIcon severity={insight.severity} />
            <div className="min-w-0 flex-1">
              <p className="font-medium">{insight.title}</p>
              <p className="text-muted-foreground">{insight.detail}</p>
            </div>
            {insight.nodeId !== null && (
              <button
                type="button"
                className="text-primary shrink-0 self-start text-xs font-medium hover:underline"
                onClick={() => onShow(insight.nodeId ?? '')}
              >
                {t('queries.planView.showInPlan')}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}

function Bar({ share, tone }: { share: number | null; tone: 'time' | 'cost' }) {
  if (share === null) return null
  const color =
    share >= 0.6
      ? 'bg-destructive/70'
      : share >= 0.3
        ? 'bg-warning'
        : tone === 'time'
          ? 'bg-primary/50'
          : 'bg-muted-foreground/40'
  return (
    <span
      aria-hidden="true"
      className="bg-muted mt-1 block h-1.5 w-full overflow-hidden rounded-full"
    >
      <span
        className={`block h-full ${color}`}
        style={{ width: `${Math.max(2, Math.round(share * 100))}%` }}
      />
    </span>
  )
}

function EstimateBadge({ node }: { node: PlanNodeView }) {
  const t = useT()
  const factor = node.estimateFactor
  if (factor === null) return null
  const spread = factor >= 1 ? factor : 1 / factor
  if (spread < 2) return null
  const tone =
    spread >= 100
      ? 'bg-destructive/15 text-destructive'
      : spread >= 10
        ? 'bg-warning/20'
        : 'bg-muted text-muted-foreground'
  return (
    <span
      className={`ml-1 rounded px-1 text-[11px] font-medium ${tone}`}
      title={factor > 1 ? t('queries.planView.moreRows') : t('queries.planView.fewerRows')}
    >
      {t(factor > 1 ? 'queries.planView.factorUnder' : 'queries.planView.factorOver', {
        factor: formatFactor(factor),
      })}
    </span>
  )
}

function NodeRow({
  node,
  analyzed,
  expanded,
  focused,
  onToggle,
}: {
  node: PlanNodeView
  analyzed: boolean
  expanded: boolean
  focused: boolean
  onToggle: () => void
}) {
  const t = useT()
  const rowRef = useRef<HTMLTableRowElement>(null)
  useEffect(() => {
    if (focused) rowRef.current?.scrollIntoView?.({ block: 'center', behavior: 'smooth' })
  }, [focused])
  const hasMore = node.details.length > 0 || node.output !== null
  return (
    <tr
      ref={rowRef}
      className={`border-b align-top last:border-0 ${focused ? 'bg-primary/5 ring-primary/40 ring-2 ring-inset' : ''} ${node.neverExecuted ? 'opacity-60' : ''}`}
      data-node-id={node.id}
    >
      <td className="py-2 pr-3">
        <div className="flex gap-1" style={{ paddingLeft: `${node.depth * 18}px` }}>
          {hasMore ? (
            <button
              type="button"
              aria-expanded={expanded}
              aria-label={
                expanded
                  ? t('queries.planView.hideDetails', { title: node.title })
                  : t('queries.planView.showDetails', { title: node.title })
              }
              className="text-muted-foreground hover:text-foreground mt-0.5 shrink-0"
              onClick={onToggle}
            >
              {expanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
            </button>
          ) : (
            <span className="w-4 shrink-0" />
          )}
          <div
            className={`min-w-0 ${node.depth > 0 ? 'border-muted-foreground/20 border-l pl-2' : ''}`}
          >
            <p className="leading-snug">
              {node.role && (
                <span className="bg-muted text-muted-foreground mr-1.5 rounded px-1 text-[11px]">
                  {node.role}
                </span>
              )}
              <span className="font-semibold">{node.title}</span>
              {node.object && (
                <span className="text-muted-foreground font-mono text-xs">
                  {t('queries.planView.onObject', { object: node.object })}
                </span>
              )}
              {node.neverExecuted && (
                <span className="text-muted-foreground ml-1 text-xs">
                  {t('queries.planView.neverExecuted')}
                </span>
              )}
            </p>
            {node.conditions.map((condition) => (
              <p key={condition.label} className="font-mono text-xs break-words">
                <span className="text-muted-foreground">{condition.label}:</span> {condition.text}
              </p>
            ))}
            {expanded && (
              <div className="mt-2 space-y-1">
                {node.details.length > 0 && (
                  <dl className="grid gap-x-4 gap-y-0.5 text-xs sm:grid-cols-[auto_1fr]">
                    {node.details.map((detail) => (
                      <div key={detail.key} className="contents">
                        <dt className="text-muted-foreground">{detail.label}</dt>
                        <dd className="tabular-nums">{detail.value}</dd>
                      </div>
                    ))}
                  </dl>
                )}
                {node.output && (
                  <p className="font-mono text-xs break-words">
                    <span className="text-muted-foreground">{t('queries.planView.output')}</span>{' '}
                    {node.output}
                  </p>
                )}
              </div>
            )}
          </div>
        </div>
      </td>
      <td className="py-2 pr-3 text-right whitespace-nowrap tabular-nums">
        {analyzed ? (
          <>
            <span>{node.actualRows === null ? '—' : formatCount(Math.round(node.actualRows))}</span>
            <EstimateBadge node={node} />
            <span className="text-muted-foreground block text-xs">
              {t('queries.planView.estimatedRows', {
                rows:
                  node.estimatedRows === null ? '—' : formatCount(Math.round(node.estimatedRows)),
              })}
            </span>
          </>
        ) : (
          <span>
            {node.estimatedRows === null ? '—' : formatCount(Math.round(node.estimatedRows))}
          </span>
        )}
      </td>
      {analyzed && (
        <td className="py-2 pr-3 text-right whitespace-nowrap tabular-nums">
          {node.loops === null ? '—' : formatCount(node.loops)}
        </td>
      )}
      <td className="w-36 py-2 pr-3 text-right whitespace-nowrap tabular-nums">
        {analyzed ? (
          <>
            <span>{formatMilliseconds(node.exclusiveMs)}</span>
            <span className="text-muted-foreground ml-1 text-xs">
              {node.timeShare === null ? '' : pct.format(node.timeShare)}
            </span>
            <Bar share={node.timeShare} tone="time" />
          </>
        ) : (
          <>
            <span>{node.exclusiveCost === null ? '—' : formatDecimal(node.exclusiveCost, 2)}</span>
            <span className="text-muted-foreground ml-1 text-xs">
              {node.costShare === null ? '' : pct.format(node.costShare)}
            </span>
            <Bar share={node.costShare} tone="cost" />
          </>
        )}
      </td>
      {analyzed && (
        <td className="py-2 text-right text-xs whitespace-nowrap tabular-nums">
          {node.sharedHit === null && node.sharedRead === null ? (
            '—'
          ) : (
            <>
              <span className="block">
                {t('queries.planView.bufferCached', { size: bufferBytes(node.sharedHit) })}
              </span>
              <span
                className={`block ${(node.sharedRead ?? 0) > 0 ? '' : 'text-muted-foreground'}`}
              >
                {t('queries.planView.bufferRead', { size: bufferBytes(node.sharedRead) })}
              </span>
            </>
          )}
        </td>
      )}
    </tr>
  )
}

function PlanTable({
  plan,
  focusId,
  expanded,
  onToggle,
}: {
  plan: ParsedPlan
  focusId: string | null
  expanded: ReadonlySet<string>
  onToggle: (id: string) => void
}) {
  const t = useT()
  return (
    <div className="overflow-x-auto rounded-lg border">
      <table aria-label={t('queries.planView.treeLabel')} className="w-full text-sm">
        <thead className="bg-muted/40 text-muted-foreground text-xs">
          <tr className="border-b">
            <th className="px-0 py-2 pl-3 text-left font-medium">
              {t('queries.common.operation')}
            </th>
            <th className="py-2 pr-3 text-right font-medium">
              {plan.analyzed ? t('queries.planView.rowsActual') : t('queries.common.rowsEstimated')}
            </th>
            {plan.analyzed && (
              <th className="py-2 pr-3 text-right font-medium">{t('queries.planView.loops')}</th>
            )}
            <th className="py-2 pr-3 text-right font-medium">
              {plan.analyzed ? t('queries.common.timeInNode') : t('queries.planView.costInNode')}
            </th>
            {plan.analyzed && (
              <th className="py-2 pr-3 text-right font-medium">{t('queries.planView.buffers')}</th>
            )}
          </tr>
        </thead>
        <tbody className="[&_td:first-child]:pl-3">
          {plan.nodes.map((node) => (
            <NodeRow
              key={node.id}
              node={node}
              analyzed={plan.analyzed}
              expanded={expanded.has(node.id)}
              focused={focusId === node.id}
              onToggle={() => onToggle(node.id)}
            />
          ))}
        </tbody>
      </table>
    </div>
  )
}

export interface PlanViewProps {
  plan: ParsedPlan
  valuesFrom?: PlanValuesFrom | null
  title?: string
}

/** One plan: summary, what to look at first, and the operations as a tree. */
export function PlanView({ plan, valuesFrom = null, title }: PlanViewProps) {
  const t = useT()
  const heading = title ?? t('queries.planView.defaultTitle')
  const [view, setView] = useState<View>('tree')
  const [focusId, setFocusId] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const toggle = (id: string) =>
    setExpanded((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const text = planText(plan)
  const json = JSON.stringify(plan.root, null, 2)
  return (
    <section aria-label={heading} className="space-y-4" data-caption-id="queries.plan-tree">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-1">
          <h3 className="font-medium">{heading}</h3>
          <InfoHint id="queries.plan-tree" label={heading} />
        </div>
        <div
          role="group"
          aria-label={t('queries.planView.viewLabel')}
          className="flex rounded-md border p-0.5 text-xs"
        >
          {(['tree', 'text', 'json'] as const).map((option) => (
            <button
              key={option}
              type="button"
              aria-pressed={view === option}
              className={`rounded px-2.5 py-1 font-medium ${view === option ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground'}`}
              onClick={() => setView(option)}
            >
              {option === 'tree'
                ? t('queries.planView.viewTree')
                : option === 'text'
                  ? t('queries.planView.viewText')
                  : t('queries.planView.viewJson')}
            </button>
          ))}
        </div>
      </div>
      <Summary plan={plan} valuesFrom={valuesFrom} />
      <Insights
        plan={plan}
        onShow={(id) => {
          setView('tree')
          setFocusId(id)
          setExpanded((current) => new Set(current).add(id))
        }}
      />
      {view === 'tree' && (
        <PlanTable plan={plan} focusId={focusId} expanded={expanded} onToggle={toggle} />
      )}
      {view !== 'tree' && (
        <div className="relative">
          <div className="absolute top-2 right-2">
            <CopyButton text={view === 'text' ? text : json} />
          </div>
          <pre
            aria-label={
              view === 'text' ? t('queries.planView.asText') : t('queries.planView.asJson')
            }
            className="bg-muted/30 max-h-[32rem] overflow-auto rounded-lg border p-3 font-mono text-xs"
            tabIndex={0}
          >
            {view === 'text' ? text : json}
          </pre>
        </div>
      )}
    </section>
  )
}
