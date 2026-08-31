import type { ApiFailure } from '@/api/client'

interface ErrorStateProps {
  failure: ApiFailure
  onRetry: () => void
  endpoint?: string
}

function describeFailure(failure: ApiFailure): string {
  switch (failure.kind) {
    case 'unauthorized':
      return 'Authentication is required.'
    case 'forbidden':
      return 'The current session is not permitted.'
    case 'not_found':
      return 'The requested resource was not found.'
    case 'unprocessable':
      return `${failure.error}: ${failure.detail}`
    case 'server':
      return `Server returned ${failure.status}: ${failure.error}: ${failure.detail}`
    case 'network':
      return `The server could not be reached: ${failure.message}`
    case 'malformed':
      return `The server returned an invalid response: ${failure.message}`
    default: {
      const exhaustive: never = failure
      return exhaustive
    }
  }
}

export function ErrorState({ failure, onRetry, endpoint = 'API' }: ErrorStateProps) {
  return (
    <div role="alert" className="border-destructive/40 bg-destructive/10 text-sm">
      <strong>Could not load {endpoint}.</strong>
      <p>{describeFailure(failure)}</p>
      <button type="button" onClick={onRetry}>
        Retry {endpoint}
      </button>
    </div>
  )
}
