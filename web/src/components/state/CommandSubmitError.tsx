import type { ApiFailure } from '@/api/client'
import { commandQueueFullMessage } from '@/lib/commands'

import { describeFailure } from './describeFailure'
import { Button } from '@/components/ui/button'
import { Callout } from '@/components/ui/callout'
import { useT } from '@/i18n/react'

interface CommandSubmitErrorProps {
  endpoint: string
  failure: ApiFailure
  onDismiss: () => void
}

/**
 * A command the server refused to queue. The command was submitted and
 * refused, not loaded, so every failure says so; a full agent queue gets its
 * own text with the server's count. Dismissing clears the error, and the
 * action can be submitted again.
 */
export function CommandSubmitError({ endpoint, failure, onDismiss }: CommandSubmitErrorProps) {
  const t = useT()
  return (
    <Callout variant="destructive">
      <strong>{t('shell.state.couldNotSubmit', { endpoint })}</strong>
      <p>{commandQueueFullMessage(failure) ?? describeFailure(failure)}</p>
      <Button size="sm" variant="outline" type="button" onClick={onDismiss}>
        {t('shell.state.dismiss')}
      </Button>
    </Callout>
  )
}
