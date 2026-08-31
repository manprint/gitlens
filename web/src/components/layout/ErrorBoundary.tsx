import { Component, type ReactNode } from 'react'

import type { ApiFailure } from '@/api/client'
import { ErrorState } from '@/components/state/ErrorState'

interface ErrorBoundaryProps {
  children: ReactNode
}

interface ErrorBoundaryState {
  error: Error | null
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  override state: ErrorBoundaryState = { error: null }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error }
  }

  override render() {
    if (!this.state.error) return this.props.children

    const failure: ApiFailure = {
      kind: 'malformed',
      message: this.state.error.message,
    }

    return (
      <main className="mx-auto flex min-h-screen max-w-3xl flex-col gap-4 p-6">
        <h1 className="text-xl font-semibold">The application could not render.</h1>
        <ErrorState endpoint="pglens" failure={failure} onRetry={() => window.location.reload()} />
        <p className="text-text-secondary text-sm">Build {__PGLENS_BUILD__}</p>
      </main>
    )
  }
}
