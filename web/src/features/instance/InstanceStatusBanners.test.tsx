import { act, screen, within } from '@testing-library/react'
import { http } from 'msw/http'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AppShell } from '@/components/layout/AppShell'
import { base as clusters } from '@/test/fixtures/getClusters'
import { complete, base as coverage } from '@/test/fixtures/getCoverage'
import { base as readinessFull, partial as readinessPartial } from '@/test/fixtures/getReadiness'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { instanceStatusRoute } from './InstanceStatusBanners'

describe('instanceStatusRoute', () => {
  it('INSTBAN-001 matches exactly the four instance dashboards', () => {
    expect(instanceStatusRoute('/instances/node-1')).toBe('node-1')
    expect(instanceStatusRoute('/instances/node-1/ash')).toBe('node-1')
    expect(instanceStatusRoute('/instances/node-1/queries')).toBe('node-1')
    expect(instanceStatusRoute('/instances/node-1/locks')).toBe('node-1')
    expect(instanceStatusRoute('/instances/node%201/ash')).toBe('node 1')
    // A trailing slash reaches the same page.
    expect(instanceStatusRoute('/instances/node-1/')).toBe('node-1')
    expect(instanceStatusRoute('/instances/node-1/locks/')).toBe('node-1')
    // React Router matches the routes case-insensitively; the id is kept as written.
    expect(instanceStatusRoute('/Instances/Node-1/ASH')).toBe('Node-1')
    // An id that does not decode names no instance.
    expect(instanceStatusRoute('/instances/%E0%A4%A/ash')).toBeNull()
    expect(instanceStatusRoute('/instances/%/queries')).toBeNull()

    for (const pathname of [
      '/instances/x/queries/123',
      '/instances',
      '/instances/',
      '/instances/x/other',
      '/instances/x/ash/more',
      '/instances/x//ash',
      '/instances/x/ash//',
      '/clusters/1',
      '/settings',
      '/findings',
      '/queries',
      '/',
    ]) {
      expect(instanceStatusRoute(pathname), pathname).toBeNull()
    }
  })
})

/** A matchMedia that answers `(min-width: Npx)` for a viewport width the test can change. */
function viewportWidth(initial: number) {
  let width = initial
  const listeners = new Set<() => void>()
  vi.stubGlobal('matchMedia', (query: string) => ({
    get matches() {
      return width >= Number.parseInt(/min-width:\s*(\d+)px/.exec(query)?.[1] ?? '', 10)
    },
    media: query,
    addEventListener: (_: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_: string, listener: () => void) => listeners.delete(listener),
  }))
  return {
    resize(next: number) {
      width = next
      for (const listener of listeners) listener()
    },
  }
}

/** Serves the coverage fixture and records each request's instance id. */
function countCoverageRequests(): string[] {
  const asked: string[] = []
  server.use(
    http.get('*/api/v1/instances/:id/coverage', ({ params }) => {
      asked.push(String(params.id))
      return Response.json(coverage)
    }),
  )
  return asked
}

