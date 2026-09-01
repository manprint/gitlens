import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { NOW } from '@/test/time'

import { BlockingTree, type LocksResponseLike } from './BlockingTree'

function response(nodes: readonly unknown[], overrides: Partial<LocksResponseLike> = {}) {
  return {
    sampled_at: NOW.toISOString(),
    stale: false,
    nodes,
    ...overrides,
  } satisfies LocksResponseLike
}

describe('BlockingTree', () => {
  it('UI-LOCK-010 ranks roots by blocked sessions before rendering the forest', () => {
    render(
      <BlockingTree
        response={response([
          { pid: 10, blocked_by: [], wait_duration: 30 },
          { pid: 11, blocked_by: [10], wait_duration: 2 },
          { pid: 20, blocked_by: [], wait_duration: 5 },
          { pid: 21, blocked_by: [20], wait_duration: 2 },
          { pid: 22, blocked_by: [20], wait_duration: 2 },
        ])}
        now={NOW}
      />,
    )

    expect(screen.getAllByRole('treeitem')[0]).toHaveTextContent('PID 20')
    expect(screen.getAllByRole('treeitem')[1]).toHaveTextContent('PID 21')
  })

  it('UI-LOCK-011 distinguishes an instance with no stored sample from no contention', () => {
    render(<BlockingTree response={response([], { sampled_at: null, stale: true })} now={NOW} />)

    expect(
      screen.getByText(/no lock sample has been stored for this instance yet/i),
    ).toBeInTheDocument()
    expect(screen.queryByText(/no lock contention/i)).not.toBeInTheDocument()
  })

  it('UI-LOCK-012 reports age from sampled_at rather than the fetch time', () => {
    const sampledAt = new Date(NOW.getTime() - 17_000).toISOString()
    render(<BlockingTree response={response([], { sampled_at: sampledAt })} now={NOW} />)

    expect(screen.getByRole('region', { name: 'Blocking tree' })).toHaveTextContent(
      /sample age:.*17s/i,
    )
  })

  it('UI-LOCK-013 renders the truncation state at the 2048-byte boundary', () => {
    render(
      <BlockingTree
        response={response([{ pid: 42, blocked_by: [], query: 'x'.repeat(2048) }])}
        now={NOW}
      />,
    )

    expect(screen.getByText(/truncated; budget 2048/i)).toBeInTheDocument()
  })

  it('UI-LOCK-014 exposes a cycle marker without recursing forever', () => {
    render(
      <BlockingTree
        response={response([
          { pid: 1, blocked_by: [2] },
          { pid: 2, blocked_by: [1] },
        ])}
        now={NOW}
      />,
    )

    expect(screen.getByText('Cycle detected')).toBeInTheDocument()
  })

  it('UI-LOCK-015 supports tree levels and arrow-key navigation', () => {
    render(
      <BlockingTree
        response={response([
          { pid: 1, blocked_by: [], state: 'active' },
          { pid: 2, blocked_by: [1] },
        ])}
        now={NOW}
      />,
    )

    const items = screen.getAllByRole('treeitem')
    expect(items[0]).toHaveAttribute('aria-level', '1')
    expect(items[1]).toHaveAttribute('aria-level', '2')

    items[0]?.focus()
    fireEvent.keyDown(items[0]!, { key: 'ArrowLeft' })
    expect(items[0]).toHaveAttribute('aria-expanded', 'false')
    fireEvent.keyDown(items[0]!, { key: 'ArrowRight' })
    expect(items[0]).toHaveAttribute('aria-expanded', 'true')
    const expandedItems = screen.getAllByRole('treeitem')
    fireEvent.keyDown(items[0]!, { key: 'ArrowDown' })
    expect(expandedItems[1]).toHaveFocus()
  })

  it('has no serious or critical accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render(
      <BlockingTree
        response={response([
          { pid: 1, blocked_by: [], state: 'active' },
          { pid: 2, blocked_by: [1] },
        ])}
        now={NOW}
      />,
    )

    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
