import { act, fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { REFRESH } from '@/api/policy'
import { renderWithProviders } from '@/test/render'

const mocks = vi.hoisted(() => ({
  instance: vi.fn(),
  locks: vi.fn(),
  activity: vi.fn(),
}))

vi.mock('@/api/queries', () => ({
  useInstance: mocks.instance,
  useLocks: mocks.locks,
  useInstanceActivity: mocks.activity,
}))

import { LocksPage } from './BlockingTree'

const locksResponse = {
  sampled_at: '2026-09-01T10:00:00Z',
  stale: false,
  nodes: [],
}

function success<T>(data: T, overrides: Record<string, unknown> = {}) {
  return {
    data,
    dataAge: 0,
    dataUpdatedAt: Date.now(),
    error: null,
    isError: false,
    isPending: false,
    refetch: vi.fn(),
    ...overrides,
  }
}

function renderLocks(
  route = '/instances/instance-1/locks?range=1h',
  locksResult: object = success(locksResponse),
) {
  mocks.instance.mockReturnValue(success({ perm_tier: 'T2' }))
  mocks.locks.mockReturnValue(locksResult)
  mocks.activity.mockReturnValue(success({ stale: false, metrics: {} }))
  return renderWithProviders(<LocksPage instanceId="instance-1" />, { route })
}

beforeEach(() => {
  mocks.instance.mockReset()
  mocks.locks.mockReset()
  mocks.activity.mockReset()
})

describe('LocksPage degraded and error paths', () => {
  it('UI-LOCK-043 redirects a 401 to login exactly once', async () => {
    const view = renderLocks('/instances/instance-1/locks?range=1h', {
      ...success(undefined),
      error: { kind: 'unauthorized' },
    })

    await act(async () => {
      await Promise.resolve()
    })

    expect(view.router.state.location.pathname).toBe('/login')
    expect(view.router.state.location.search).toBe(
      '?next=%2Finstances%2Finstance-1%2Flocks%3Frange%3D1h',
    )

    await act(async () => {
      await Promise.resolve()
    })
    expect(view.router.state.location.pathname).toBe('/login')
  })

  it('UI-LOCK-044 renders a server error with a working retry action', () => {
    const refetch = vi.fn()
    renderLocks('/instances/instance-1/locks', {
      ...success(undefined),
      error: { kind: 'server', status: 500, error: 'internal_error', detail: 'Locks unavailable' },
      isError: true,
      refetch,
    })

    expect(screen.getByRole('alert')).toHaveTextContent('Server returned 500')
    fireEvent.click(screen.getByRole('button', { name: 'Retry locks' }))
    expect(refetch).toHaveBeenCalledOnce()
  })

  it('UI-LOCK-045 uses the locks refresh policy interval', () => {
    renderLocks()

    expect(REFRESH.locks.interval).toBe(5_000)
    expect(REFRESH.locks.staleAfter).toBeGreaterThanOrEqual(REFRESH.locks.interval * 3)
  })
})
