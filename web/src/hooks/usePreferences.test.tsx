import { focusManager, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { effectiveRefresh, REFRESH, type RefreshSurface } from '@/api/policy'
import { useClusters } from '@/api/queries'
import { DataTable } from '@/components/layout/DataTable'
import { FreshnessBadge } from '@/components/layout/FreshnessBadge'
import { updatePreferences } from '@/lib/preferences'
import { base as clusters, empty as emptyClusters } from '@/test/fixtures/getClusters'
import { ok, sequence } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'
import { NOW } from '@/test/time'

import { useTimeRange } from './useTimeRange'

function RangeProbe() {
  const { range } = useTimeRange()
  return (
    <output data-testid="range">{range.kind === 'relative' ? range.preset : 'absolute'}</output>
  )
}

function ClustersProbe() {
  const query = useClusters()
  return <output data-testid="clusters">{query.data?.length ?? 'loading'}</output>
}

const ROWS = Array.from({ length: 120 }, (_, index) => ({ name: `row-${index + 1}` }))

function Table({ local = false }: { local?: boolean }) {
  return (
    <DataTable
      ariaLabel="Rows"
      columns={[{ accessorKey: 'name', header: 'Name' }]}
      data={ROWS}
      emptyState={<p>No rows</p>}
      {...(local ? {} : { tableId: 'rows' })}
    />
  )
}

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: state })
  document.dispatchEvent(new Event('visibilitychange'))
}

function bodyRowCount() {
  return within(screen.getByTestId('data-table-rows')).getAllByRole('row').length
}

