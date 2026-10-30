import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { CAPTIONS } from '@/lib/captions'
import { glossary } from '@/lib/glossary'
import { expectNoA11yViolations } from '@/test/a11y'

import { Caption } from './Caption'
import { ColumnHint, HINT_CLOSE_DELAY_MS, HINT_OPEN_DELAY_MS, InfoHint } from './InfoHint'
import { Term } from './Term'
import { placeTooltip } from './tooltipPlacement'

const hintButton = () => screen.getByRole('button', { name: 'About Transactions' })

describe('Caption, InfoHint and Term (D30)', () => {
  it('CAPCMP-001 opens on hover after the delay and closes on leave', () => {
    render(<InfoHint id="overview.tps" label="Transactions" />)
    fireEvent.mouseEnter(hintButton())
    act(() => {
      vi.advanceTimersByTime(HINT_OPEN_DELAY_MS - 1)
    })
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
    act(() => {
      vi.advanceTimersByTime(1)
    })
    const tooltip = screen.getByRole('tooltip')
    expect(hintButton()).toHaveAttribute('aria-describedby', tooltip.id)
    expect(tooltip).toHaveTextContent(CAPTIONS['overview.tps'].what)
    fireEvent.mouseLeave(hintButton())
    act(() => {
      vi.advanceTimersByTime(HINT_CLOSE_DELAY_MS)
    })
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
    // The description target stays, hidden, so the button keeps its description.
    expect(hintButton()).toHaveAttribute('aria-describedby', tooltip.id)
    expect(hintButton()).toHaveAttribute('aria-controls', tooltip.id)
  })

  it('CAPCMP-001 leaving before the delay never opens it', () => {
    render(<InfoHint id="overview.tps" label="Transactions" />)
    fireEvent.mouseEnter(hintButton())
    act(() => {
      vi.advanceTimersByTime(HINT_OPEN_DELAY_MS - 50)
    })
    fireEvent.mouseLeave(hintButton())
    act(() => {
      vi.advanceTimersByTime(HINT_OPEN_DELAY_MS)
    })
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })

  it('CAPCMP-001 the pointer can move onto the tooltip, which then stays open (WCAG 1.4.13)', () => {
    render(<InfoHint id="overview.tps" label="Transactions" />)
    fireEvent.mouseEnter(hintButton())
    act(() => {
      vi.advanceTimersByTime(HINT_OPEN_DELAY_MS)
    })
    const tooltip = screen.getByRole('tooltip')
    fireEvent.mouseLeave(hintButton())
    fireEvent.mouseEnter(tooltip)
    act(() => {
      vi.advanceTimersByTime(HINT_CLOSE_DELAY_MS * 2)
    })
    expect(screen.getByRole('tooltip')).toBeInTheDocument()
    // Escape anywhere on the page dismisses a hover-opened tooltip.
    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })

  it('CAPCMP-001 opens on focus and closes on blur and on Escape', () => {
    render(<InfoHint id="overview.tps" label="Transactions" />)
    fireEvent.focus(hintButton())
    expect(screen.getByRole('tooltip')).toBeInTheDocument()
    fireEvent.blur(hintButton())
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
    fireEvent.focus(hintButton())
    fireEvent.keyDown(hintButton(), { key: 'Escape' })
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })

  it('CAPCMP-001 a click pins the tooltip open until the next click', () => {
    render(<InfoHint id="overview.tps" label="Transactions" />)
    expect(hintButton()).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(hintButton())
    expect(hintButton()).toHaveAttribute('aria-expanded', 'true')
    fireEvent.mouseLeave(hintButton())
    expect(screen.getByRole('tooltip')).toBeInTheDocument()
    fireEvent.click(hintButton())
    expect(hintButton()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })

  it('CAPCMP-001 a pinned tooltip closes on Escape and on a tap outside', () => {
    render(
      <>
        <InfoHint id="overview.tps" label="Transactions" />
        <p>Elsewhere</p>
      </>,
    )
    fireEvent.click(hintButton())
    fireEvent.keyDown(document.body, { key: 'Escape' })
    expect(hintButton()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
    fireEvent.click(hintButton())
    fireEvent.pointerDown(screen.getByRole('tooltip'))
    expect(screen.getByRole('tooltip')).toBeInTheDocument()
    fireEvent.pointerDown(screen.getByText('Elsewhere'))
    expect(hintButton()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
  })

  it('CAPCMP-001 lists only the fields the entry has', () => {
    render(<InfoHint id="index.duplicate" label="Duplicate indexes" />)
    fireEvent.focus(screen.getByRole('button', { name: 'About Duplicate indexes' }))
    const terms = within(screen.getByRole('tooltip'))
      .getAllByRole('term')
      .map((term) => term.textContent)
    expect(terms).toEqual(['Source', 'Unit', 'Window'])
  })

  it('CAPCMP-001 a column hint shows its sentence', () => {
    render(<ColumnHint id="statements.mean-time" label="Mean time" />)
    fireEvent.focus(screen.getByRole('button', { name: 'About Mean time' }))
    expect(screen.getByRole('tooltip')).toHaveTextContent(/Average execution time of one call/)
  })

  it('CAPCMP-004 the tooltip stays inside the viewport', () => {
    const viewport = { width: 1000, height: 800 }
    const tip = { width: 288, height: 120 }
    const cases = [
      // anchor box, expected side
      [{ top: 400, left: 500, width: 20, height: 20 }, 'top'],
      [{ top: 20, left: 500, width: 20, height: 20 }, 'bottom'],
      [{ top: 20, left: 20, width: 20, height: 760 }, 'right'],
      [{ top: 20, left: 900, width: 90, height: 760 }, 'left'],
    ] as const
    for (const [anchor, side] of cases) {
      const placement = placeTooltip(anchor, tip, viewport)
      expect(placement.side, JSON.stringify(anchor)).toBe(side)
      expect(placement.left).toBeGreaterThanOrEqual(8)
      expect(placement.left + tip.width).toBeLessThanOrEqual(viewport.width - 8)
      expect(placement.top).toBeGreaterThanOrEqual(8)
      expect(placement.top + tip.height).toBeLessThanOrEqual(viewport.height - 8)
    }
    // No side has room: below, kept inside the viewport.
    const cramped = placeTooltip({ top: 50, left: 50, width: 900, height: 700 }, tip, viewport)
    expect(cramped).toEqual({ side: 'bottom', top: viewport.height - tip.height - 8, left: 356 })
    // At the right edge the tooltip above is pulled back into the viewport.
    const edge = placeTooltip({ top: 400, left: 990, width: 10, height: 10 }, tip, viewport)
    expect(edge).toEqual({ side: 'top', top: 272, left: 1000 - 288 - 8 })
  })

  it('CAPCMP-002 Term exposes its definition as the description', () => {
    render(<Term term="WAL" />)
    expect(screen.getByText('WAL')).toHaveAccessibleDescription(glossary.WAL.short)
    expect(screen.getByText('WAL')).toHaveAttribute('title', glossary.WAL.short)
  })

  it('CAPCMP-005 Caption renders the what sentence with its id', () => {
    render(<Caption id="overview.tps" />)
    expect(screen.getByText(CAPTIONS['overview.tps'].what)).toHaveAttribute(
      'data-caption-id',
      'overview.tps',
    )
  })

  it('CAPCMP-001 has no serious accessibility violations, open or closed', async () => {
    vi.useRealTimers()
    const { container } = render(
      <p>
        <Term term="LSN" /> <InfoHint id="overview.tps" label="Transactions" />
      </p>,
    )
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
    fireEvent.focus(hintButton())
    await expect(expectNoA11yViolations(container)).resolves.toBeUndefined()
  })
})
