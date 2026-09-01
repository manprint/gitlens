import { useMemo, useState } from 'react'

import { usePlans } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { Degraded } from '@/components/state/Degraded'
import { ErrorState } from '@/components/state/ErrorState'
import { formatTimestamp } from '@/lib/format'

type Plan = Schemas['Plan']

interface PlanHistoryProps {
  queryid: number | string
  instanceId?: string
  datname?: string
}

interface PlanNodeSnapshot {
  path: string
  nodeType: string
  totalCost: number | null
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function flattenPlan(value: unknown, path = 'root', result: PlanNodeSnapshot[] = []) {
  if (Array.isArray(value)) {
    value.forEach((child, index) => flattenPlan(child, `${path}.${index}`, result))
    return result
  }
  if (!isRecord(value)) return result

  if (typeof value['Node Type'] === 'string') {
    const rawCost = value['Total Cost']
    result.push({
      path,
      nodeType: value['Node Type'],
      totalCost: typeof rawCost === 'number' && Number.isFinite(rawCost) ? rawCost : null,
    })
  }

  Object.entries(value).forEach(([key, child]) => {
    if (key !== 'Node Type' && key !== 'Total Cost') {
      flattenPlan(child, `${path}.${key}`, result)
    }
  })
  return result
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

function planKind(plan: Plan): string {
  return plan.analyzed ? 'ANALYZE' : 'EXPLAIN'
}

function describePlan(plan: Plan): string {
  return `${formatTimestamp(plan.captured_at, 'UTC')} · ${planKind(plan)} · ${plan.plan_hash}`
}

function firstTwo(plans: Plan[]): [Plan, Plan] | null {
  if (plans.length < 2) return null
  const [first, second] = plans
  return first && second ? [first, second] : null
}

function plansDiffer(left: PlanNodeSnapshot | undefined, right: PlanNodeSnapshot | undefined) {
  return (
    left?.nodeType !== right?.nodeType ||
    left?.totalCost !== right?.totalCost ||
    left === undefined ||
    right === undefined
  )
}

function PlanComparison({ plans }: { plans: [Plan, Plan] }) {
  const [left, right] = plans
  const leftNodes = flattenPlan(left.plan)
  const rightNodes = flattenPlan(right.plan)
  const paths = [
    ...new Set([...leftNodes.map((node) => node.path), ...rightNodes.map((node) => node.path)]),
  ]
  const leftByPath = new Map(leftNodes.map((node) => [node.path, node]))
  const rightByPath = new Map(rightNodes.map((node) => [node.path, node]))

  return (
    <section aria-labelledby="plan-comparison-title" className="space-y-3">
      <h3 id="plan-comparison-title" className="font-medium">
        Plan comparison
      </h3>
      <p className="text-muted-foreground text-sm">
        Differing node types and total costs are highlighted.
      </p>
      {paths.length === 0 ? (
        <p className="text-muted-foreground text-sm">Neither selected entry contains plan nodes.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">Side-by-side comparison of two query plans</caption>
            <thead>
              <tr>
                <th scope="col">Path</th>
                <th scope="col">{planKey(left)}</th>
                <th scope="col">{planKey(right)}</th>
              </tr>
            </thead>
            <tbody>
              {paths.map((path) => {
                const leftNode = leftByPath.get(path)
                const rightNode = rightByPath.get(path)
                const different = plansDiffer(leftNode, rightNode)
                return (
                  <tr key={path}>
                    <th scope="row" className="py-2 pr-3 font-normal">
                      {path}
                    </th>
                    {[leftNode, rightNode].map((node, index) => (
                      <td
                        key={`${path}-${index}`}
                        className={`py-2 pr-3 ${different ? 'bg-amber-100 dark:bg-amber-950' : ''}`}
                        data-different={different ? 'true' : undefined}
                        data-testid={different ? 'plan-diff' : undefined}
                      >
                        {node ? `${node.nodeType} · cost ${node.totalCost ?? '—'}` : '—'}
                      </td>
                    ))}
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

export function PlanHistory({ queryid, instanceId, datname }: PlanHistoryProps) {
  const params = {
    queryid,
    ...(instanceId === undefined ? {} : { instance_id: instanceId }),
    ...(datname === undefined ? {} : { datname }),
    limit: 20,
  }
  const plansQuery = usePlans(params)
  const response = plansQuery.data as Schemas['PlansResponse'] | undefined
  const plans = useMemo(() => sortPlans(response?.plans ?? []), [response?.plans])
  const [selectedKeys, setSelectedKeys] = useState<string[]>([])
  const selectedPlans = plans.filter((plan) => selectedKeys.includes(planKey(plan))).slice(0, 2)
  const selectedPair = firstTwo(selectedPlans)

  const togglePlan = (plan: Plan) => {
    const key = planKey(plan)
    setSelectedKeys((current) => {
      if (current.includes(key)) return current.filter((selectedKey) => selectedKey !== key)
      return [...current, key].slice(-2)
    })
  }

  if (plansQuery.isPending && response === undefined) {
    return <div role="status">Loading plan history…</div>
  }

  if (plansQuery.error && response === undefined) {
    if (plansQuery.error.kind === 'not_found') {
      return (
        <Degraded
          reason={`Query ${String(queryid)} is no longer available in plan history. The entry may have been evicted from pg_stat_statements; query ids are only comparable within this cluster.`}
        />
      )
    }
    return (
      <ErrorState
        endpoint="plan history"
        failure={plansQuery.error}
        onRetry={() => void plansQuery.refetch()}
      />
    )
  }

  return (
    <section aria-labelledby="plan-history-title" className="space-y-4">
      <div>
        <h2 id="plan-history-title" className="text-lg font-medium">
          Plan history
        </h2>
        <p className="text-muted-foreground mt-1 text-sm">
          History contains only explicitly requested EXPLAIN plans; pglens does not sample plans
          automatically.
        </p>
      </div>

      {plans.length === 0 ? (
        <p role="status" className="text-muted-foreground text-sm">
          No plan history yet. Request an EXPLAIN above to create the first entry.
        </p>
      ) : (
        <>
          <ul aria-label="Plan history entries" className="space-y-2">
            {plans.map((plan) => {
              const key = planKey(plan)
              const selected = selectedKeys.includes(key)
              return (
                <li key={key}>
                  <button
                    type="button"
                    aria-pressed={selected}
                    className={`w-full rounded-md border px-3 py-2 text-left text-sm ${selected ? 'border-primary bg-muted' : ''}`}
                    onClick={() => togglePlan(plan)}
                  >
                    <span className="font-medium">{describePlan(plan)}</span>
                    <span className="text-muted-foreground ml-2">plan {key}</span>
                  </button>
                </li>
              )
            })}
          </ul>

          {selectedPair === null ? (
            <p role="status" className="text-muted-foreground text-sm">
              Select two plan entries to compare them side by side.
            </p>
          ) : (
            <PlanComparison plans={selectedPair} />
          )}
        </>
      )}
    </section>
  )
}

export default PlanHistory
