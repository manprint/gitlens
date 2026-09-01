import { useRef, useState } from 'react'

import { useCreateCommand } from '@/api/commands'
import type { PermTier } from '@/api/types'
import { NotPermitted } from '@/components/state'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { describeCommandState } from '@/lib/commands'

type QueryID = number | string

export interface ExplainPanelProps {
  queryid: QueryID
  datname?: string
  instanceId?: string
  currentTier?: PermTier
  normalized?: boolean
}

type PlanRecord = Record<string, unknown>

function isRecord(value: unknown): value is PlanRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function findPlanNode(value: unknown): PlanRecord | null {
  if (Array.isArray(value)) {
    for (const item of value) {
      const plan = findPlanNode(item)
      if (plan) return plan
    }
    return null
  }
  if (!isRecord(value)) return null
  if (typeof value['Node Type'] === 'string') return value
  for (const child of Object.values(value)) {
    const plan = findPlanNode(child)
    if (plan) return plan
  }
  return null
}

function displayNumber(value: unknown): string | null {
  return typeof value === 'number' && Number.isFinite(value) ? String(value) : null
}

function PlanNode({ node, depth = 0 }: { node: PlanRecord; depth?: number }) {
  const nodeType = typeof node['Node Type'] === 'string' ? node['Node Type'] : 'Plan node'
  const estimatedRows = displayNumber(node['Plan Rows'])
  const actualRows = displayNumber(node['Actual Rows'])
  const totalCost = displayNumber(node['Total Cost'])
  const children = Array.isArray(node.Plans)
    ? node.Plans.filter((child): child is PlanRecord => isRecord(child))
    : []

  return (
    <li>
      <details open={depth === 0}>
        <summary>
          <span>{nodeType}</span>
          {totalCost !== null && (
            <span className="text-muted-foreground ml-2">cost {totalCost}</span>
          )}
        </summary>
        <dl className="mt-2 grid gap-1 pl-4 text-sm sm:grid-cols-3">
          {estimatedRows !== null && (
            <div>
              <dt className="text-muted-foreground">Estimated rows</dt>
              <dd className="tabular-nums">{estimatedRows}</dd>
            </div>
          )}
          {actualRows !== null && (
            <div>
              <dt className="text-muted-foreground">Actual rows</dt>
              <dd className="tabular-nums">{actualRows}</dd>
            </div>
          )}
          {totalCost !== null && (
            <div>
              <dt className="text-muted-foreground">Total cost</dt>
              <dd className="tabular-nums">{totalCost}</dd>
            </div>
          )}
        </dl>
        {children.length > 0 && (
          <ul className="mt-3 space-y-2 border-l pl-4">
            {children.map((child, index) => (
              <PlanNode key={`${nodeType}-${index}`} node={child} depth={depth + 1} />
            ))}
          </ul>
        )}
      </details>
    </li>
  )
}

function PlanResult({ result }: { result: unknown }) {
  const plan = findPlanNode(result)
  return (
    <section aria-labelledby="explain-result-title" className="space-y-3">
      <h3 id="explain-result-title" className="font-medium">
        Query plan
      </h3>
      {plan ? (
        <ul aria-label="Query plan tree" className="rounded-md border p-3">
          <PlanNode node={plan} />
        </ul>
      ) : (
        <p className="text-muted-foreground text-sm">
          The agent returned no structured plan nodes.
        </p>
      )}
      <details>
        <summary>Raw JSON</summary>
        <pre className="mt-2 max-h-96 overflow-auto rounded-md border p-3 text-xs">
          {JSON.stringify(result, null, 2)}
        </pre>
      </details>
    </section>
  )
}

function queryIDText(queryid: QueryID): string | null {
  if (typeof queryid === 'number') {
    return Number.isSafeInteger(queryid) ? String(queryid) : null
  }
  return /^-?\d+$/.test(queryid) ? queryid : null
}

export function ExplainPanel({
  queryid,
  datname,
  instanceId,
  currentTier = 'T0',
  normalized = false,
}: ExplainPanelProps) {
  const [confirmationOpen, setConfirmationOpen] = useState(false)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const createCommand = useCreateCommand(instanceId)
  const queryID = queryIDText(queryid)
  const canPlan = currentTier === 'T1' || currentTier === 'T2'

  const submit = (analyze: boolean) => {
    if (queryID === null) return
    createCommand.mutate({
      args: { analyze, datname, queryid: queryID },
      kind: 'explain',
      instanceId,
    })
  }

  const command = createCommand.command.data
  const status = command
    ? describeCommandState(command.state, command.error)
    : createCommand.isPending
      ? 'Command pending: waiting for the agent.'
      : null
  const result =
    command?.state === 'done' || command?.state === 'succeeded' ? command.result : undefined
  const planOnlyButton = (
    <button
      type="button"
      className="rounded-md border px-3 py-2 text-sm font-medium"
      aria-label="Run plan only"
      onClick={() => submit(false)}
      disabled={!canPlan || queryID === null || createCommand.isPending}
    >
      Plan only
    </button>
  )

  return (
    <section aria-labelledby="explain-panel-title" className="space-y-5">
      <header>
        <h1 id="explain-panel-title">Query detail and plan history</h1>
        <p className="text-muted-foreground text-sm">
          Request an execution plan for query {String(queryid)}
          {datname ? ` in ${datname}` : ''}.
        </p>
      </header>

      <div className="flex flex-wrap gap-3">
        {canPlan ? (
          planOnlyButton
        ) : (
          <NotPermitted required="T1" current={currentTier}>
            {planOnlyButton}
          </NotPermitted>
        )}
        <Button
          type="button"
          variant="outline"
          onClick={() => setConfirmationOpen(true)}
          disabled={queryID === null || createCommand.isPending}
        >
          Plan with ANALYZE
        </Button>
      </div>

      {normalized && (
        <p role="note" className="text-muted-foreground text-sm">
          This statement is normalised with placeholders; the plan may be unrepresentative for
          parameter-sensitive values.
        </p>
      )}

      {status && <p role="status">{status}</p>}
      {result !== undefined && <PlanResult result={result} />}

      <Dialog open={confirmationOpen} onOpenChange={setConfirmationOpen}>
        <DialogContent
          onOpenAutoFocus={(event) => {
            event.preventDefault()
            cancelRef.current?.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle>Run EXPLAIN ANALYZE?</DialogTitle>
            <DialogDescription>
              This executes the statement inside a transaction that is rolled back afterwards. It
              still consumes database resources and can take locks. Continue only if that impact is
              acceptable.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <button
              ref={cancelRef}
              type="button"
              className="rounded-md border px-3 py-2 text-sm font-medium"
              onClick={() => setConfirmationOpen(false)}
            >
              Cancel
            </button>
            <Button
              type="button"
              onClick={() => {
                setConfirmationOpen(false)
                submit(true)
              }}
            >
              Run EXPLAIN ANALYZE
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}

export default ExplainPanel
