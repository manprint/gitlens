import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { TimeRangePicker } from './TimeRangePicker'
import { renderWithProviders } from '@/test/render'

describe('TimeRangePicker', () => {
  it('UI-TIME-010 writes a selected preset to the URL', () => {
    const result = renderWithProviders(<TimeRangePicker />, { route: '/findings?range=1h' })

    fireEvent.change(screen.getByRole('combobox', { name: 'Time range preset' }), {
      target: { value: '15m' },
    })

    expect(result.router.state.location.search).toBe('?range=15m')
  })

  it('UI-TIME-011 renders Degraded for an invalid URL range and uses 1h', () => {
    renderWithProviders(<TimeRangePicker />, { route: '/findings?range=invalid' })

    expect(screen.getByRole('status')).toHaveTextContent('Degraded:')
    expect(screen.getByRole('status')).toHaveTextContent('range')
    expect(screen.getByRole('combobox', { name: 'Time range preset' })).toHaveValue('1h')
    expect(screen.getByText('Last hour')).toBeInTheDocument()
  })
})