describe('InstanceStatusBanners in the application shell', () => {
  beforeEach(() => {
    server.use(
      ok('getClusters', clusters),
      ok('getCoverage', coverage),
      ok('getReadiness', readinessFull),
    )
  })

  it('INSTBAN-002 renders the banner container above the page on an instance dashboard', async () => {
    renderWithProviders(<AppShell />, { route: '/instances/node-1/ash' })
    await flushNetwork()

    const main = screen.getByRole('main')
    const container = within(main).getByTestId('instance-status-banners')
    expect(within(container).getByTestId('coverage-banner')).toHaveTextContent(
      '2 of 4 checks have a current result',
    )
  })

  it('INSTBAN-002 leaves the container empty on an instance dashboard once coverage is complete', async () => {
    server.use(ok('getCoverage', complete))
    renderWithProviders(<AppShell />, { route: '/instances/node-1' })
    await flushNetwork()

    const container = within(screen.getByRole('main')).getByTestId('instance-status-banners')
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByTestId('coverage-banner')).not.toBeInTheDocument()
  })

  describe('from 1600 px', () => {
    afterEach(() => {
      vi.unstubAllGlobals()
    })

    it('INSTBAN-003 puts the banner in the header row, once, with no height above the page', async () => {
      viewportWidth(1920)
      const asked = countCoverageRequests()
      renderWithProviders(<AppShell />, { route: '/instances/node-1/queries' })
      await flushNetwork()

      const header = screen.getByRole('banner')
      const slot = within(header).getByTestId('header-status')
      const container = within(slot).getByTestId('instance-status-banners')
      expect(container).toHaveAttribute('data-placement', 'header')
      const banner = within(container).getByTestId('coverage-banner')
      // The count is its own span that never truncates; the rest of the
      // sentence truncates, with the whole text as the title of that line.
      expect(within(banner).getByTestId('coverage-count')).toHaveTextContent('2/4 checks')
      expect(within(banner).getByTestId('coverage-count')).toHaveClass('shrink-0')
      const line = within(banner).getByTestId('coverage-line')
      expect(line).toHaveClass('truncate')
      expect(line).toHaveTextContent('Failed on its last run: locks.')
      expect(line).toHaveAttribute(
        'title',
        expect.stringMatching(
          /^Collecting data: 2 of 4 checks have a current result.* Failed on its last run: locks\.$/,
        ),
      )
      expect(banner).not.toHaveAttribute('title')

      // Nothing above the page, one live region, one request.
      expect(within(screen.getByRole('main')).queryByTestId('instance-status-banners')).toBeNull()
      expect(screen.getAllByTestId('coverage-banner')).toHaveLength(1)
      expect(
        screen.getAllByRole('status').filter((node) => node.matches('[aria-live="polite"]')),
      ).toHaveLength(1)
      expect(asked).toEqual(['node-1'])

      // The slot sits after the breadcrumbs in the left group, clipped with
      // room for a focus ring: the connection and data-age controls keep their
      // own group, which does not shrink.
      const start = within(header).getByTestId('header-start')
      const controls = within(header).getByTestId('header-controls')
      expect(within(start).getByRole('navigation', { name: 'Breadcrumb' })).toBeInTheDocument()
      expect(within(start).getByTestId('header-status')).toBe(slot)
      expect(slot).toHaveClass('min-w-0', 'flex-1', 'overflow-x-clip', 'p-[3px]')
      expect(within(start).queryByTestId('connection-indicator')).toBeNull()
      expect(within(controls).getByTestId('connection-indicator')).toBeInTheDocument()
      expect(controls).toHaveClass('xl:shrink-0')
      expect(within(header).getByTestId('header-row')).toHaveClass('xl:flex-nowrap')
    })

    it('INSTBAN-003 moves the banner across 1600 px without a remount or a new request', async () => {
      const viewport = viewportWidth(1920)
      const asked = countCoverageRequests()
      renderWithProviders(<AppShell />, { route: '/instances/node-1/queries' })
      await flushNetwork()
      const banner = screen.getByTestId('coverage-banner')
      expect(within(screen.getByRole('banner')).getByTestId('coverage-banner')).toBe(banner)

      act(() => viewport.resize(1440))
      await flushNetwork()
      expect(within(screen.getByRole('banner')).queryByTestId('header-status')).toBeNull()
      const container = within(screen.getByRole('main')).getByTestId('instance-status-banners')
      expect(container).toHaveAttribute('data-placement', 'page')
      // The same live region, now in its page form.
      expect(within(container).getByTestId('coverage-banner')).toBe(banner)
      expect(banner).toHaveTextContent('Collecting data: 2 of 4 checks have a current result')

      act(() => viewport.resize(1920))
      await flushNetwork()
      expect(within(screen.getByRole('banner')).getByTestId('coverage-banner')).toBe(banner)
      expect(screen.getAllByTestId('coverage-banner')).toHaveLength(1)
      expect(asked).toEqual(['node-1'])
    })

    it('INSTBAN-004 readiness and coverage banners: one live region and one request each, moved across 1600 px without remount', async () => {
      const viewport = viewportWidth(1920)
      const readinessAsked: string[] = []
      server.use(
        http.get('*/api/v1/instances/:id/readiness', ({ params }) => {
          readinessAsked.push(String(params.id))
          return Response.json(readinessPartial)
        }),
      )
      const coverageAsked = countCoverageRequests()
      renderWithProviders(<AppShell />, { route: '/instances/node-1/queries' })
      await flushNetwork()

      const header = screen.getByRole('banner')
      const readiness = within(header).getByTestId('readiness-banner')
      const coverageBanner = within(header).getByTestId('coverage-banner')
      expect(within(readiness).getByRole('link')).toHaveTextContent('Partial · 1 warning')
      expect(screen.getAllByTestId('readiness-banner')).toHaveLength(1)
      expect(
        screen.getAllByRole('status').filter((node) => node.matches('[aria-live="polite"]')),
      ).toHaveLength(2)
      expect(readinessAsked).toEqual(['node-1'])
      expect(coverageAsked).toEqual(['node-1'])

      act(() => viewport.resize(1440))
      await flushNetwork()
      const container = within(screen.getByRole('main')).getByTestId('instance-status-banners')
      expect(within(container).getByTestId('readiness-banner')).toBe(readiness)
      expect(within(container).getByTestId('coverage-banner')).toBe(coverageBanner)

      act(() => viewport.resize(1920))
      await flushNetwork()
      expect(within(screen.getByRole('banner')).getByTestId('readiness-banner')).toBe(readiness)
      expect(readinessAsked).toEqual(['node-1'])
      expect(coverageAsked).toEqual(['node-1'])
    })

    it('INSTBAN-003 keeps the banner above the page below 1600 px', async () => {
      viewportWidth(1440)
      renderWithProviders(<AppShell />, { route: '/instances/node-1/queries' })
      await flushNetwork()

      expect(within(screen.getByRole('banner')).queryByTestId('header-status')).toBeNull()
      const container = within(screen.getByRole('main')).getByTestId('instance-status-banners')
      expect(container).toHaveAttribute('data-placement', 'page')
      expect(within(container).getByTestId('coverage-banner')).toBeInTheDocument()
    })

    it('INSTBAN-003 leaves the header without a status slot off the instance dashboards', async () => {
      viewportWidth(1920)
      renderWithProviders(<AppShell />, { route: '/findings' })
      await flushNetwork()

      expect(screen.queryByTestId('header-status')).toBeNull()
    })
  })

  it('INSTBAN-002 renders no banner container and asks for no coverage on other pages', async () => {
    const asked: string[] = []
    server.use(
      http.get('*/api/v1/instances/:id/coverage', ({ params }) => {
        asked.push(String(params.id))
        return new Response(null, { status: 500 })
      }),
    )
    renderWithProviders(<AppShell />, { route: '/findings' })
    await flushNetwork()

    expect(screen.queryByTestId('instance-status-banners')).not.toBeInTheDocument()
    expect(asked).toEqual([])
  })
})
