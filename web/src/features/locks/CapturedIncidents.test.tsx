import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { deadlockPayload, incidentEvent } from '@/test/incidents'

const mocks = vi.hoisted(() => ({ events: vi.fn() }))

vi.mock('@/api/queries', () => ({ useEvents: mocks.events }))

import { CapturedIncidents } from './CapturedIncidents'

const INSTANCE = '22222222-2222-2222-2222-222222222222'

function result(data: unknown, extra: Record<string, unknown> = {}) {
  return { data, isPending: false, isError: false, error: null, refetch: vi.fn(), ...extra }
}

beforeEach(() => {
  mocks.events.mockReset()
})

describe('CapturedIncidents', () => {
  it('INC-UI-010 asks for this instance and every capture type', () => {
    mocks.events.mockReturnValue(result([]))
    render(<CapturedIncidents instanceId={INSTANCE} />)
    expect(mocks.events).toHaveBeenCalledWith(
      {
        instance_id: INSTANCE,
        type: 'deadlock_captured,lock_wait_captured,idle_in_transaction_captured,long_transaction_captured,long_query_captured',
        limit: 100,
      },
      { durationMs: 7 * 24 * 60 * 60 * 1_000 },
    )
    expect(screen.getByText('No incident captured in the last 7 days')).toBeInTheDocument()
  })

  it('INC-UI-011 shows the newest capture in full and switches on selection', () => {
    mocks.events.mockReturnValue(
      result([
        incidentEvent(
          1,
          {
            type: 'lock_wait',
            captured_at: '2026-11-20T10:00:00Z',
            relation: 'public.items',
            sessions: [],
          },
          'lock_wait_captured',
        ),
        incidentEvent(2, deadlockPayload),
      ]),
    )
    render(<CapturedIncidents instanceId={INSTANCE} />)
    const detail = screen.getByTestId('incident-detail')
    expect(
      within(detail).getByText('Deadlock captured · 2026-11-20 16:02:03 UTC'),
    ).toBeInTheDocument()
    expect(within(detail).getByText('public.orders, row ctid (3,7)')).toBeInTheDocument()
    expect(
      within(detail).getByText(
        'pid 102 waits for ExclusiveLock on public.orders (3,7), blocked by pid 101',
      ),
    ).toBeInTheDocument()
    expect(within(detail).getAllByTestId('incident-session')).toHaveLength(2)
    expect(within(detail).getByText('UPDATE orders SET qty = 1 WHERE id = 7')).toBeInTheDocument()
    expect(
      within(detail).getByText('2 more sessions were left out of the capture.'),
    ).toBeInTheDocument()
    expect(within(detail).getByText(/3 earlier captures were dropped/)).toBeInTheDocument()

    const list = screen.getByRole('list', { name: 'Captured incidents' })
    const buttons = within(list).getAllByRole('button')
    expect(buttons[0]).toHaveAttribute('aria-current', 'true')
    fireEvent.click(buttons[1]!)
    expect(
      within(screen.getByTestId('incident-detail')).getByText(
        'Lock wait captured · 2026-11-20 10:00:00 UTC',
      ),
    ).toBeInTheDocument()
    expect(
      within(screen.getByTestId('incident-detail')).queryByText('Who waited for whom'),
    ).toBeNull()
  })

  it('INC-UI-012 shows loading and error states', () => {
    mocks.events.mockReturnValue(result(undefined, { isPending: true }))
    const { unmount } = render(<CapturedIncidents instanceId={INSTANCE} />)
    expect(screen.getByRole('status')).toHaveTextContent('Loading captured incidents…')
    unmount()

    const refetch = vi.fn()
    mocks.events.mockReturnValue(
      result(undefined, { isError: true, error: { kind: 'network', message: 'down' }, refetch }),
    )
    render(<CapturedIncidents instanceId={INSTANCE} />)
    fireEvent.click(screen.getByRole('button', { name: 'Retry events' }))
    expect(refetch).toHaveBeenCalled()
  })
})
