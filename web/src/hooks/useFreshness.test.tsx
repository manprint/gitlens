import { act, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { REFRESH, type RefreshSurface } from '@/api/policy'
import { NOW } from '@/test/time'

import { useFreshness } from './useFreshness'

interface FreshnessProbeProps {
  dataUpdatedAt: number
  policy: RefreshSurface
  errored?: boolean
}

function FreshnessProbe({ dataUpdatedAt, policy, errored = false }: FreshnessProbeProps) {
  const { age, isStale, label } = useFreshness(dataUpdatedAt, policy)

  return (
    <>
      <output data-testid="freshness-age">{age}</output>
      <output data-testid="freshness-stale">{String(isStale)}</output>
      <output data-testid="freshness-label">{label}</output>
      <output data-testid="freshness-error">{errored ? 'error' : 'ok'}</output>
    </>
  )
}

describe('useFreshness', () => {
  it('UI-FRESH-001 reports the age from the frozen clock', () => {
    render(<FreshnessProbe dataUpdatedAt={NOW.getTime() - 12_000} policy="fleet" />)

    expect(screen.getByTestId('freshness-age')).toHaveTextContent('12000')
    expect(screen.getByTestId('freshness-label')).toHaveTextContent('Updated 12s ago')
    expect(screen.getByTestId('freshness-stale')).toHaveTextContent('false')
  })

  it('UI-FRESH-002 flips to stale exactly at the threshold, not before', async () => {
    const view = render(<FreshnessProbe dataUpdatedAt={NOW.getTime()} policy="fleet" />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.fleet.staleAfter - 1_000)
    })
    expect(screen.getByTestId('freshness-stale')).toHaveTextContent('false')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000)
    })
    expect(screen.getByTestId('freshness-age')).toHaveTextContent(String(REFRESH.fleet.staleAfter))
    expect(screen.getByTestId('freshness-stale')).toHaveTextContent('true')
    view.unmount()
  })

  it('UI-FRESH-003 keeps the last good age when a refetch errors', async () => {
    const dataUpdatedAt = NOW.getTime()
    const view = render(<FreshnessProbe dataUpdatedAt={dataUpdatedAt} policy="fleet" />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.fleet.staleAfter)
    })
    view.rerender(<FreshnessProbe dataUpdatedAt={dataUpdatedAt} policy="fleet" errored />)

    expect(screen.getByTestId('freshness-error')).toHaveTextContent('error')
    expect(screen.getByTestId('freshness-age')).toHaveTextContent(String(REFRESH.fleet.staleAfter))
    expect(screen.getByTestId('freshness-stale')).toHaveTextContent('true')
  })

  it('does not call an unavailable timestamp fresh or stale', () => {
    render(<FreshnessProbe dataUpdatedAt={0} policy="fleet" />)

    expect(screen.getByTestId('freshness-age')).toHaveTextContent('0')
    expect(screen.getByTestId('freshness-label')).toHaveTextContent('No data yet')
    expect(screen.getByTestId('freshness-stale')).toHaveTextContent('false')
  })
})
