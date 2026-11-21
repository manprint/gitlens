import { Link, useLocation, useRouteError } from 'react-router-dom'

import type { ApiFailure } from '@/api/client'
import { ErrorState } from '@/components/state'
import { useT } from '@/i18n/react'

export function RouteErrorBoundary() {
  const location = useLocation()
  const t = useT()
  const routeError = useRouteError()
  const message = routeError instanceof Error ? routeError.message : t('auth.route.unexpected')
  const failure: ApiFailure = { kind: 'client', message }
  const routePath = location.pathname || '/'

  return (
    <section className="mx-auto max-w-2xl space-y-4 py-10">
      <h1 className="text-foreground text-2xl font-semibold tracking-tight">
        {t('auth.route.error')}
      </h1>
      <ErrorState endpoint={routePath} failure={failure} onRetry={() => window.location.reload()} />
    </section>
  )
}

export function NotFoundPage() {
  const t = useT()
  return (
    <section className="flex flex-col items-center justify-center py-24 text-center">
      <p className="text-primary text-sm font-semibold">404</p>
      <h1 className="text-foreground mt-2 text-2xl font-semibold tracking-tight">
        {t('auth.route.notFoundTitle')}
      </h1>
      <p className="text-muted-foreground mt-2">{t('auth.route.notFoundBody')}</p>
      <Link
        className="bg-primary text-primary-foreground hover:bg-primary/90 mt-6 inline-flex h-9 items-center rounded-md px-4 text-sm font-medium"
        to="/"
      >
        {t('auth.route.backFleet')}
      </Link>
    </section>
  )
}
