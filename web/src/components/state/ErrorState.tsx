import { CircleAlert, RefreshCw } from 'lucide-react'

import type { ApiFailure } from '@/api/client'
import { Button } from '@/components/ui/button'

import { describeFailure } from './describeFailure'

interface ErrorStateProps {
  failure: ApiFailure
  onRetry: () => void
  endpoint?: string
}

export function ErrorState({ failure, onRetry, endpoint = 'API' }: ErrorStateProps) {
  return (
    <div
      role="alert"
      className="border-destructive/30 bg-destructive/5 flex items-start gap-3 rounded-xl border p-4 text-sm"
    >
      <CircleAlert aria-hidden="true" className="text-destructive mt-0.5 size-5 shrink-0" />
      <div className="min-w-0 flex-1 space-y-1">
        <strong className="text-foreground block font-semibold">Could not load {endpoint}.</strong>
        <p className="text-muted-foreground">{describeFailure(failure)}</p>
        <Button className="mt-2" onClick={onRetry} size="sm" type="button" variant="outline">
          <RefreshCw aria-hidden="true" />
          Retry {endpoint}
        </Button>
      </div>
    </div>
  )
}