/** Count the cluster list requests the mocked server receives. */
function countClusterRequests() {
  let count = 0
  const listener = ({ request }: { request: Request }) => {
    if (new URL(request.url).pathname === '/api/v1/clusters') count += 1
  }
  server.events.on('request:start', listener)
  return {
    get count() {
      return count
    },
    stop: () => server.events.removeListener('request:start', listener),
  }
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

beforeEach(() => localStorage.clear())
afterEach(() => localStorage.clear())

describe('console preferences in the running app', () => {
  it('PREF-005 useTimeRange opens on the preferred range only when the URL names none', () => {
    updatePreferences({ defaultTimeRange: '6h' })

    const preferred = renderWithProviders(<RangeProbe />)
    expect(screen.getByTestId('range')).toHaveTextContent('6h')
    // A change applies at once, without a reload.
    act(() => void updatePreferences({ defaultTimeRange: '7d' }))
    expect(screen.getByTestId('range')).toHaveTextContent('7d')
    preferred.unmount()

    // A range in the URL wins, including 15m that the preference does not offer (P2).
    const fromUrl = renderWithProviders(<RangeProbe />, { route: '/?range=15m' })
    expect(screen.getByTestId('range')).toHaveTextContent('15m')
    fromUrl.unmount()

    // An invalid range in the URL falls back to the preferred range too.
    const invalid = renderWithProviders(<RangeProbe />, { route: '/?range=2y' })
    expect(screen.getByTestId('range')).toHaveTextContent('7d')
    invalid.unmount()

    localStorage.clear()
    renderWithProviders(<RangeProbe />)
    expect(screen.getByTestId('range')).toHaveTextContent('1h')
  })

  it('PREF-006 the refresh interval preference changes the query refetch interval', async () => {
    const requests = countClusterRequests()
    updatePreferences({ refreshInterval: '60s' })
    server.use(sequence('getClusters', clusters, emptyClusters))
    renderWithProviders(<ClustersProbe />)
    await advance(0)
    expect(screen.getByTestId('clusters')).toHaveTextContent(String(clusters.length))
    expect(requests.count).toBe(1)

    // The fleet policy polls every 15 s; the preference moves it to 60 s.
    await advance(REFRESH.fleet.interval)
    expect(requests.count).toBe(1)
    await advance(60_000 - REFRESH.fleet.interval)
    expect(requests.count).toBe(2)

    // Back to auto: the policy's 15 s applies at once.
    act(() => void updatePreferences({ refreshInterval: 'auto' }))
    await advance(REFRESH.fleet.interval)
    expect(requests.count).toBe(3)
    requests.stop()
  })

  it('PREF-006 off stops the polling of the surface at once', async () => {
    const requests = countClusterRequests()
    server.use(sequence('getClusters', clusters, emptyClusters))
    renderWithProviders(<ClustersProbe />)
    await advance(0)
    await advance(REFRESH.fleet.interval)
    expect(requests.count).toBe(2)

    act(() => void updatePreferences({ refreshInterval: 'off' }))
    await advance(10 * REFRESH.fleet.interval)
    expect(requests.count).toBe(2)
    requests.stop()
  })

  it('PREF-007 the page size preference sets the default page size of the tables; a URL size wins', () => {
    updatePreferences({ tablePageSize: 50 })

    const preferred = renderWithProviders(<Table />)
    expect(screen.getByRole('combobox', { name: 'Rows per page' })).toHaveValue('50')
    expect(bodyRowCount()).toBe(50)
    act(() => void updatePreferences({ tablePageSize: 100 }))
    expect(screen.getByRole('combobox', { name: 'Rows per page' })).toHaveValue('100')
    expect(bodyRowCount()).toBe(100)
    preferred.unmount()

    // P3: the URL size wins over the preference.
    const fromUrl = renderWithProviders(<Table />, { route: '/?rows.size=20' })
    expect(screen.getByRole('combobox', { name: 'Rows per page' })).toHaveValue('20')
    expect(bodyRowCount()).toBe(20)
    fromUrl.unmount()

    // A table without a URL id follows a change at once, until its own size is picked.
    renderWithProviders(<Table local />)
    expect(screen.getByRole('combobox', { name: 'Rows per page' })).toHaveValue('100')
    act(() => void updatePreferences({ tablePageSize: 50 }))
    expect(screen.getByRole('combobox', { name: 'Rows per page' })).toHaveValue('50')
    expect(bodyRowCount()).toBe(50)
    fireEvent.change(screen.getByRole('combobox', { name: 'Rows per page' }), {
      target: { value: '20' },
    })
    expect(bodyRowCount()).toBe(20)
    act(() => void updatePreferences({ tablePageSize: 100 }))
    expect(screen.getByRole('combobox', { name: 'Rows per page' })).toHaveValue('20')
    expect(bodyRowCount()).toBe(20)
  })

  it('PREF-010 off also stops the refetch on window focus and on return to the tab; auto keeps it', async () => {
    const requests = countClusterRequests()
    server.use(ok('getClusters', clusters))
    // The application's client: React Query's default refetch on window focus.
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={client}>
        <ClustersProbe />
      </QueryClientProvider>,
    )
    try {
      await advance(0)
      expect(requests.count).toBe(1)

      // auto: a focus event refetches (today's behaviour).
      await act(async () => {
        focusManager.setFocused(false)
        focusManager.setFocused(true)
        await vi.advanceTimersByTimeAsync(0)
      })
      expect(requests.count).toBe(2)

      // off: neither a focus event nor a return to the tab refetches.
      act(() => void updatePreferences({ refreshInterval: 'off' }))
      await act(async () => {
        focusManager.setFocused(false)
        focusManager.setFocused(true)
        await vi.advanceTimersByTimeAsync(0)
      })
      await act(async () => {
        setVisibility('hidden')
        await vi.advanceTimersByTimeAsync(0)
        setVisibility('visible')
        await vi.advanceTimersByTimeAsync(0)
      })
      await advance(10 * REFRESH.fleet.interval)
      expect(requests.count).toBe(2)
    } finally {
      focusManager.setFocused(undefined)
      setVisibility('visible')
      requests.stop()
      client.clear()
    }
  })

  it('PREF-009 a chosen interval scales staleAfter like the policy; off sets it to 0 and hides the stale badge', () => {
    const kept: RefreshSurface[] = ['command', 'locks', 'activity', 'static']
    for (const surface of Object.keys(REFRESH) as RefreshSurface[]) {
      const policy = REFRESH[surface]
      expect(effectiveRefresh(policy, 'auto'), surface).toBe(policy)
      for (const preference of ['off', '15s', '30s', '60s'] as const) {
        const timing = effectiveRefresh(policy, preference)
        if (kept.includes(surface)) {
          expect(timing, `${surface} ${preference}`).toBe(policy)
          continue
        }
        if (preference === 'off') {
          expect(timing, surface).toEqual({ interval: 0, staleAfter: 0 })
          continue
        }
        const interval = { '15s': 15_000, '30s': 30_000, '60s': 60_000 }[preference]
        expect(timing.interval, `${surface} ${preference}`).toBe(interval)
        // Same ratio as the policy.
        expect(timing.staleAfter / timing.interval, `${surface} ${preference}`).toBeCloseTo(
          policy.staleAfter / policy.interval,
        )
      }
    }
    // Every surface not kept polls at 15 s or slower.
    for (const surface of Object.keys(REFRESH) as RefreshSurface[]) {
      if (!kept.includes(surface)) expect(REFRESH[surface].interval).toBeGreaterThanOrEqual(15_000)
    }
    expect(effectiveRefresh(REFRESH.fleet, '60s')).toEqual({
      interval: 60_000,
      staleAfter: 180_000,
    })

    // The badge: 100 s old fleet data is stale under the policy (45 s), fresh at 60 s
    // (threshold 180 s), and never stale with off.
    const updatedAt = NOW.getTime() - 100_000
    const view = render(<FreshnessBadge dataUpdatedAt={updatedAt} policy="fleet" />)
    expect(screen.getByRole('status', { name: /Stale data/ })).toBeInTheDocument()
    act(() => void updatePreferences({ refreshInterval: '60s' }))
    expect(screen.queryByRole('status', { name: /Stale data/ })).toBeNull()
    expect(screen.getByRole('status', { name: /Data freshness/ })).toBeInTheDocument()
    view.rerender(<FreshnessBadge dataUpdatedAt={NOW.getTime() - 3_600_000} policy="fleet" />)
    expect(screen.getByRole('status', { name: /Stale data/ })).toBeInTheDocument()
    act(() => void updatePreferences({ refreshInterval: 'off' }))
    expect(screen.queryByRole('status', { name: /Stale data/ })).toBeNull()
  })
})
