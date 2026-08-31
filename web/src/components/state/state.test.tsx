import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { ApiFailure } from '@/api/client'
import { expectNoA11yViolations } from '@/test/a11y'

import {
  Degraded,
  Disabled,
  EmptyState,
  ErrorState,
  NotPermitted,
  Stale,
  Truncated,
  Unknown,
} from './index'

afterEach(() => {
  vi.restoreAllMocks()
})

beforeEach(() => {
  vi.useRealTimers()
})

describe('state primitives', () => {
  it('UI-STATE-001 renders an em dash for an unknown value', async () => {
    const { container } = render(<Unknown reason="No sample was collected." />)

    expect(screen.getByLabelText('not measured')).toHaveTextContent('—')
    expect(screen.getByLabelText('not measured')).not.toHaveTextContent('0')
    await expectNoA11yViolations(container)
  })

  it('UI-STATE-002 announces stale age and threshold', async () => {
    const { container } = render(
      <Stale age={125} threshold={60}>
        42 requests
      </Stale>,
    )

    expect(screen.getByRole('status')).toHaveAccessibleName('Stale data: 2m 5s old')
    expect(screen.getByRole('status')).toHaveTextContent('threshold 1m')
    await expectNoA11yViolations(container)
  })

  it('UI-STATE-003 states the shown count, budget and scope', async () => {
    const { container } = render(<Truncated shown={12} budget={100} scope="relations" />)

    expect(screen.getByRole('status')).toHaveTextContent('Showing 12 of 100 relations')
    expect(screen.getByRole('status')).toHaveTextContent('budget 100')
    await expectNoA11yViolations(container)
  })

  it('UI-STATE-004 names the required and current permission tiers', async () => {
    const { container } = render(<NotPermitted required="T2" current="T0" />)

    expect(screen.getByRole('status')).toHaveTextContent('Requires T2 permission')
    expect(screen.getByRole('status')).toHaveTextContent('current tier is T0')
    await expectNoA11yViolations(container)
  })

  it('UI-STATE-005 disables rather than hides an associated control', async () => {
    const { container } = render(
      <NotPermitted required="T1" current="T0">
        <button type="button" aria-label="Run explain">
          Run explain
        </button>
      </NotPermitted>,
    )
    const control = screen.getByRole('button', {
      name: 'Run explain — Requires T1 permission; current tier is T0.',
    })

    expect(control).toBeDisabled()
    expect(control).toHaveAttribute('aria-disabled', 'true')
    await expectNoA11yViolations(container)
  })

  it('UI-STATE-006 names the disabled configuration key', async () => {
    const { container } = render(<Disabled feature="ASH sampling" configKey="PGLENS_ASH_ENABLED" />)

    expect(screen.getByRole('status')).toHaveTextContent('PGLENS_ASH_ENABLED')
    await expectNoA11yViolations(container)
  })

  it('UI-STATE-007 distinguishes disabled and empty results', async () => {
    const { container, rerender } = render(
      <Disabled feature="Bloat check" configKey="PGLENS_BLOAT_ENABLED" />,
    )
    expect(screen.getByRole('status')).toHaveTextContent('disabled')
    await expectNoA11yViolations(container)

    rerender(
      <EmptyState
        title="No bloat findings"
        description="The enabled check found no relations requiring attention."
      />,
    )
    expect(screen.getByRole('heading', { name: 'No bloat findings' })).toBeInTheDocument()
    expect(screen.getByText(/enabled check found no relations/)).toBeInTheDocument()
    await expectNoA11yViolations(container)
  })

  it('covers an optional EmptyState action', async () => {
    const { container } = render(
      <EmptyState
        title="No agents"
        description="Register an agent to start collecting data."
        action={<button type="button">Register agent</button>}
      />,
    )

    expect(screen.getByRole('button', { name: 'Register agent' })).toBeInTheDocument()
    await expectNoA11yViolations(container)
  })

  it('UI-STATE-008 names the endpoint, describes every failure and retries', async () => {
    const failures: ApiFailure[] = [
      { kind: 'unauthorized' },
      { kind: 'forbidden' },
      { kind: 'not_found' },
      { kind: 'unprocessable', error: 'invalid', detail: 'bad filter' },
      { kind: 'server', status: 500, error: 'internal', detail: 'try later' },
      { kind: 'network', message: 'offline' },
      { kind: 'malformed', message: 'bad JSON' },
    ]
    const onRetry = vi.fn()

    for (const failure of failures) {
      const { container, unmount } = render(
        <ErrorState failure={failure} endpoint="/api/v1/clusters" onRetry={onRetry} />,
      )
      expect(screen.getByRole('alert')).toHaveTextContent('Could not load /api/v1/clusters.')
      fireEvent.click(screen.getByRole('button', { name: 'Retry /api/v1/clusters' }))
      await expectNoA11yViolations(container)
      unmount()
    }
    expect(onRetry).toHaveBeenCalledTimes(failures.length)
  })

  it('uses the default endpoint label for ErrorState', async () => {
    const { container } = render(
      <ErrorState failure={{ kind: 'not_found' }} onRetry={() => undefined} />,
    )

    expect(screen.getByRole('button', { name: 'Retry API' })).toBeInTheDocument()
    await expectNoA11yViolations(container)
  })

  it('keeps the ErrorState failure mapping exhaustive at runtime', () => {
    const unknownFailure = { kind: 'unexpected' } as unknown as ApiFailure

    expect(() =>
      render(<ErrorState failure={unknownFailure} onRetry={() => undefined} />),
    ).toThrow()
  })

  it('UI-STATE-009 states why a view is degraded, including an optional requirement', async () => {
    const { container, rerender } = render(
      <Degraded reason="replication data is unavailable" requires="T1 monitoring permission" />,
    )
    expect(screen.getByRole('status')).toHaveTextContent('T1 monitoring permission')
    await expectNoA11yViolations(container)

    rerender(<Degraded reason="the check is still warming up" />)
    expect(screen.getByRole('status')).toHaveTextContent('still warming up')
    await expectNoA11yViolations(container)
  })

  it('uses the default Unknown explanation and remains accessible', async () => {
    const { container } = render(<Unknown />)

    expect(screen.getByLabelText('not measured')).toHaveAttribute(
      'title',
      'Value was not measured.',
    )
    await expectNoA11yViolations(container)
  })
})
