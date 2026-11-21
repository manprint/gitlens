import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { renderWithProviders } from '@/test/render'

const mocks = vi.hoisted(() => ({
  statements: vi.fn(),
  compare: vi.fn(),
}))

vi.mock('@/api/queries', () => ({
  useStatements: mocks.statements,
  useStatementsCompare: mocks.compare,
}))

import { QueryListPage } from './QueryListPage'

const metrics = (calls: number) => ({
  calls,
  total_exec_time_ms: calls * 2,
  rows: null,
  mean_exec_time: 2,
  shared_blks_hit: null,
  shared_blks_read: null,
  wal_bytes: null,
  temp_blks_written: null,
})

const rows = [
  {
    datname: 'app',
    queryid: '11',
    query_text: 'SELECT * FROM orders',
    status: 'both',
    a: metrics(1),
    b: metrics(3),
    delta: metrics(2),
    ratio: metrics(3),
  },
  {
    datname: 'app',
    queryid: '12',
    query_text: 'SELECT 2',
    status: 'new',
    a: null,
    b: metrics(1),
    delta: metrics(1),
    ratio: { ...metrics(0), calls: null },
  },
]

function success<T>(data: T) {
  return { data, error: null, isPending: false, refetch: vi.fn(), dataUpdatedAt: Date.now() }
}

const ROUTE =
  '/instances/instance-1/queries?mode=compare&compare=week&sort=calls&from=2026-11-20T10:00:00Z&to=2026-11-20T12:00:00Z'

describe('QueryCompare', () => {
  beforeEach(() => {
    mocks.statements.mockReturnValue(success({ statements: [] }))
    mocks.compare.mockReturnValue(
      success({ a: {}, b: {}, order_by: 'calls', normalized: 'per_second', rows }),
    )
  })

  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('UI-CMP-003 Queries compare requests /statements/compare with A and B of the chosen preset', () => {
    renderWithProviders(<QueryListPage instanceId="instance-1" />, { route: ROUTE })

    expect(mocks.compare).toHaveBeenLastCalledWith({
      instance_id: 'instance-1',
      order_by: 'calls',
      limit: 50,
      a_from: '2026-11-13T10:00:00.000Z',
      a_to: '2026-11-13T12:00:00.000Z',
      b_from: '2026-11-20T10:00:00.000Z',
      b_to: '2026-11-20T12:00:00.000Z',
    })
    expect(mocks.statements).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Period A'), { target: { value: 'previous' } })
    expect(mocks.compare).toHaveBeenLastCalledWith(
      expect.objectContaining({
        a_from: '2026-11-20T08:00:00.000Z',
        a_to: '2026-11-20T10:00:00.000Z',
      }),
    )

    const table = screen.getByRole('table', { name: 'Statements compared between two periods' })
    const worse = within(table).getByText('×3.0')
    expect(worse).toHaveAttribute('data-variant', 'warning')
    expect(within(table).getByText('new')).toBeInTheDocument()
    expect(within(table).getByRole('link', { name: /11/ })).toHaveAttribute(
      'href',
      '/instances/instance-1/queries/11?db=app',
    )
    expect(within(table).getByText('+2/s')).toBeInTheDocument()
  })

  it('waits for both ends of a custom period A before requesting', () => {
    renderWithProviders(<QueryListPage instanceId="instance-1" />, {
      route:
        '/instances/instance-1/queries?mode=compare&compare=custom&a_from=2026-11-19T10:00:00Z',
    })
    expect(mocks.compare).toHaveBeenLastCalledWith(null)
    expect(screen.getByRole('status')).toHaveTextContent('Choose both ends of period A')
  })

  it('the mode switch returns to the statement list', () => {
    renderWithProviders(<QueryListPage instanceId="instance-1" />, { route: ROUTE })
    fireEvent.click(screen.getByLabelText('Statement list'))
    expect(mocks.statements).toHaveBeenCalled()
  })
})
