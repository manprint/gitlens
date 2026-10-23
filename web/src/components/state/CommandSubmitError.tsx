import type { ApiFailure } from '@/api/client'
import { commandQueueFullMessage } from '@/lib/commands'

import { describeFailure } from './describeFailure'

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
  return (
    <div role="alert" className="border-destructive/40 bg-destructive/10 text-sm">
      <strong>Could not submit {endpoint}.</strong>
      <p>{commandQueueFullMessage(failure) ?? describeFailure(failure)}</p>
      <button type="button" onClick={onDismiss}>
        Dismiss
      </button>
    </div>
  )
}
