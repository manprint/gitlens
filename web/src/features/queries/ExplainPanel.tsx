import { useMemo, useRef, useState } from 'react'

import { useCreateCommand } from '@/api/commands'
import type { PermTier } from '@/api/types'
import { HelpLink } from '@/components/layout/HelpLink'
import { InfoHint } from '@/components/layout/InfoHint'
import { CommandSubmitError, NotPermitted } from '@/components/state'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { useT } from '@/i18n/react'
import { describeCommandState } from '@/lib/commands'
import { isTruncatedQuery } from '@/lib/format'
import { parsePlan, placeholderCount, type PlanValuesFrom } from '@/lib/planTree'

import { PlanView } from './PlanView'
import { useQueryStatement } from './useQueryStatement'

type QueryID = number | string

export interface ExplainPanelProps {
  queryid: QueryID
  datname?: string
  instanceId?: string
  currentTier?: PermTier
  /** The normalised statement text, to offer one input per $n. */
  statementText?: string | null
}

interface ParamState {
  value: string
  isNull: boolean
}

const VALUES_FROM = new Set<string>(['values', 'recent_execution', 'generic'])

function resultValuesFrom(result: unknown): PlanValuesFrom | null {
  if (typeof result !== 'object' || result === null) return null
  const value = (result as Record<string, unknown>).values_from
  return typeof value === 'string' && VALUES_FROM.has(value) ? (value as PlanValuesFrom) : null
}

/**
 * Inputs for the $1…$n of a normalised statement. All empty sends no values:
 * plan-only then plans generically and ANALYZE takes a recent execution's.
 */
