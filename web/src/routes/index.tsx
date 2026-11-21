/* eslint-disable react-refresh/only-export-components -- this is the route registry, not a component boundary. */
import { lazy, Suspense, type ComponentType, type ReactElement } from 'react'
import type { RouteObject } from 'react-router-dom'

import App from '@/App'
import LoginPage from '@/features/auth/LoginPage'
import RequireSession from '@/features/auth/RequireSession'
import { messages as english, type MessageKey } from '@/i18n/messages/en'
import { useT } from '@/i18n/react'

import { NotFoundPage, RouteErrorBoundary } from './eager-pages'

type LazyPage = ComponentType

const FleetOverviewPage = lazy(() =>
  import('./pages').then(({ FleetOverviewPage: Page }) => ({ default: Page })),
)
const SlowQueriesPage = lazy(() =>
  import('./pages').then(({ SlowQueriesPage: Page }) => ({ default: Page })),
)
const IndexAnalysisPage = lazy(() =>
  import('./pages').then(({ IndexAnalysisPage: Page }) => ({ default: Page })),
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
const WorkbookPage = lazy(() => import('@/features/workbook/WorkbookPage'))
const LocksPage = lazy(() => import('./pages').then(({ LocksPage: Page }) => ({ default: Page })))
const LogsPage = lazy(() => import('./pages').then(({ LogsPage: Page }) => ({ default: Page })))
const FindingsPage = lazy(() =>
  import('./pages').then(({ FindingsPage: Page }) => ({ default: Page })),
)
const AlertsPage = lazy(() => import('./pages').then(({ AlertsPage: Page }) => ({ default: Page })))
const EventsPage = lazy(() => import('./pages').then(({ EventsPage: Page }) => ({ default: Page })))
const RulesPage = lazy(() => import('./pages').then(({ RulesPage: Page }) => ({ default: Page })))
const SilencesPage = lazy(() =>
  import('./pages').then(({ SilencesPage: Page }) => ({ default: Page })),
)
const HelpPage = lazy(() => import('./pages').then(({ HelpPage: Page }) => ({ default: Page })))
const SettingsPage = lazy(() =>
  import('./pages').then(({ SettingsPage: Page }) => ({ default: Page })),
)

interface PageSkeletonProps {
  surface: string
}

function PageSkeleton({ surface }: PageSkeletonProps) {
  const t = useT()
  return (
    <section
      aria-busy="true"
      aria-label={t('auth.route.loading', { surface })}
      className="animate-pulse space-y-6"
    >
      <div className="space-y-2">
        <div className="bg-muted h-7 w-64 rounded-md" />
        <div className="bg-muted h-4 w-96 max-w-full rounded-md" />
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[0, 1, 2, 3].map((tile) => (
          <div key={tile} className="bg-muted h-24 rounded-xl" />
        ))}
      </div>
      <div className="bg-muted h-80 rounded-xl" />
      <p className="sr-only">{t('auth.route.loadingDots', { surface })}</p>
    </section>
  )
}

function LazyRoute({ page: Page, surfaceKey }: { page: LazyPage; surfaceKey: MessageKey }) {
  const t = useT()
  return (
    <Suspense fallback={<PageSkeleton surface={t(surfaceKey)} />}>
      <Page />
    </Suspense>
  )
}

interface RouteDefinition {
  element: ReactElement
  path: string
  /** English name, for route tests; the console shows `surfaceKey`. */
  surface: string
  surfaceKey: MessageKey
}

function lazyRoute(path: string, surfaceKey: MessageKey, page: LazyPage): RouteDefinition {
  return {
    element: <LazyRoute page={page} surfaceKey={surfaceKey} />,
    path,
    surface: english[surfaceKey],
    surfaceKey,
  }
}

const loginRoute: RouteDefinition = {
  element: <LoginPage />,
  path: '/login',
  surface: english['auth.surface.signIn'],
  surfaceKey: 'auth.surface.signIn',
}

const dataRoutes: RouteDefinition[] = [
  lazyRoute('/', 'auth.surface.fleet', FleetOverviewPage),
  lazyRoute('/queries', 'auth.surface.slowQueries', SlowQueriesPage),
  lazyRoute('/indexes', 'auth.surface.indexes', IndexAnalysisPage),
  lazyRoute('/clusters/:clusterId', 'auth.surface.cluster', ClusterDetailPage),
  lazyRoute('/instances/:instanceId', 'auth.surface.instance', InstanceDetailPage),
  lazyRoute('/instances/:instanceId/ash', 'auth.surface.ash', AshPage),
  lazyRoute('/instances/:instanceId/queries', 'auth.surface.queryInspector', QueryInspectorPage),
  lazyRoute('/instances/:instanceId/queries/:queryid', 'auth.surface.queryDetail', QueryDetailPage),
  lazyRoute(
    '/instances/:instanceId/queries/:queryid/workbooks/:workbookId',
    'auth.surface.workbook',
    WorkbookPage,
  ),
  lazyRoute('/instances/:instanceId/locks', 'auth.surface.locks', LocksPage),
  lazyRoute('/instances/:instanceId/logs', 'auth.surface.logs', LogsPage),
  lazyRoute('/findings', 'auth.surface.findings', FindingsPage),
  lazyRoute('/alerts', 'auth.surface.alerts', AlertsPage),
  lazyRoute('/events', 'auth.surface.events', EventsPage),
  lazyRoute('/alerts/rules', 'auth.surface.rules', RulesPage),
  lazyRoute('/alerts/silences', 'auth.surface.silences', SilencesPage),
  lazyRoute('/settings', 'auth.surface.settings', SettingsPage),
  lazyRoute('/help', 'auth.surface.help', HelpPage),
  lazyRoute('/help/:topicId', 'auth.surface.helpTopic', HelpPage),
]

const notFoundRoute: RouteDefinition = {
  element: <NotFoundPage />,
  path: '*',
  surface: english['auth.surface.notFound'],
  surfaceKey: 'auth.surface.notFound',
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
