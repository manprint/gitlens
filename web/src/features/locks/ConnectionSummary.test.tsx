import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { withOrigins, withTruncatedConnections } from '@/test/fixtures/getLocks'

import { ConnectionSummary } from './ConnectionSummary'
import { OriginBadge } from './OriginBadge'

describe('ConnectionSummary', () => {
  it('SORIGIN-UI-009 shows one tab per group with counts and shares', () => {
    render(<ConnectionSummary connections={withOrigins.connections} />)

    const tabs = within(screen.getByRole('tablist', { name: 'Connection groups' }))
    expect(tabs.getAllByRole('tab').map((tab) => tab.textContent)).toEqual([
      'By origin',
      'By application',
      'By user',
      'By client',
      'By database',
      'By state',
    ])

    const origin = within(screen.getByRole('list', { name: 'Connections by origin' }))
    expect(origin.getAllByRole('listitem').map((item) => item.textContent)).toEqual([
      'application266.7%',
      'pglens133.3%',
    ])

    fireEvent.mouseDown(tabs.getByRole('tab', { name: 'By application' }), { button: 0 })
    fireEvent.click(tabs.getByRole('tab', { name: 'By application' }))
    expect(screen.getByRole('list', { name: 'Connections by application' })).toHaveTextContent(
      'billing',
    )
  })

  it('SORIGIN-UI-009 counts a cut sample from the activity totals and says so', () => {
    render(<ConnectionSummary connections={withTruncatedConnections.connections} />)

    expect(screen.getByText(/every client connection of the instance/)).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Connections by origin' })).toHaveTextContent(
      'application240',
    )
  })

  it('SORIGIN-UI-009 states an empty name and an empty group in words', () => {
    render(
      <ConnectionSummary
        connections={{
          ...withOrigins.connections,
          by_application_enabled: false,
          by_application: [],
          by_user: [{ key: '', count: 1 }],
        }}
      />,
    )

    fireEvent.mouseDown(screen.getByRole('tab', { name: 'By user' }), { button: 0 })
    expect(screen.getByText('(empty name)')).toBeInTheDocument()
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'By application' }), { button: 0 })
    expect(screen.getByText('Counts per application are off.')).toBeInTheDocument()
  })
})

describe('OriginBadge', () => {
  it('SORIGIN-UI-006 reads the server origin and shows nothing without one', () => {
    const { container, rerender } = render(<OriginBadge session={{ pid: 1, origin: 'pglens' }} />)
    expect(screen.getByText('pglens')).toBeInTheDocument()

    rerender(<OriginBadge session={{ pid: 1, origin: 'application' }} />)
    expect(screen.getByText('app')).toBeInTheDocument()

    rerender(<OriginBadge session={{ pid: 1, application_name: 'pglens/check' }} />)
    expect(container).toBeEmptyDOMElement()
  })
})
