import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { renderWithProviders } from '@/test/render'

import { SlotsSection, type SlotRow } from './SlotsSection'

const slots: SlotRow[] = [
  {
    active: true,
    inactiveAgeSeconds: null,
    name: 'active_slot',
    owningInstance: 'primary',
    retainedBytes: 512,
    walStatus: 'Streaming',
  },
  {
    active: false,
    inactiveAgeSeconds: 86_400,
    name: 'inactive_slot',
    owningInstance: 'standby-1',
    retainedBytes: 1_536,
    walStatus: null,
  },
]

describe('SlotsSection', () => {
  it('UI-CLUS-020 inactive slots sort first', () => {
    renderWithProviders(<SlotsSection slots={slots} />)

    const rows = within(screen.getByTestId('data-table-rows')).getAllByRole('row')
    expect(rows[0]).toHaveTextContent('inactive_slot')
    expect(rows[1]).toHaveTextContent('active_slot')
  })

  it('UI-CLUS-021 retained bytes are formatted in binary units', () => {
    renderWithProviders(<SlotsSection slots={slots} />)

    expect(screen.getByText('1.5 KiB')).toBeInTheDocument()
  })

  it('UI-CLUS-024 a missing slot endpoint renders ErrorState without hiding the rest of the page', () => {
    const onRetry = vi.fn()
    renderWithProviders(
      <>
        <p>Cluster overview remains available</p>
        <SlotsSection failure={{ kind: 'not_found' }} onRetry={onRetry} />
      </>,
    )

    expect(screen.getByText('Cluster overview remains available')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load replication slots')
    fireEvent.click(screen.getByRole('button', { name: 'Retry replication slots' }))
    expect(onRetry).toHaveBeenCalledOnce()
  })
})
