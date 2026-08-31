import type { ReactElement, ReactNode, ComponentType } from 'react'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, type RenderResult } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider, type RouteObject } from 'react-router-dom'
import { vi } from 'vitest'

import { routes as applicationRoutes } from '@/routes'

export type TestProvider = ComponentType<{ children: ReactNode }>

export interface RenderWithProvidersOptions {
  /** Initial URL for the in-memory router. */
  route?: string
  /** Providers owned by the application, extendable as the shell grows. */
  providers?: readonly TestProvider[]
}

export type TestRenderResult = RenderResult & {
  queryClient: QueryClient
  router: ReturnType<typeof createMemoryRouter>
}

/**
 * The application provider registry is intentionally empty until the shell
 * defines its theme and time-range providers. Tests extend this array rather
 * than introducing ad-hoc wrappers, keeping provider order explicit.
 */
export const applicationProviders: readonly TestProvider[] = []

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: Infinity,
        staleTime: 0,
        refetchOnWindowFocus: false,
      },
      mutations: {
        retry: false,
      },
    },
  })
}

function renderRoutes(
  routeObjects: RouteObject[],
  options: RenderWithProvidersOptions,
): TestRenderResult {
  const queryClient = createTestQueryClient()
  const router = createMemoryRouter(routeObjects, {
    initialEntries: [options.route ?? '/'],
  })

  let tree: ReactNode = <RouterProvider router={router} />
  tree = <QueryClientProvider client={queryClient}>{tree}</QueryClientProvider>

  for (const Provider of options.providers ?? applicationProviders) {
    tree = <Provider>{tree}</Provider>
  }

  return Object.assign(render(tree), { queryClient, router })
}

/** Render an isolated component with a fresh query cache and memory router. */
export function renderWithProviders(
  ui: ReactElement,
  options: RenderWithProvidersOptions = {},
): TestRenderResult {
  return renderRoutes([{ path: '*', element: ui }], options)
}

/** Render the application's route registry at a controlled in-memory URL. */
export function renderRoute(
  path: string,
  options: Omit<RenderWithProvidersOptions, 'route'> = {},
): TestRenderResult {
  return renderRoutes(applicationRoutes, { ...options, route: path })
}

/** Create a user-event instance that advances the suite's fake clock. */
export function user() {
  return userEvent.setup({ advanceTimers: vi.advanceTimersByTime })
}
