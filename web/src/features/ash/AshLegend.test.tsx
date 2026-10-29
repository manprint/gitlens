import { fireEvent, render, screen, within } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'

import { formatAverage, swatchLetters } from '@/lib/ashGroups'

import { AshLegend, type AshLegendEntry } from './AshLegend'

const entries: AshLegendEntry[] = [
  {
    name: 'Lock',
    color: 'var(--wait-lock)',
    description: 'Waiting for a heavyweight lock held by another transaction',
    average: 0.25,
  },
  {
    name: 'LWLock',
    color: 'var(--wait-lwlock)',
    description: 'Waiting for an internal lightweight lock (buffer, WAL, catalog contention)',
    average: 1.5,
  },
]

function Stateful() {
  const [isolated, setIsolated] = useState<string | null>(null)
  return (
    <AshLegend
      entries={entries}
      isolated={isolated}
      label="Locks and contention legend"
      onToggle={(name) => setIsolated((current) => (current === name ? null : name))}
    />
  )
}

const row = (name: string) => screen.getByRole('button', { name: new RegExp(`^.?${name} —`, 'u') })

describe('AshLegend', () => {
  it('ASHLEG-001 lists every series with swatch, name, description and average', () => {
    render(<AshLegend entries={entries} isolated={null} label="Legend" onToggle={vi.fn()} />)

    const list = screen.getByRole('list', { name: 'Legend' })
    expect(within(list).getAllByRole('listitem')).toHaveLength(2)
    expect(row('Lock')).toHaveTextContent(
      'Lock — Waiting for a heavyweight lock held by another transaction',
    )
    expect(row('Lock')).toHaveTextContent('avg 0.25 active backends')
    expect(row('LWLock')).toHaveTextContent('avg 1.50 active backends')
    const swatches = screen.getAllByTestId('ash-legend-swatch')
    expect(swatches.map((swatch) => swatch.style.backgroundColor)).toEqual([
      'var(--wait-lock)',
      'var(--wait-lwlock)',
    ])
  })

  it('ASHLEG-002 a click isolates the series and a second click restores all', () => {
    render(<Stateful />)

    expect(row('Lock')).toHaveAttribute('aria-pressed', 'false')
    fireEvent.click(row('Lock'))
    expect(row('Lock')).toHaveAttribute('aria-pressed', 'true')
    expect(row('LWLock')).toHaveAttribute('aria-pressed', 'false')
    expect(row('LWLock').className).toContain('opacity-50')
    fireEvent.click(row('Lock'))
    expect(row('Lock')).toHaveAttribute('aria-pressed', 'false')
    expect(row('LWLock').className).not.toContain('opacity-50')
  })

  it('ASHLEG-003 rows are native buttons reachable and pressed by keyboard', () => {
    const onToggle = vi.fn()
    render(<AshLegend entries={entries} isolated={null} label="Legend" onToggle={onToggle} />)

    const lock = row('Lock')
    lock.focus()
    expect(lock).toHaveFocus()
    expect(lock.tagName).toBe('BUTTON')
    expect(lock).toHaveAttribute('type', 'button')
    // A native button turns Enter and Space into a click.
    fireEvent.keyDown(lock, { key: 'Enter' })
    fireEvent.click(lock)
    expect(onToggle).toHaveBeenCalledWith('Lock')
  })

  it('ASHLEG-004 swatches carry a letter, not colour alone', () => {
    render(<AshLegend entries={entries} isolated={null} label="Legend" onToggle={vi.fn()} />)

    const swatches = screen.getAllByTestId('ash-legend-swatch')
    expect(swatches.map((swatch) => swatch.getAttribute('data-swatch-letter'))).toEqual(['L', 'W'])
    expect(swatches.map((swatch) => swatch.textContent)).toEqual(['L', 'W'])
    expect(swatchLetters(['Lock', 'LWLock', 'BufferPin', 'Buffer'])).toEqual(['L', 'W', 'B', 'U'])
    expect(swatchLetters(['WALWrite', 'WALSync', 'WALInsert', 'other (folded)'])).toEqual([
      'W',
      'A',
      'L',
      'O',
    ])
    const many = swatchLetters(['aa', 'a', 'A'])
    expect(new Set(many).size).toBe(3)
  })

  it('ASHLEG-006 shows a tiny average as <0.01, not 0.00', () => {
    expect(formatAverage(0.004)).toBe('<0.01')
    expect(formatAverage(0)).toBe('0.00')
    expect(formatAverage(0.05)).toBe('0.05')
  })

  it('ASHLEG-005 has no accessibility violations', async () => {
    vi.useRealTimers()
    const { container } = render(<Stateful />)
    fireEvent.click(row('LWLock'))
    expect(row('LWLock')).toHaveAttribute('aria-pressed', 'true')

    await expectNoA11yViolations(container)
  })
})
