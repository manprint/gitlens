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

  it('UI-TIME-012 keeps the drill-down parameters when a preset is chosen or zoomed out', () => {
    const result = renderWithProviders(<TimeRangePicker />, {
      route: '/instances/i1/ash?range=24h&db=app&group=wait_event&wait_event_type=Lock',
    })

    fireEvent.change(screen.getByRole('combobox', { name: 'Time range preset' }), {
      target: { value: '1h' },
    })
    let params = new URLSearchParams(result.router.state.location.search)
    expect(params.get('range')).toBe('1h')
    expect(params.get('db')).toBe('app')
    expect(params.get('group')).toBe('wait_event')
    expect(params.get('wait_event_type')).toBe('Lock')

    fireEvent.click(screen.getByRole('button', { name: 'Zoom out time range' }))
    params = new URLSearchParams(result.router.state.location.search)
    expect(params.get('range')).toBe('6h')
    expect(params.get('db')).toBe('app')
    expect(params.get('wait_event_type')).toBe('Lock')
  })

  it('UI-TIME-013 keeps the other parameters when an absolute range is applied', () => {
    const result = renderWithProviders(<TimeRangePicker />, {
      route: '/instances/i1/ash?range=24h&db=app&wait_event_type=Lock',
    })

    fireEvent.submit(screen.getByRole('form', { name: 'Absolute time range' }))

    const params = new URLSearchParams(result.router.state.location.search)
    expect(params.has('range')).toBe(false)
    expect(params.get('from')).not.toBeNull()
    expect(params.get('to')).not.toBeNull()
    expect(params.get('db')).toBe('app')
    expect(params.get('wait_event_type')).toBe('Lock')
  })

  it('UI-TIME-014 drops an absolute window when a preset is chosen', () => {
    const result = renderWithProviders(<TimeRangePicker />, {
      route: '/findings?from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z&db=app',
    })

    fireEvent.change(screen.getByRole('combobox', { name: 'Time range preset' }), {
      target: { value: '15m' },
    })

    const params = new URLSearchParams(result.router.state.location.search)
    expect(params.get('range')).toBe('15m')
    expect(params.has('from')).toBe(false)
    expect(params.has('to')).toBe(false)
    expect(params.get('db')).toBe('app')
  })

  it('UI-TIME-011 renders Degraded for an invalid URL range and uses 1h', () => {
    renderWithProviders(<TimeRangePicker />, { route: '/findings?range=invalid' })

    expect(screen.getByRole('status')).toHaveTextContent('Degraded:')
    expect(screen.getByRole('status')).toHaveTextContent('range')
    expect(screen.getByRole('combobox', { name: 'Time range preset' })).toHaveValue('1h')
    expect(screen.getByText('Last hour')).toBeInTheDocument()
  })
  it('refuses a custom range that ends before it starts, and keeps the URL', () => {
    const result = renderWithProviders(<TimeRangePicker />, { route: '/findings?range=1h' })

    fireEvent.change(screen.getByLabelText('From'), { target: { value: '2026-01-02T00:00' } })
    fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-01-01T00:00' } })
    fireEvent.submit(screen.getByRole('form', { name: 'Absolute time range' }))

    expect(screen.getByRole('alert')).toHaveTextContent('The start must not be later than the end.')
    expect(result.router.state.location.search).toBe('?range=1h')
  })

  it('refuses a custom range longer than 30 days', () => {
    const result = renderWithProviders(<TimeRangePicker />, { route: '/findings?range=1h' })

    fireEvent.change(screen.getByLabelText('From'), { target: { value: '2026-01-01T00:00' } })
    fireEvent.change(screen.getByLabelText('To'), { target: { value: '2026-03-01T00:00' } })
    fireEvent.submit(screen.getByRole('form', { name: 'Absolute time range' }))

    expect(screen.getByRole('alert')).toHaveTextContent('The range cannot be longer than 30 days.')
    expect(result.router.state.location.search).toBe('?range=1h')
  })

  it('zooms an absolute range out to the shortest longer preset', () => {
    const result = renderWithProviders(<TimeRangePicker />, {
      route: '/findings?from=2026-01-01T00:00:00Z&to=2026-01-01T02:00:00Z',
    })

    fireEvent.click(screen.getByRole('button', { name: 'Zoom out time range' }))

    expect(new URLSearchParams(result.router.state.location.search).get('range')).toBe('6h')
  })
})
