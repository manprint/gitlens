import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { DeliverySection } from './DeliverySection'

describe('DeliverySection', () => {
  it('DELIVERY-UI-001 lists Google Chat with both server variables', () => {
    // The section links to Settings, Notifications, so it renders inside a router.
    render(
      <MemoryRouter>
        <DeliverySection />
      </MemoryRouter>,
    )
    const channels = screen.getByRole('list', { name: 'Supported notification channels' })
    const cards = within(channels)
      .getAllByRole('listitem')
      .filter((item) => within(item).queryByRole('heading', { name: 'Google Chat' }))
    const [card] = cards
    expect(cards).toHaveLength(1)
    if (!card) throw new Error('Google Chat card missing')
    const variables = within(card)
      .getAllByRole('code')
      .map((code) => code.textContent)
    expect(variables).toEqual([
      'PGLENS_GOOGLE_CHAT_WEBHOOK_URL',
      'PGLENS_GOOGLE_CHAT_WEBHOOK_URL_FILE',
    ])
    expect(
      within(channels)
        .getAllByRole('heading', { level: 3 })
        .map((heading) => heading.textContent),
    ).toEqual(['Slack', 'Generic webhook', 'Google Chat'])
  })
})
