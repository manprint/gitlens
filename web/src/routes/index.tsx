/* eslint-disable react-refresh/only-export-components -- this is the route registry, not a component boundary. */
import { lazy, Suspense, type ComponentType, type ReactElement } from 'react'
import type { RouteObject } from 'react-router-dom'

import App from '@/App'
import LoginPage from '@/features/auth/LoginPage'
import RequireSession from '@/features/auth/RequireSession'

import { NotFoundPage, RouteErrorBoundary } from './eager-pages'

type LazyPage = ComponentType

const FleetOverviewPage = lazy(() =>
  import('./pages').then(({ FleetOverviewPage: Page }) => ({ default: Page })),
)
const ClusterDetailPage = lazy(() =>
  import('./pages').then(({ ClusterDetailPage: Page }) => ({ default: Page })),
)
const InstanceDetailPage = lazy(() =>
  import('./pages').then(({ InstanceDetailPage: Page }) => ({ default: Page })),
)
const AshPage = lazy(() => import('./pages').then(({ AshPage: Page }) => ({ default: Page })))
const QueryInspectorPage = lazy(() =>
  import('./pages').then(({ QueryInspectorPage: Page }) => ({ default: Page })),
)
const QueryDetailPage = lazy(() =>
  import('./pages').then(({ QueryDetailPage: Page }) => ({ default: Page })),
)
const LocksPage = lazy(() => import('./pages').then(({ LocksPage: Page }) => ({ default: Page })))
const FindingsPage = lazy(() =>
  import('./pages').then(({ FindingsPage: Page }) => ({ default: Page })),
)
const AlertsPage = lazy(() => import('./pages').then(({ AlertsPage: Page }) => ({ default: Page })))
const SettingsPage = lazy(() =>
  import('./pages').then(({ SettingsPage: Page }) => ({ default: Page })),
)

interface PageSkeletonProps {
  surface: string
}

function PageSkeleton({ surface }: PageSkeletonProps) {
  return (
    <section aria-busy="true" aria-label={`Loading ${surface}`}>
      <p aria-hidden="true">Loading {surface}…</p>
    </section>
  )
}

function LazyRoute({ page: Page, surface }: { page: LazyPage; surface: string }) {
  return (
    <Suspense fallback={<PageSkeleton surface={surface} />}>
      <Page />
    </Suspense>
  )
}

interface RouteDefinition {
  element: ReactElement
  path: string
  surface: string
}

function lazyRoute(path: string, surface: string, page: LazyPage): RouteDefinition {
  return { element: <LazyRoute page={page} surface={surface} />, path, surface }
}

const loginRoute: RouteDefinition = {
  element: <LoginPage />,
  path: '/login',
  surface: 'Sign in',
}

const dataRoutes: RouteDefinition[] = [
  lazyRoute('/', 'Fleet overview', FleetOverviewPage),
  lazyRoute('/clusters/:clusterId', 'Cluster detail', ClusterDetailPage),
  lazyRoute('/instances/:instanceId', 'Instance detail', InstanceDetailPage),
  lazyRoute('/instances/:instanceId/ash', 'ASH and wait analysis', AshPage),
  lazyRoute('/instances/:instanceId/queries', 'Query inspector', QueryInspectorPage),
  lazyRoute(
    '/instances/:instanceId/queries/:queryid',
    'Query detail and plan history',
    QueryDetailPage,
  ),
  lazyRoute('/instances/:instanceId/locks', 'Locks and activity', LocksPage),
  lazyRoute('/findings', 'Advisor findings', FindingsPage),
  lazyRoute('/alerts', 'Alerts and events', AlertsPage),
  lazyRoute('/settings', 'Settings and inventory', SettingsPage),
]

const notFoundRoute: RouteDefinition = {
  element: <NotFoundPage />,
  path: '*',
  surface: 'Not found',
}

/** The normative phase-7 route table; additions require a STATE deviation. */
export const routeTable: readonly RouteDefinition[] = [loginRoute, ...dataRoutes, notFoundRoute]

const authenticatedChildren: RouteObject[] = [
  ...dataRoutes.map(({ element, path }) =>
    path === '/'
      ? { element, errorElement: <RouteErrorBoundary />, index: true }
      : { element, errorElement: <RouteErrorBoundary />, path: path.slice(1) },
  ),
  { element: notFoundRoute.element, errorElement: <RouteErrorBoundary />, path: '*' },
]

/**
 * The route registry is the single entry point used by the application and by
 * route tests. Data pages are deferred behind the eager shell, login, and
 * route-level recovery UI.
 */
export const routes: RouteObject[] = [
  {
    element: <LoginPage />,
    errorElement: <RouteErrorBoundary />,
    path: '/login',
  },
  {
    children: authenticatedChildren,
    element: (
      <RequireSession>
        <App />
      </RequireSession>
    ),
    errorElement: <RouteErrorBoundary />,
    path: '/',
  },
]
