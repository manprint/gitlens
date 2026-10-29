import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'

import { ChartCard } from './ChartCard'

const range = {
  from: new Date(Date.UTC(2026, 9, 29, 9, 41)),
  to: new Date(Date.UTC(2026, 9, 29, 10, 41)),
}

describe('ChartCard', () => {
  it('CCARD-001 shows the title and the range with its step', () => {
    render(
      <ChartCard range={range} state="ready" stepSeconds={60} title="Transactions per second">
        <div>chart</div>
      </ChartCard>,
    )
    expect(screen.getByRole('region', { name: 'Transactions per second' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 3, name: 'Transactions per second' })).toBeVisible()
    expect(screen.getByText('Oct 29 09:41 → 10:41 UTC · step 1m')).toBeVisible()
    expect(screen.getByText('chart')).toBeVisible()
  })

  it('CCARD-002 states that the range has no data', () => {
    render(<ChartCard range={range} state="empty" stepSeconds={60} title="TPS" />)
    expect(screen.getByText('No data in this range')).toBeVisible()
  })

  it('CCARD-003 shows a loading status', () => {
    render(<ChartCard range={range} state="loading" stepSeconds={60} title="TPS" />)
    const status = screen.getByRole('status', { name: 'Loading TPS' })
    expect(status).toHaveAttribute('aria-busy', 'true')
    expect(status).toHaveStyle({ height: '300px' })
  })

  it('CCARD-004 shows the error with a retry that calls onRetry', () => {
    const onRetry = vi.fn()
    render(
      <ChartCard
        error={{ kind: 'network', message: 'offline' }}
        onRetry={onRetry}
        range={range}
        state="error"
        stepSeconds={60}
        title="TPS"
      />,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load TPS.')
    fireEvent.click(screen.getByRole('button', { name: 'Retry TPS' }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('CCARD-005 appends the subtitle to the range line', () => {
    render(
      <ChartCard
        range={range}
        state="ready"
        stepSeconds={300}
        subtitle="3 counter resets"
        title="TPS"
        unitHint="per second"
      />,
    )
    expect(screen.getByText('Oct 29 09:41 → 10:41 UTC · step 5m · 3 counter resets')).toBeVisible()
    expect(screen.getByRole('heading', { level: 3 })).toHaveTextContent('TPS (per second)')
    expect(screen.getByRole('region', { name: /^TPS/ })).not.toHaveClass('lg:col-span-2')
  })

  it('CCARD-006 has no accessibility violations in any state', async () => {
    // axe schedules its own timers.
    vi.useRealTimers()
    for (const state of ['ready', 'loading', 'empty', 'error'] as const) {
      const { container, unmount } = render(
        <ChartCard
          onRetry={() => undefined}
          range={range}
          state={state}
          stepSeconds={60}
          title="TPS"
        >
          <div>chart</div>
        </ChartCard>,
      )
      await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
      unmount()
    }
  })
})
