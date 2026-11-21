import { cleanup, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { renderWithProviders } from '@/test/render'

const mocks = vi.hoisted(() => ({
  ash: vi.fn(),
  ashTop: vi.fn(),
  settings: vi.fn(),
  compare: vi.fn(),
}))

vi.mock('@/api/queries', () => ({
  useAsh: mocks.ash,
  useAshTop: mocks.ashTop,
  useInstanceSettings: mocks.settings,
  useAshCompare: mocks.compare,
}))

import { AshPage } from './AshPage'

const rows = [
  {
    key: 'Lock',
    status: 'both',
    a: { aas: 1 },
    b: { aas: 4 },
    delta: { aas: 3 },
    ratio: { aas: 4 },
  },
  { key: 'IO', status: 'new', a: null, b: { aas: 0.5 }, delta: { aas: 0.5 }, ratio: { aas: null } },
  {
    key: 'Client',
    status: 'gone',
    a: { aas: 0.25 },
    b: null,
    delta: { aas: -0.25 },
    ratio: { aas: 0 },
  },
]

const ROUTE =
  '/instances/instance-1/ash?mode=compare&group=wait_event_type&usename=app&from=2026-11-20T10:00:00Z&to=2026-11-20T11:00:00Z'

describe('AshCompare', () => {
  beforeEach(() => {
    vi.useRealTimers()
    mocks.compare.mockReturnValue({
      data: { a: {}, b: {}, group_by: 'wait_event_type', rows },
      error: null,
      refetch: vi.fn(),
    })
  })

  afterEach(async () => {
    cleanup()
    vi.clearAllMocks()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('UI-CMP-004 Activity compare renders new and gone rows', () => {
    renderWithProviders(<AshPage instanceId="instance-1" />, { route: ROUTE })

    expect(mocks.compare).toHaveBeenLastCalledWith({
      instance_id: 'instance-1',
      group_by: 'wait_event_type',
      limit: 50,
      a_from: '2026-11-20T09:00:00.000Z',
      a_to: '2026-11-20T10:00:00.000Z',
      b_from: '2026-11-20T10:00:00.000Z',
      b_to: '2026-11-20T11:00:00.000Z',
      usename: 'app',
    })
    expect(mocks.ash).not.toHaveBeenCalled()
    const table = screen.getByRole('table', {
      name: 'Active sessions compared between two periods',
    })
    const body = within(table).getAllByRole('row').slice(1)
    expect(body.map((row) => row.textContent)).toEqual([
      'Lock1.004.00+3.00×4.0',
      'IO—0.50+0.50new',
      'Client0.25—−0.25gone',
    ])
    expect(within(table).getByText('×4.0')).toHaveAttribute('data-variant', 'warning')
  })

  it('UI-CMP-005 the compare view reads in Italian', async () => {
    await setServerLanguage('it')
    renderWithProviders(<AshPage instanceId="instance-1" />, { route: ROUTE })

    expect(
      await screen.findByRole('heading', { name: 'Confronta due periodi' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('radiogroup', { name: 'Modalità della pagina' })).toBeInTheDocument()
    expect(screen.getByLabelText('Periodo A')).toHaveDisplayValue('Periodo precedente')
    const table = screen.getByRole('table', { name: 'Sessioni attive confrontate tra due periodi' })
    expect(within(table).getByText('Variazione')).toBeInTheDocument()
    expect(within(table).getByText('nuovo')).toBeInTheDocument()
    expect(within(table).getByText('sparito')).toBeInTheDocument()
    expect(within(table).getByText('×4,0')).toBeInTheDocument()
  })
})
