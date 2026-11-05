import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { CAPTIONS } from '@/lib/captions'

import { InstancePanel } from './InstancePanel'

describe('InstancePanel caption (D30)', () => {
  it('PANEL-CAP-001 a captioned panel is a card with its caption line and its ⓘ', () => {
    render(
      <InstancePanel captionId="overview.tps" title="Throughput">
        <p>Body</p>
      </InstancePanel>,
    )
    const panel = screen.getByRole('region', { name: 'Throughput' })
    expect(panel).toHaveAttribute('data-card', '')
    expect(panel).toHaveAttribute('data-caption-id', 'overview.tps')
    const line = within(panel).getByTitle(CAPTIONS['overview.tps'].what)
    expect(line).toHaveTextContent(CAPTIONS['overview.tps'].what)
    expect(line).toHaveClass('min-[1600px]:line-clamp-2')
    fireEvent.focus(within(panel).getByRole('button', { name: 'About Throughput' }))
    expect(screen.getByRole('tooltip')).toHaveTextContent(CAPTIONS['overview.tps'].what)
  })

  it('PANEL-CAP-001 a description given too wraps below the caption', () => {
    render(
      <InstancePanel captionId="overview.tps" description="Extra note" title="Throughput">
        <p>Body</p>
      </InstancePanel>,
    )
    const note = screen.getByText('Extra note')
    expect(note).not.toHaveClass('min-[1600px]:truncate', 'min-[1600px]:line-clamp-2')
    expect(note).not.toHaveAttribute('data-caption-id')
  })

  it('PANEL-CAP-002 without a caption id the description clamps to two lines with its title', () => {
    render(
      <InstancePanel description="Long hint" title="T">
        <p>Body</p>
      </InstancePanel>,
    )
    const panel = screen.getByRole('region', { name: 'T' })
    expect(panel).not.toHaveAttribute('data-card')
    expect(panel).not.toHaveAttribute('data-caption-id')
    const hint = screen.getByText('Long hint')
    expect(hint).toHaveAttribute('title', 'Long hint')
    expect(hint).toHaveClass('min-[1600px]:line-clamp-2')
    expect(hint).not.toHaveClass('min-[1600px]:truncate')
    expect(screen.queryByRole('button', { name: /About/ })).toBeNull()
  })
})
