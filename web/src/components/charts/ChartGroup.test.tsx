import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ChartCard } from './ChartCard'
import { ChartGroup } from './ChartGroup'

describe('ChartGroup', () => {
  it('CGRP-001 renders its heading, description and id', () => {
    render(
      <ChartGroup description="Commits and rollbacks" id="throughput" title="Throughput">
        <div>card</div>
      </ChartGroup>,
    )
    const group = screen.getByRole('region', { name: 'Throughput' })
    expect(group).toHaveAttribute('id', 'throughput')
    expect(within(group).getByRole('heading', { level: 2, name: 'Throughput' })).toBeVisible()
    expect(within(group).getByText('Commits and rollbacks')).toBeVisible()
  })

  it('CGRP-002 places the children in a two-column grid', () => {
    render(
      <ChartGroup description="d" id="g" title="Group">
        <div>first card</div>
        <div>second card</div>
        <ChartCard
          height={180}
          range={{ from: new Date(0), to: new Date(3_600_000) }}
          state="loading"
          stepSeconds={60}
          title="Wide"
          wide
        />
      </ChartGroup>,
    )
    const grid = screen.getByTestId('g-grid')
    expect(grid).toHaveClass('grid', 'gap-6', 'lg:grid-cols-2')
    expect(grid).toContainElement(screen.getByText('first card'))
    expect(grid).toContainElement(screen.getByText('second card'))
    const wide = screen.getByRole('region', { name: 'Wide' })
    expect(grid).toContainElement(wide)
    expect(wide).toHaveClass('lg:col-span-2')
    expect(screen.getByRole('status', { name: 'Loading Wide' })).toHaveStyle({ height: '180px' })
  })
})
