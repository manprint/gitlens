import { Link, useLocation, useRouteError } from 'react-router-dom'

import type { ApiFailure } from '@/api/client'
import { ErrorState } from '@/components/state'

export function RouteErrorBoundary() {
  const location = useLocation()
  const routeError = useRouteError()
  const message = routeError instanceof Error ? routeError.message : 'Unexpected route error.'
  const failure: ApiFailure = { kind: 'malformed', message }
  const routePath = location.pathname || '/'

  return (
    <section>
      <h1>Route error</h1>
      <ErrorState endpoint={routePath} failure={failure} onRetry={() => window.location.reload()} />
    </section>
  )
}

export function NotFoundPage() {
  return (
    <section>
      <h1>Page not found</h1>
      <p>The requested page does not exist.</p>
      <Link to="/">Back to fleet overview</Link>
    </section>
  )
}