function ParameterInputs({
  count,
  params,
  onChange,
}: {
  count: number
  params: ParamState[]
  onChange: (index: number, next: ParamState) => void
}) {
  const t = useT()
  return (
    <fieldset className="space-y-3 rounded-lg border p-4" data-caption-id="queries.explain-params">
      <legend className="px-1 text-sm font-semibold">{t('queries.explain.paramsLegend')}</legend>
      <p className="text-muted-foreground text-sm">{t('queries.explain.paramsHelp', { count })}</p>
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {params.map((param, index) => {
          const id = `explain-param-${index + 1}`
          return (
            <div key={id} className="flex items-center gap-2">
              <label htmlFor={id} className="w-8 shrink-0 font-mono text-sm">
                ${index + 1}
              </label>
              <input
                id={id}
                type="text"
                value={param.value}
                disabled={param.isNull}
                placeholder={
                  param.isNull
                    ? t('queries.explain.nullPlaceholder')
                    : t('queries.explain.paramPlaceholder')
                }
                className="bg-background focus-visible:ring-ring h-8 min-w-0 flex-1 rounded-md border px-2 font-mono text-sm focus-visible:ring-2 focus-visible:outline-none disabled:opacity-50"
                onChange={(event) => onChange(index, { ...param, value: event.target.value })}
              />
              <label className="text-muted-foreground flex shrink-0 items-center gap-1 text-xs">
                <input
                  type="checkbox"
                  checked={param.isNull}
                  aria-label={t('queries.explain.paramIsNull', { index: index + 1 })}
                  onChange={(event) => onChange(index, { ...param, isNull: event.target.checked })}
                />
                NULL
              </label>
            </div>
          )
        })}
      </div>
    </fieldset>
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
  statementText = null,
}: ExplainPanelProps) {
  const t = useT()
  const [confirmationOpen, setConfirmationOpen] = useState(false)
  const count =
    statementText && !isTruncatedQuery(statementText) ? placeholderCount(statementText) : 0
  const [params, setParams] = useState<ParamState[]>([])
  const shown = Array.from(
    { length: count },
    (_, index) => params[index] ?? { value: '', isNull: false },
  )
  const given = shown.filter((param) => param.isNull || param.value !== '').length
  const partial = given > 0 && given < count
  const cancelRef = useRef<HTMLButtonElement>(null)
  const createCommand = useCreateCommand(instanceId)
  const queryID = queryIDText(queryid)
  const canPlan = currentTier === 'T1' || currentTier === 'T2'

  const submit = (analyze: boolean) => {
    if (queryID === null) return
    createCommand.mutate({
      args: {
        analyze,
        ...(datname === undefined ? {} : { datname }),
        ...(given === count && count > 0
          ? { params: shown.map((param) => (param.isNull ? null : param.value)) }
          : {}),
        queryid: queryID,
      },
      kind: 'explain',
      ...(instanceId === undefined ? {} : { instanceId }),
    })
  }

  const command = createCommand.command.data
  // A queued or claimed command may still run. A second run would repeat the
  // same EXPLAIN ANALYZE, so the controls wait until the server ends it.
  const commandOpen = command?.state === 'pending' || command?.state === 'claimed'
  const status = command
    ? describeCommandState(command.state, command.error)
    : createCommand.isPending
      ? t('queries.explain.commandPending')
      : null
  const result =
    command?.state === 'done' || command?.state === 'succeeded' ? command.result : undefined
  const valuesFrom = resultValuesFrom(result)
  const plan = useMemo(
    () => (result === undefined ? null : parsePlan(result, valuesFrom)),
    [result, valuesFrom],
  )
  const planOnlyButton = (
    <Button
      type="button"
      aria-label={t('queries.explain.runPlanOnly')}
      onClick={() => submit(false)}
      disabled={!canPlan || queryID === null || partial || createCommand.isPending || commandOpen}
    >
      {t('queries.explain.planOnly')}
    </Button>
  )
  const analyzeButton = (
    <Button
      type="button"
      variant="outline"
      aria-label={t('queries.explain.planAnalyze')}
      onClick={() => setConfirmationOpen(true)}
      disabled={queryID === null || partial || createCommand.isPending || commandOpen}
    >
      {t('queries.explain.planAnalyze')}
    </Button>
  )

  return (
    <section
      aria-labelledby="explain-panel-title"
      className="bg-card shadow-card space-y-5 rounded-xl border p-5"
      data-card=""
      data-caption-id="queries.explain-panel"
    >
      <header>
        <div className="flex items-center gap-1">
          <h2 id="explain-panel-title" className="text-base font-semibold">
            {t('queries.explain.title')}
          </h2>
          <InfoHint id="queries.explain-panel" label={t('queries.explain.title')} />
          <HelpLink anchor="query-performance#execution-plan" label={t('queries.explain.title')} />
        </div>
        <p className="text-muted-foreground mt-1 text-sm">
          {t('queries.explain.intro', {
            scope: datname ? t('queries.explain.inDatabase', { datname }) : '',
          })}
        </p>
      </header>

      <div className="flex flex-wrap items-start gap-3">
        {canPlan ? (
          planOnlyButton
        ) : (
          <NotPermitted required="T1" current={currentTier}>
            {planOnlyButton}
          </NotPermitted>
        )}
        {currentTier === 'T2' ? (
          analyzeButton
        ) : (
          <NotPermitted required="T2" current={currentTier}>
            {analyzeButton}
          </NotPermitted>
        )}
      </div>

      {count > 0 && (
        <ParameterInputs
          count={count}
          params={shown}
          onChange={(index, next) =>
            setParams(shown.map((param, at) => (at === index ? next : param)))
          }
        />
      )}
      {partial && (
        <p role="alert" className="text-destructive text-sm">
          {t('queries.explain.enterValues', { count })}
        </p>
      )}

      {status && <p role="status">{status}</p>}
      {createCommand.error && (
        <CommandSubmitError
          endpoint="EXPLAIN command"
          failure={createCommand.error.failure}
          onDismiss={() => createCommand.reset()}
        />
      )}
      {result !== undefined &&
        (plan ? (
          <PlanView plan={plan} valuesFrom={valuesFrom} />
        ) : (
          <p className="text-muted-foreground text-sm">{t('queries.explain.noPlan')}</p>
        ))}

      <Dialog open={confirmationOpen} onOpenChange={setConfirmationOpen}>
        <DialogContent
          onOpenAutoFocus={(event) => {
            event.preventDefault()
            cancelRef.current?.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('queries.explain.analyzeTitle')}</DialogTitle>
            <DialogDescription>{t('queries.explain.analyzeText')}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              ref={cancelRef}
              type="button"
              variant="outline"
              onClick={() => setConfirmationOpen(false)}
            >
              {t('queries.explain.cancel')}
            </Button>
            <Button
              type="button"
              onClick={() => {
                setConfirmationOpen(false)
                submit(true)
              }}
            >
              {t('queries.explain.analyzeRun')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}

/** The panel of a query detail page, fed with the statement text it needs for parameters. */
export function QueryExplainPanel(
  props: Omit<ExplainPanelProps, 'statementText' | 'queryid'> & { queryid: string },
) {
  const { statement } = useQueryStatement(props.instanceId, props.queryid, props.datname)
  return <ExplainPanel {...props} statementText={statement?.query_text ?? null} />
}

export default ExplainPanel
