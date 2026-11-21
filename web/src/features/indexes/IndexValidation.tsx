import { useMemo } from 'react'

import { useCreateCommand } from '@/api/commands'
import { useInstance } from '@/api/queries'
import { HelpLink } from '@/components/layout/HelpLink'
import { CommandSubmitError, NotPermitted } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useT } from '@/i18n/react'
import { describeCommandState } from '@/lib/commands'
import { formatTimestamp } from '@/lib/format'
import {
  indexCheckArgs,
  validationApproximate,
  validationCostText,
  validationErrorText,
  validationFromResult,
  validationSizeText,
  validationVerdict,
  type IndexSuggestion,
  type IndexValidation,
} from '@/lib/indexSuggestions'

import { CELL_BADGE } from './IndexPanel'

function ValidationResult({ validation }: { validation: IndexValidation }) {
  const t = useT()
  return (
    <div className="space-y-2" data-validation="">
      <dl className="grid gap-x-8 gap-y-1.5 text-sm sm:grid-cols-[auto_1fr_auto_1fr]">
        <dt className="text-muted-foreground">{t('indexes.missing.validate.verdict')}</dt>
        <dd>
          <Badge
            className={CELL_BADGE}
            variant={validation.index_used ? 'success' : 'warning'}
            data-index-used={validation.index_used ? 'true' : 'false'}
          >
            {validationVerdict(validation)}
          </Badge>
        </dd>
        <dt className="text-muted-foreground">{t('indexes.missing.validate.cost')}</dt>
        <dd className="tabular-nums">{validationCostText(validation)}</dd>
        <dt className="text-muted-foreground">{t('indexes.missing.validate.size')}</dt>
        <dd className="tabular-nums">{validationSizeText(validation)}</dd>
        <dt className="text-muted-foreground">{t('indexes.missing.validate.at')}</dt>
        <dd className="tabular-nums">{formatTimestamp(validation.validated_at)}</dd>
        <dt className="text-muted-foreground">{t('indexes.missing.validate.statement')}</dt>
        <dd className="font-mono text-xs">{validation.queryid}</dd>
      </dl>
      {validation.approximate ? (
        <p className="border-warning/40 bg-warning/10 rounded-lg border px-3 py-2 text-sm">
          {t('indexes.missing.validate.approximate')}
        </p>
      ) : null}
    </div>
  )
}

/**
 * Validates a suggestion with hypopg: the agent costs the heaviest statement
 * with a hypothetical index in its own session (index_check). Nothing is
 * created on the database.
 */
export function IndexValidationSection({
  row,
  instanceId,
}: {
  row: IndexSuggestion
  instanceId: string
}) {
  const t = useT()
  const tier = useInstance(instanceId).data?.perm_tier ?? 'T0'
  const canRun = tier === 'T1' || tier === 'T2'
  const args = useMemo(() => indexCheckArgs(row), [row])
  const createCommand = useCreateCommand(instanceId)
  const command = createCommand.command.data
  const commandOpen = command?.state === 'pending' || command?.state === 'claimed'
  const result =
    command?.state === 'done' || command?.state === 'succeeded' ? command.result : undefined
  const approximate = validationApproximate(row)
  const live = useMemo(
    () =>
      result === undefined || args === null
        ? null
        : validationFromResult(result, args.queryid, new Date().toISOString(), approximate),
    [result, args, approximate],
  )
  const validation = live ?? row.validation
  const failed = command?.state === 'failed' || command?.state === 'expired'
  const status = command
    ? commandOpen
      ? describeCommandState(command.state, command.error)
      : null
    : createCommand.isPending
      ? t('indexes.missing.validate.pending')
      : null

  const button = (
    <Button
      type="button"
      variant="outline"
      size="sm"
      aria-label={t('indexes.missing.validate.button')}
      disabled={!canRun || args === null || createCommand.isPending || commandOpen}
      onClick={() => {
        if (args === null) return
        createCommand.mutate({ kind: 'index_check', args, instanceId })
      }}
    >
      {t('indexes.missing.validate.button')}
    </Button>
  )

  return (
    <section aria-label={t('indexes.missing.validate.title')} className="space-y-3">
      <div className="flex items-center gap-1">
        <h4 className="text-sm font-medium">{t('indexes.missing.validate.title')}</h4>
        <HelpLink anchor="indexes#missing-validate" label={t('indexes.missing.validate.title')} />
      </div>
      <p className="text-muted-foreground text-sm">
        {args === null
          ? t('indexes.missing.validate.noStatement')
          : t('indexes.missing.validate.intro', { queryid: args.queryid })}
      </p>
      <div>
        {canRun ? (
          button
        ) : (
          <NotPermitted required="T1" current={tier}>
            {button}
          </NotPermitted>
        )}
      </div>
      {status ? <p role="status">{status}</p> : null}
      {failed ? (
        <p role="alert" className="text-destructive text-sm">
          {command.state === 'failed'
            ? validationErrorText(command.error, row.database)
            : describeCommandState(command.state, command.error)}
        </p>
      ) : null}
      {createCommand.error ? (
        <CommandSubmitError
          endpoint="index_check command"
          failure={createCommand.error.failure}
          onDismiss={() => createCommand.reset()}
        />
      ) : null}
      {validation ? <ValidationResult validation={validation} /> : null}
    </section>
  )
}
