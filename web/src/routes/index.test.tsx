import { act, render, screen } from '@testing-library/react'
import type { ReactElement } from 'react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { routeTable } from './index'
import { NotFoundPage, RouteErrorBoundary } from './eager-pages'
import { renderRoute } from '@/test/render'
import { base as session } from '@/test/fixtures/getSession'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'

describe('application route registry', () => {
  it('UI-SHELL-001 every route in table resolves to an element', () => {
    expect(routeTable.map(({ path }) => path)).toEqual([
      '/login',
      '/',
      '/clusters/:clusterId',
      '/instances/:instanceId',
      '/instances/:instanceId/ash',
      '/instances/:instanceId/queries',
      '/instances/:instanceId/queries/:queryid',
      '/instances/:instanceId/locks',
      '/findings',
      '/alerts',
      '/alerts/rules',
      '/alerts/silences',
      '/settings',
      '*',
    ])

    for (const route of routeTable) {
      expect(route.element).toBeTruthy()
    }
  })

  it('UI-SHELL-002 unknown path renders not found', async () => {
    server.use(ok('getSession', session))
    renderRoute('/does-not-exist')

    await act(async () => {
      await import('./pages')
      await vi.advanceTimersByTimeAsync(1)
      for (let index = 0; index < 10; index += 1) {
        await Promise.resolve()
      }
    })

    expect(screen.getByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Back to fleet overview' })).toHaveAttribute(
      'href',
      '/',
    )
  })

  it('UI-SHELL-003 throwing page renders ErrorState rather than blank', () => {
    function ThrowingPage(): ReactElement {
      throw new Error('render exploded')
    }

    vi.mocked(console.error).mockImplementation(() => undefined)
    const router = createMemoryRouter(
      [
        {
          element: <ThrowingPage />,
          errorElement: <RouteErrorBoundary />,
          path: '/broken',
        },
      ],
      { initialEntries: ['/broken'] },
    )

    render(<RouterProvider router={router} />)

    expect(screen.getByRole('alert')).toHaveTextContent('Could not load /broken.')
    expect(screen.getByRole('alert')).toHaveTextContent('render exploded')
    expect(screen.getByRole('button', { name: 'Retry /broken' })).toBeInTheDocument()
  })

  it('keeps the eager not-found page linkable in isolation', () => {
    const router = createMemoryRouter([{ element: <NotFoundPage />, path: '*' }], {
      initialEntries: ['/missing'],
    })

    render(<RouterProvider router={router} />)

    expect(screen.getByRole('link', { name: 'Back to fleet overview' })).toHaveAttribute(
      'href',
      '/',
    )
  })
})
