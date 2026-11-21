import type { MessageArgs } from '@/i18n/format'
import type { MessageKey } from '@/i18n/messages/en'
import { REFERENCE_LOCALE } from '@/i18n/locales'
import { activeIntl, translate } from '@/i18n/store'

import { formatBytes, formatCount, formatDecimal, formatMilliseconds } from './format'

/*
 * An EXPLAIN (FORMAT JSON) plan as a reader needs it: one row per operation in
 * tree order, with what it reads, its conditions, the time and cost it adds by
 * itself (not with its children), rows estimated against rows produced, and
 * the problems worth a look first. PostgreSQL reports times and rows per loop
 * and inclusive of the children; this module turns them into totals.
 */

type PlanRecord = Record<string, unknown>

export type PlanValuesFrom = 'values' | 'recent_execution' | 'generic'

export interface PlanCondition {
  label: string
  text: string
}

export type PlanDetailKey =
  | 'rowsRemovedByFilter'
  | 'rowsRemovedByJoinFilter'
  | 'rowsRemovedByRecheck'
  | 'heapFetches'
  | 'sort'
  | 'hash'
  | 'diskUsed'
  | 'workers'
  | 'heapBlocks'
  | 'memoize'
  | 'partitionsPruned'
  | 'startupCost'
  | 'rowWidth'

export interface PlanDetail {
  key: PlanDetailKey
  label: string
  value: string
}

export interface PlanNodeView {
  /** Position in the tree: "0", "0.1", "0.1.0". */
  id: string
  depth: number
  title: string
  /** What the node reads: table, index, function or CTE. */
  object: string | null
  /** InitPlan, SubPlan or the side of a join, when it is not plain. */
  role: string | null
  conditions: PlanCondition[]
  details: PlanDetail[]
  output: string | null
  totalCost: number | null
  exclusiveCost: number | null
  /** Share of the root cost this node adds by itself, 0..1. */
  costShare: number | null
  /** Rows the planner expects over all loops. */
  estimatedRows: number | null
  /** Rows produced over all loops (ANALYZE). */
  actualRows: number | null
  loops: number | null
  inclusiveMs: number | null
  exclusiveMs: number | null
  /** Share of the execution time spent in this node itself, 0..1. */
  timeShare: number | null
  /** actual / estimated rows; above 1 the planner underestimated. */
  estimateFactor: number | null
  rowsRemoved: number | null
  sharedHit: number | null
  sharedRead: number | null
  tempWritten: number | null
  neverExecuted: boolean
  raw: PlanRecord
}

export type InsightSeverity = 'high' | 'medium' | 'info'

export type PlanInsightKind =
  | 'slowest'
  | 'estimate'
  | 'discards'
  | 'sortSpill'
  | 'hashBatches'
  | 'hashAggSpill'
  | 'heapFetches'
  | 'seqLoop'
  | 'lossyBitmap'
  | 'workersShort'
  | 'joinFilterDiscards'
  | 'jitShare'
  | 'triggerShare'
  | 'never'
  | 'planOnly'
  | 'generic'

export interface PlanInsight {
  kind: PlanInsightKind
  severity: InsightSeverity
  nodeId: string | null
  /** Raw figures behind the title: kB, ms, shares 0..1, counts. */
  args?: Record<string, number | string>
  title: string
  detail: string
}

export interface ParsedPlan {
  analyzed: boolean
  nodes: PlanNodeView[]
  planningMs: number | null
  executionMs: number | null
  totalCost: number | null
  rows: number | null
  sharedHit: number | null
  sharedRead: number | null
  triggers: { name: string; ms: number | null; calls: number | null }[]
  insights: PlanInsight[]
  /** The top-level object of the EXPLAIN output (Plan, Planning Time, ...). */
  root: PlanRecord
}

export const BLOCK_BYTES = 8192

export function valuesFromLabel(valuesFrom: PlanValuesFrom): string {
  return translate(`queries.plan.valuesFrom.${valuesFrom}`)
}

