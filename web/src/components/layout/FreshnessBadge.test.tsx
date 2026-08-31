import { act, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { REFRESH } from '@/api/policy'
import { NOW } from '@/test/time'

import { FreshnessBadge } from './FreshnessBadge'

describe('FreshnessBadge', () => {
  it('renders a status badge while data is fresh', () => {
    const { container } = render(<FreshnessBadge dataUpdatedAt={NOW.getTime()} policy="fleet" />)

    expect(screen.getByRole('status')).toHaveAccessibleName('Data freshness: Updated 0s ago')
    expect(container).toHaveTextContent('Updated 0s ago')
  })

  it('switches to the Stale primitive after the policy threshold', async () => {
    render(<FreshnessBadge dataUpdatedAt={NOW.getTime()} policy="fleet" />)

    await act(async () => {
      await vi.advanceTimersByTimeAsync(REFRESH.fleet.staleAfter)
    })

    expect(screen.getByRole('status')).toHaveAccessibleName('Stale data: 45s old')
    expect(screen.getByRole('status')).toHaveTextContent('Updated 45s ago')
    expect(screen.getByRole('status')).toHaveTextContent('threshold 45s')
  })
})
