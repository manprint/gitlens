import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { CAPTIONS } from '@/lib/captions'
import { expectNoA11yViolations } from '@/test/a11y'

import { MetricTile } from './MetricTile'

describe('MetricTile card contract (D31)', () => {
  it('CARD-001 renders the detail sentence on a line of its own', () => {
    render(
      <MetricTile
        detail="3 of them are unique indexes."
        label="Exact duplicate indexes"
        value="5"
      />,
    )
    const tile = screen.getByRole('article', { name: 'Exact duplicate indexes' })
    const detail = within(tile).getByText('3 of them are unique indexes.')
    expect(detail.tagName).toBe('P')
    expect(detail).not.toHaveTextContent('5')
    expect(detail).toHaveClass('min-[1600px]:w-full')
  })

  it('CARD-002 renders the status as text with its tone', () => {
    render(
      <MetricTile label="Replication" status={{ tone: 'warn', text: 'Lagging' }} value="12 s" />,
    )
    const status = screen.getByText('Lagging')
    expect(status).toHaveAttribute('data-tone', 'warn')
  })

  it('CARD-003 a caption id marks the tile and puts the explanation behind its ⓘ', () => {
    render(<MetricTile captionId="overview.tps" label="Transactions" value="42" unit="/s" />)
    const tile = screen.getByRole('article', { name: 'Transactions' })
    expect(tile).toHaveAttribute('data-caption-id', 'overview.tps')
    // CAPCMP-005: no visible caption line on a tile, the ⓘ carries it.
    expect(screen.getByText(CAPTIONS['overview.tps'].what)).not.toBeVisible()
    fireEvent.focus(within(tile).getByRole('button', { name: 'About Transactions' }))
    expect(screen.getByRole('tooltip')).toHaveTextContent(CAPTIONS['overview.tps'].what)
  })

  it('CARD-003 a tile without caption, detail or status renders as before', () => {
    render(<MetricTile label="Latency" value={42} unit="ms" />)
    const tile = screen.getByRole('article', { name: 'Latency' })
    expect(tile).not.toHaveAttribute('data-caption-id')
    expect(within(tile).queryByRole('button')).not.toBeInTheDocument()
    expect(tile).toHaveTextContent('Latency42ms')
  })

  it('CARD-004 has no serious accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render(
      <MetricTile
        captionId="activity.connections-used"
        detail="12 of them are idle."
        label="Connections in use"
        status={{ tone: 'ok', text: 'Below the warning level' }}
        value="40"
        unit="connections"
      />,
    )
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
