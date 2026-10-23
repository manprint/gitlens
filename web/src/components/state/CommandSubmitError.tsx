import type { ApiFailure } from '@/api/client'
import { commandQueueFullMessage } from '@/lib/commands'

import { ErrorState } from './ErrorState'

interface CommandSubmitErrorProps {
  endpoint: string
  failure: ApiFailure
  onRetry: () => void
}

/**
 * A command the server refused to queue. A full agent queue gets its own text
 * with the server's count; every other failure is the generic error.
 */
export function CommandSubmitError({ endpoint, failure, onRetry }: CommandSubmitErrorProps) {
  const queueFull = commandQueueFullMessage(failure)
  if (queueFull === null) {
    return <ErrorState endpoint={endpoint} failure={failure} onRetry={onRetry} />
  }
  return (
    <div role="alert" className="border-destructive/40 bg-destructive/10 text-sm">
      <strong>Could not submit {endpoint}.</strong>
      <p>{queueFull}</p>
    </div>
  )
}
