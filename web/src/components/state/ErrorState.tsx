import type { ApiFailure } from '@/api/client'

import { describeFailure } from './describeFailure'

interface ErrorStateProps {
  failure: ApiFailure
  onRetry: () => void
  endpoint?: string
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
