import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { renderWithProviders } from '@/test/render'

const mocks = vi.hoisted(() => ({
  ash: vi.fn(),
  ashTop: vi.fn(),
  settings: vi.fn(),
}))

vi.mock('@/api/queries', () => ({
  useAsh: mocks.ash,
  useAshTop: mocks.ashTop,
  useInstanceSettings: mocks.settings,
}))

vi.mock('echarts-for-react/esm/core', () => ({
  default: () => <div data-testid="mock-echarts" />,
}))

import { AshPage } from './AshPage'

const response = {
  buckets: [
    {
      avg_active_sessions: 1,
      samples: 10,
      ticks: 10,
      ts: '2026-09-01T10:00:00Z',
      wait_event: 'ClientRead',
      wait_event_type: 'Client',
    },
  ],
  enabled: true,
  resolution_seconds: 30,
  statistical: false,
}

function success<T>(data: T) {
  return {
    data,
    dataAge: 0,
    dataUpdatedAt: Date.now(),
    error: null,
    isPending: false,
    refetch: vi.fn(),
  }
}

describe('ash i18n (plan 006 F3)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    mocks.ash.mockReturnValue(success(response))
    mocks.ashTop.mockReturnValue(success({ enabled: true, entries: [] }))
    mocks.settings.mockReturnValue(success({ instance_id: 'instance-1', settings: [] }))
  })

  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-036 ASH page reads in Italian when server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<AshPage instanceId="instance-1" />, {
      route: '/instances/instance-1/ash?range=1h',
    })

    expect(
      await screen.findByRole('heading', { name: 'Analisi ASH e wait event' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('radiogroup', { name: 'Layout del grafico' })).toBeInTheDocument()
    expect(screen.getByLabelText('Impilati')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Tipi di wait event' })).toBeInTheDocument()
    expect(screen.getByText(/campionamento statistico di 1 s/)).toBeInTheDocument()
  })
})