function isRecord(value: unknown): value is PlanRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function num(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function str(value: unknown): string | null {
  return typeof value === 'string' && value !== '' ? value : null
}

function text(value: unknown): string | null {
  if (Array.isArray(value)) {
    const items = value.filter((item) => typeof item === 'string' || typeof item === 'number')
    return items.length > 0 ? items.join(', ') : null
  }
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return str(value)
}

/** Finds the object holding "Plan" in a command result or a stored plan. */
export function findExplainRoot(value: unknown, depth = 0): PlanRecord | null {
  if (depth > 4) return null
  if (Array.isArray(value)) {
    for (const item of value) {
      const found = findExplainRoot(item, depth + 1)
      if (found) return found
    }
    return null
  }
  if (!isRecord(value)) return null
  if (isRecord(value.Plan)) return value
  if (typeof value['Node Type'] === 'string') return { Plan: value }
  for (const key of ['plan', 'result']) {
    const found = findExplainRoot(value[key], depth + 1)
    if (found) return found
  }
  return null
}

const AGGREGATE_NAMES: Record<string, string> = {
  Sorted: 'GroupAggregate',
  Hashed: 'HashAggregate',
  Mixed: 'MixedAggregate',
}

/** The operation as PostgreSQL's text format names it. */
export function nodeTitle(node: PlanRecord): string {
  const type = str(node['Node Type']) ?? 'Plan node'
  let title = type
  const join = str(node['Join Type'])
  if (join && join !== 'Inner') {
    if (type === 'Nested Loop') title = `Nested Loop ${join} Join`
    else if (type.endsWith(' Join')) title = `${type.slice(0, -5)} ${join} Join`
  }
  if (type === 'Aggregate') title = AGGREGATE_NAMES[str(node.Strategy) ?? ''] ?? 'Aggregate'
  if (type === 'SetOp' && node.Strategy === 'Hashed') title = 'HashSetOp'
  if (type === 'ModifyTable') title = str(node.Operation) ?? type
  const partial = str(node['Partial Mode'])
  if (partial && partial !== 'Simple') title = `${partial} ${title}`
  if (node['Parallel Aware'] === true) title = `Parallel ${title}`
  if (node['Scan Direction'] === 'Backward') title = `${title} Backward`
  return title
}

function nodeObject(node: PlanRecord): string | null {
  const relation = str(node['Relation Name'])
  const index = str(node['Index Name'])
  const parts: string[] = []
  if (relation) {
    const schema = str(node.Schema)
    const alias = str(node.Alias)
    const name = schema ? `${schema}.${relation}` : relation
    parts.push(alias && alias !== relation ? `${name} ${alias}` : name)
  }
  if (index) parts.push(`using ${index}`)
  const fn = str(node['Function Name'])
  if (fn) parts.push(`function ${fn}()`)
  const cte = str(node['CTE Name'])
  if (cte) parts.push(`CTE ${cte}`)
  if (parts.length === 0 && node['Node Type'] === 'Subquery Scan') {
    const alias = str(node.Alias)
    if (alias) parts.push(alias)
  }
  return parts.length > 0 ? parts.join(' ') : null
}

function nodeRole(node: PlanRecord): string | null {
  const subplan = str(node['Subplan Name'])
  if (subplan) return subplan
  const relationship = str(node['Parent Relationship'])
  return relationship === 'InitPlan' || relationship === 'SubPlan' ? relationship : null
}

const CONDITION_KEYS = [
  'Index Cond',
  'Recheck Cond',
  'Hash Cond',
  'Merge Cond',
  'Join Filter',
  'Filter',
  'One-Time Filter',
  'TID Cond',
  'Cache Key',
  'Sort Key',
  'Presorted Key',
  'Group Key',
  'Hash Key',
] as const

const KB = 1024

type Say = (key: MessageKey, args?: MessageArgs) => string

/** Detail rows of a node, in the console language or in `locale`. */
function detailRows(node: PlanRecord, loops: number, locale?: string): PlanDetail[] {
  const intl = locale === undefined ? activeIntl() : 'en-US'
  const say: Say = (key, args) => translate(key, args, locale)
  const count = (n: number) => formatCount(n, intl)
  const bytes = (n: number) => formatBytes(n, intl)
  const rows: PlanDetail[] = []
  const push = (key: PlanDetailKey, value: string) =>
    rows.push({ key, label: say(`queries.plan.detail.${key}`), value })
  const perLoopTotal = (field: string, key: PlanDetailKey) => {
    const value = num(node[field])
    if (value !== null && value > 0) push(key, count(value * loops))
  }
  perLoopTotal('Rows Removed by Filter', 'rowsRemovedByFilter')
  perLoopTotal('Rows Removed by Join Filter', 'rowsRemovedByJoinFilter')
  perLoopTotal('Rows Removed by Index Recheck', 'rowsRemovedByRecheck')
  perLoopTotal('Heap Fetches', 'heapFetches')
  const sortMethod = str(node['Sort Method'])
  if (sortMethod) {
    const space = num(node['Sort Space Used'])
    const onDisk = str(node['Sort Space Type']) === 'Disk'
    push(
      'sort',
      space === null
        ? sortMethod
        : say(onDisk ? 'queries.plan.detail.sortOnDisk' : 'queries.plan.detail.sortInMemory', {
            method: sortMethod,
            size: bytes(space * KB),
          }),
    )
  }
  const batches = num(node['Hash Batches'])
  if (batches !== null) {
    push(
      'hash',
      say('queries.plan.detail.hashValue', {
        buckets: count(num(node['Hash Buckets']) ?? 0),
        n: batches,
        memory: bytes((num(node['Peak Memory Usage']) ?? 0) * KB),
      }),
    )
  }
  const disk = num(node['Disk Usage'])
  if (disk !== null && disk > 0) push('diskUsed', bytes(disk * KB))
  const planned = num(node['Workers Planned'])
  if (planned !== null) {
    const launched = num(node['Workers Launched'])
    push(
      'workers',
      launched === null
        ? say('queries.plan.detail.workersPlanned', { planned })
        : say('queries.plan.detail.workersLaunched', { launched, planned }),
    )
  }
  const exact = num(node['Exact Heap Blocks'])
  const lossy = num(node['Lossy Heap Blocks'])
  if (exact !== null || lossy !== null) {
    push(
      'heapBlocks',
      say('queries.plan.detail.heapBlocksValue', {
        exact: count(exact ?? 0),
        lossy: count(lossy ?? 0),
      }),
    )
  }
  const hits = num(node['Cache Hits'])
  if (hits !== null) {
    push(
      'memoize',
      say('queries.plan.detail.memoizeValue', {
        hits: count(hits),
        misses: count(num(node['Cache Misses']) ?? 0),
      }),
    )
  }
  const removed = num(node['Subplans Removed'])
  if (removed !== null && removed > 0) push('partitionsPruned', count(removed))
  const startup = num(node['Startup Cost'])
  if (startup !== null) push('startupCost', formatDecimal(startup, 2))
  const width = num(node['Plan Width'])
  if (width !== null)
    push('rowWidth', say('queries.plan.detail.rowWidthValue', { width: count(width) }))
  return rows
}

function children(node: PlanRecord): PlanRecord[] {
  return Array.isArray(node.Plans) ? node.Plans.filter(isRecord) : []
}

function hasActuals(node: PlanRecord): boolean {
  return node['Actual Loops'] !== undefined || node['Actual Total Time'] !== undefined
}

interface Walked {
  node: PlanNodeView
  childIds: string[]
}

function walk(
  record: PlanRecord,
  id: string,
  depth: number,
  parallel: number,
  out: Walked[],
): PlanNodeView {
  const loops = num(record['Actual Loops'])
  const loopCount = loops ?? 1
  // Under Gather the per-loop figures average the leader and the workers,
  // which ran at the same time: dividing by them keeps the wall time.
  const launched = num(record['Workers Launched'])
  const divisor = launched !== null ? 1 : parallel
  const perLoopMs = num(record['Actual Total Time'])
  const inclusiveMs = perLoopMs === null || loops === null ? null : (perLoopMs * loops) / divisor
  const planRows = num(record['Plan Rows'])
  const actualPerLoop = num(record['Actual Rows'])
  const conditions: PlanCondition[] = []
  for (const key of CONDITION_KEYS) {
    const value = text(record[key])
    if (value) conditions.push({ label: key, text: value })
  }
  const shared = (key: string) => num(record[key])
  const view: PlanNodeView = {
    id,
    depth,
    title: nodeTitle(record),
    object: nodeObject(record),
    role: nodeRole(record),
    conditions,
    details: detailRows(record, loopCount),
    output: text(record.Output),
    totalCost: num(record['Total Cost']),
    exclusiveCost: null,
    costShare: null,
    estimatedRows: planRows === null ? null : planRows * (loops !== null && loops > 0 ? loops : 1),
    actualRows: actualPerLoop === null || loops === null ? null : actualPerLoop * loops,
    loops,
    inclusiveMs,
    exclusiveMs: null,
    timeShare: null,
    estimateFactor: null,
    rowsRemoved: null,
    sharedHit: shared('Shared Hit Blocks'),
    sharedRead: shared('Shared Read Blocks'),
    tempWritten: shared('Temp Written Blocks'),
    neverExecuted: hasActuals(record) && loops === 0,
    raw: record,
  }
  const removed =
    (num(record['Rows Removed by Filter']) ?? 0) + (num(record['Rows Removed by Join Filter']) ?? 0)
  if (removed > 0) view.rowsRemoved = removed * loopCount
  if (view.actualRows !== null && view.estimatedRows !== null && !view.neverExecuted) {
    view.estimateFactor = Math.max(view.actualRows, 1) / Math.max(view.estimatedRows, 1)
  }
  const entry: Walked = { node: view, childIds: [] }
  out.push(entry)
  const nextParallel = launched !== null ? launched + 1 : parallel
  const kids = children(record).map((child, index) =>
    walk(child, `${id}.${index}`, depth + 1, nextParallel, out),
  )
  entry.childIds = kids.map((kid) => kid.id)
  const childCost = kids.reduce((sum, kid) => sum + (kid.totalCost ?? 0), 0)
  if (view.totalCost !== null) view.exclusiveCost = Math.max(0, view.totalCost - childCost)
  if (inclusiveMs !== null) {
    const childMs = kids.reduce((sum, kid) => sum + (kid.inclusiveMs ?? 0), 0)
    view.exclusiveMs = Math.max(0, inclusiveMs - childMs)
  }
  return view
}

function percent(share: number): string {
  return new Intl.NumberFormat(activeIntl(), { style: 'percent', maximumFractionDigits: 0 }).format(
    share,
  )
}

export function formatFactor(factor: number): string {
  const value = factor >= 1 ? factor : 1 / factor
  return `×${value >= 100 ? formatCount(Math.round(value)) : formatDecimal(value, value >= 10 ? 0 : 1)}`
}

function nodeName(node: PlanNodeView): string {
  return node.object ? `${node.title} on ${node.object}` : node.title
}

function insightsOf(
  plan: Omit<ParsedPlan, 'insights'>,
  valuesFrom: PlanValuesFrom | null,
): PlanInsight[] {
  const insights: PlanInsight[] = []
  const executed = plan.nodes.filter((node) => !node.neverExecuted)
  if (plan.analyzed) {
    const slowest = [...executed].sort((a, b) => (b.exclusiveMs ?? 0) - (a.exclusiveMs ?? 0))[0]
    if (
      slowest?.timeShare !== null &&
      slowest !== undefined &&
      (slowest.timeShare ?? 0) >= 0.3 &&
      plan.nodes.length > 1
    ) {
      insights.push({
        kind: 'slowest',
        severity: (slowest.timeShare ?? 0) >= 0.6 ? 'high' : 'medium',
        nodeId: slowest.id,
        title: translate('queries.plan.insight.slowestTitle', {
          node: nodeName(slowest),
          share: percent(slowest.timeShare ?? 0),
        }),
        detail: translate('queries.plan.insight.slowestDetail', {
          ms: formatMilliseconds(slowest.exclusiveMs),
        }),
      })
    }
    const misestimated = executed
      .filter((node) => {
        const factor = node.estimateFactor
        if (factor === null) return false
        const big = Math.max(node.actualRows ?? 0, node.estimatedRows ?? 0) >= 100
        return big && (factor >= 10 || factor <= 0.1)
      })
      .sort((a, b) => spread(b.estimateFactor) - spread(a.estimateFactor))
      .slice(0, 3)
    for (const node of misestimated) {
      const under = (node.estimateFactor ?? 1) > 1
      insights.push({
        kind: 'estimate',
        severity: spread(node.estimateFactor) >= 100 ? 'high' : 'medium',
        nodeId: node.id,
        title: translate('queries.plan.insight.estimateTitle', {
          factor: formatFactor(node.estimateFactor ?? 1),
          node: nodeName(node),
        }),
        detail: translate(
          under ? 'queries.plan.insight.estimateUnder' : 'queries.plan.insight.estimateOver',
          {
            expected: formatCount(Math.round(node.estimatedRows ?? 0)),
            actual: formatCount(Math.round(node.actualRows ?? 0)),
          },
        ),
      })
    }
  }
  for (const node of executed) {
    const removed = node.rowsRemoved ?? 0
    const kept = node.actualRows ?? node.estimatedRows ?? 0
    if (node.title.includes('Seq Scan') && removed >= 1000 && removed / (removed + kept) >= 0.9) {
      insights.push({
        kind: 'discards',
        severity: 'medium',
        nodeId: node.id,
        title: translate('queries.plan.insight.discardsTitle', {
          node: nodeName(node),
          share: percent(removed / (removed + kept)),
        }),
        detail: translate('queries.plan.insight.discardsDetail', {
          read: formatCount(removed + kept),
          kept: formatCount(kept),
        }),
      })
    }
    const sortSpace = num(node.raw['Sort Space Used'])
    if (
      node.raw['Sort Space Type'] === 'Disk' ||
      /external/i.test(str(node.raw['Sort Method']) ?? '')
    ) {
      insights.push({
        kind: 'sortSpill',
        ...(sortSpace === null ? {} : { args: { kb: sortSpace } }),
        severity: 'medium',
        nodeId: node.id,
        title:
          sortSpace === null
            ? translate('queries.plan.insight.sortSpillTitle', { node: node.title })
            : translate('queries.plan.insight.sortSpillSizeTitle', {
                node: node.title,
                size: formatBytes(sortSpace * KB),
              }),
        detail: translate('queries.plan.insight.sortSpillDetail'),
      })
    }
    const batches = num(node.raw['Hash Batches'])
    if (batches !== null && batches > 1) {
      insights.push({
        kind: 'hashBatches',
        severity: 'medium',
        nodeId: node.id,
        title: translate('queries.plan.insight.hashBatchesTitle', {
          batches: formatCount(batches),
        }),
        detail: translate('queries.plan.insight.hashBatchesDetail'),
      })
    }
    const disk = num(node.raw['Disk Usage'])
    if (node.title.includes('HashAggregate') && disk !== null && disk > 0) {
      insights.push({
        kind: 'hashAggSpill',
        severity: 'medium',
        nodeId: node.id,
        title: translate('queries.plan.insight.hashAggSpillTitle', {
          node: node.title,
          size: formatBytes(disk * KB),
        }),
        detail: translate('queries.plan.insight.hashAggSpillDetail'),
      })
    }
    const fetches = num(node.raw['Heap Fetches'])
    if (
      node.title.includes('Index Only Scan') &&
      fetches !== null &&
      fetches * (node.loops ?? 1) >= 1000
    ) {
      insights.push({
        kind: 'heapFetches',
        severity: 'info',
        nodeId: node.id,
        title: translate('queries.plan.insight.heapFetchesTitle', {
          node: nodeName(node),
          count: formatCount(fetches * (node.loops ?? 1)),
        }),
        detail: translate('queries.plan.insight.heapFetchesDetail'),
      })
    }
    if ((node.loops ?? 0) >= 1000 && node.title.includes('Seq Scan')) {
      insights.push({
        kind: 'seqLoop',
        severity: 'high',
        nodeId: node.id,
        title: translate('queries.plan.insight.seqLoopTitle', {
          node: nodeName(node),
          count: formatCount(node.loops ?? 0),
        }),
        detail: translate('queries.plan.insight.seqLoopDetail'),
      })
    }
    const type = str(node.raw['Node Type'])
    const lossy = num(node.raw['Lossy Heap Blocks'])
    if (type === 'Bitmap Heap Scan' && lossy !== null && lossy > 0) {
      const exact = num(node.raw['Exact Heap Blocks']) ?? 0
      insights.push({
        kind: 'lossyBitmap',
        severity: 'medium',
        nodeId: node.id,
        args: { lossy, exact },
        title: translate('queries.plan.insight.lossyBitmapTitle', {
          lossy: formatCount(lossy),
          total: formatCount(lossy + exact),
        }),
        detail: translate('queries.plan.insight.lossyBitmapDetail'),
      })
    }
    const planned = num(node.raw['Workers Planned'])
    const launched = num(node.raw['Workers Launched'])
    if (
      (type === 'Gather' || type === 'Gather Merge') &&
      planned !== null &&
      launched !== null &&
      launched < planned
    ) {
      insights.push({
        kind: 'workersShort',
        severity: 'medium',
        nodeId: node.id,
        args: { launched, planned },
        title: translate('queries.plan.insight.workersShortTitle', {
          launched: formatCount(launched),
          planned: formatCount(planned),
        }),
        detail: translate('queries.plan.insight.workersShortDetail'),
      })
    }
    const joinRemoved = num(node.raw['Rows Removed by Join Filter'])
    if (
      (type === 'Nested Loop' || type === 'Hash Join' || type === 'Merge Join') &&
      joinRemoved !== null
    ) {
      const loops = node.loops ?? 1
      const removedTotal = joinRemoved * loops
      const keptTotal = node.actualRows ?? 0
      if (removedTotal >= 10000 && removedTotal >= 10 * keptTotal) {
        insights.push({
          kind: 'joinFilterDiscards',
          severity: 'medium',
          nodeId: node.id,
          args: { removed: removedTotal, kept: keptTotal },
          title: translate('queries.plan.insight.joinFilterDiscardsTitle', {
            removed: formatCount(removedTotal),
            kept: formatCount(keptTotal),
          }),
          detail: translate('queries.plan.insight.joinFilterDiscardsDetail'),
        })
      }
    }
  }
  const execMs = plan.analyzed ? plan.executionMs : null
  if (execMs !== null && execMs > 0) {
    const jit =
      isRecord(plan.root.JIT) && isRecord(plan.root.JIT.Timing)
        ? num(plan.root.JIT.Timing.Total)
        : null
    if (jit !== null && jit >= 50 && jit / execMs >= 0.2) {
      insights.push({
        kind: 'jitShare',
        severity: 'medium',
        nodeId: null,
        args: { ms: jit, pct: jit / execMs },
        title: translate('queries.plan.insight.jitShareTitle', {
          ms: formatMilliseconds(jit),
          pct: percent(jit / execMs),
        }),
        detail: translate('queries.plan.insight.jitShareDetail'),
      })
    }
    const timed = plan.triggers.filter((trigger) => trigger.ms !== null)
    const triggerMs = timed.reduce((sum, trigger) => sum + (trigger.ms ?? 0), 0)
    if (timed.length > 0 && triggerMs / execMs >= 0.2) {
      const slowestTrigger = timed.reduce((top, trigger) =>
        (trigger.ms ?? 0) > (top.ms ?? 0) ? trigger : top,
      )
      insights.push({
        kind: 'triggerShare',
        severity: 'medium',
        nodeId: null,
        args: { ms: triggerMs, pct: triggerMs / execMs, name: slowestTrigger.name },
        title: translate('queries.plan.insight.triggerShareTitle', {
          ms: formatMilliseconds(triggerMs),
          pct: percent(triggerMs / execMs),
          name: slowestTrigger.name,
        }),
        detail: translate('queries.plan.insight.triggerShareDetail'),
      })
    }
  }
  const never = plan.nodes.filter((node) => node.neverExecuted).length
  if (never > 0) {
    insights.push({
      kind: 'never',
      severity: 'info',
      nodeId: null,
      title: translate('queries.plan.insight.neverTitle', { n: never }),
      detail: translate('queries.plan.insight.neverDetail'),
    })
  }
  if (!plan.analyzed) {
    insights.push({
      kind: 'planOnly',
      severity: 'info',
      nodeId: null,
      title: translate('queries.plan.insight.planOnlyTitle'),
      detail: translate('queries.plan.insight.planOnlyDetail'),
    })
  }
  if (valuesFrom === 'generic') {
    insights.push({
      kind: 'generic',
      severity: 'info',
      nodeId: null,
      title: translate('queries.plan.insight.genericTitle'),
      detail: translate('queries.plan.insight.genericDetail'),
    })
  }
  return insights
}

function spread(factor: number | null): number {
  if (factor === null || factor <= 0) return 1
  return factor >= 1 ? factor : 1 / factor
}

/** Parses an EXPLAIN (FORMAT JSON) result; null when it holds no plan. */
export function parsePlan(
  value: unknown,
  valuesFrom: PlanValuesFrom | null = null,
): ParsedPlan | null {
  const root = findExplainRoot(value)
  if (!root || !isRecord(root.Plan)) return null
  const walked: Walked[] = []
  const top = walk(root.Plan, '0', 0, 1, walked)
  const nodes = walked.map((entry) => entry.node)
  const analyzed = hasActuals(root.Plan)
  const executionMs = num(root['Execution Time'])
  const timeBase = executionMs ?? top.inclusiveMs
  const costBase = top.totalCost
  for (const node of nodes) {
    if (costBase && node.exclusiveCost !== null) node.costShare = node.exclusiveCost / costBase
    if (timeBase && node.exclusiveMs !== null)
      node.timeShare = Math.min(1, node.exclusiveMs / timeBase)
  }
  const triggers = Array.isArray(root.Triggers)
    ? root.Triggers.filter(isRecord).map((trigger) => ({
        name: str(trigger['Trigger Name']) ?? 'trigger',
        ms: num(trigger.Time),
        calls: num(trigger.Calls),
      }))
    : []
  const base = {
    analyzed,
    nodes,
    planningMs: num(root['Planning Time']),
    executionMs,
    totalCost: top.totalCost,
    rows: analyzed ? top.actualRows : top.estimatedRows,
    sharedHit: top.sharedHit,
    sharedRead: top.sharedRead,
    triggers,
    root,
  }
  return { ...base, insights: insightsOf(base, valuesFrom) }
}

function fixed(value: unknown, digits: number): string {
  const n = num(value)
  return n === null ? '?' : n.toFixed(digits)
}

/** The plan in PostgreSQL's text format, to paste where EXPLAIN text is read. */
export function planText(plan: ParsedPlan): string {
  const lines: string[] = []
  for (const node of plan.nodes) {
    const record = node.raw
    const pad = node.depth === 0 ? '' : ' '.repeat(node.depth * 6 - 4)
    const arrow = node.depth === 0 ? '' : '->  '
    if (node.role && node.depth > 0) lines.push(`${' '.repeat(node.depth * 6 - 4)}${node.role}`)
    let head = `${pad}${arrow}${node.title}`
    if (node.object) head += ` on ${node.object}`
    if (record['Total Cost'] !== undefined) {
      head += `  (cost=${fixed(record['Startup Cost'], 2)}..${fixed(record['Total Cost'], 2)} rows=${fixed(record['Plan Rows'], 0)} width=${fixed(record['Plan Width'], 0)})`
    }
    if (node.neverExecuted) head += ' (never executed)'
    else if (hasActuals(record)) {
      head += ` (actual time=${fixed(record['Actual Startup Time'], 3)}..${fixed(record['Actual Total Time'], 3)} rows=${fixed(record['Actual Rows'], 0)} loops=${fixed(record['Actual Loops'], 0)})`
    }
    lines.push(head)
    const indent = ' '.repeat(node.depth === 0 ? 2 : node.depth * 6 + 2)
    for (const condition of node.conditions)
      lines.push(`${indent}${condition.label}: ${condition.text}`)
    for (const detail of detailRows(record, node.loops ?? 1, REFERENCE_LOCALE)) {
      if (detail.key !== 'startupCost' && detail.key !== 'rowWidth') {
        lines.push(`${indent}${detail.label}: ${detail.value}`)
      }
    }
    if (node.sharedHit !== null || node.sharedRead !== null) {
      lines.push(`${indent}Buffers: shared hit=${node.sharedHit ?? 0} read=${node.sharedRead ?? 0}`)
    }
  }
  if (plan.planningMs !== null) lines.push(`Planning Time: ${plan.planningMs.toFixed(3)} ms`)
  for (const trigger of plan.triggers) {
    lines.push(
      `Trigger ${trigger.name}: time=${trigger.ms?.toFixed(3) ?? '?'} calls=${trigger.calls ?? '?'}`,
    )
  }
  if (plan.executionMs !== null) lines.push(`Execution Time: ${plan.executionMs.toFixed(3)} ms`)
  return lines.join('\n')
}

/** Highest $n in a normalised statement, as the agent counts parameters. */
export function placeholderCount(statement: string): number {
  let highest = 0
  for (const match of statement.matchAll(/\$(\d+)/g)) {
    const n = Number(match[1])
    if (n > highest) highest = n
  }
  return highest
}

export function bufferBytes(blocks: number | null): string {
  return blocks === null ? '—' : formatBytes(blocks * BLOCK_BYTES)
}

/* ---------- comparison of two captured plans ---------- */

export type NodeChange = 'same' | 'changed' | 'added' | 'removed'

export interface PlanDiffRow {
  id: string
  depth: number
  left: PlanNodeView | undefined
  right: PlanNodeView | undefined
  change: NodeChange
}

function signature(node: PlanNodeView): string {
  return `${node.title}|${node.object ?? ''}|${node.conditions.map((c) => `${c.label}:${c.text}`).join('|')}`
}

/** Aligns two plans by position in the tree: same id, same place. */
export function diffPlans(left: ParsedPlan, right: ParsedPlan): PlanDiffRow[] {
  const leftById = new Map(left.nodes.map((node) => [node.id, node]))
  const rightById = new Map(right.nodes.map((node) => [node.id, node]))
  const ids: string[] = []
  const seen = new Set<string>()
  const push = (id: string) => {
    if (!seen.has(id)) {
      seen.add(id)
      ids.push(id)
    }
  }
  // Tree order: walk both pre-orders, keeping a node's subtree under it.
  const order = [...left.nodes, ...right.nodes].map((node) => node.id)
  order.sort((a, b) => {
    const pa = a.split('.').map(Number)
    const pb = b.split('.').map(Number)
    for (let i = 0; i < Math.min(pa.length, pb.length); i++) {
      if (pa[i] !== pb[i]) return (pa[i] ?? 0) - (pb[i] ?? 0)
    }
    return pa.length - pb.length
  })
  order.forEach(push)
  return ids.map((id) => {
    const l = leftById.get(id)
    const r = rightById.get(id)
    const change: NodeChange =
      l === undefined
        ? 'added'
        : r === undefined
          ? 'removed'
          : signature(l) === signature(r)
            ? 'same'
            : 'changed'
    return { id, depth: id.split('.').length - 1, left: l, right: r, change }
  })
}

export function changeRatio(left: number | null, right: number | null): number | null {
  if (left === null || right === null || left === 0) return null
  return right / left
}
