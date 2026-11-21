import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/api/types'
import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { CLUSTER } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { FleetPage } from './FleetPage'

const critical: Cluster = { ...(CLUSTER as unknown as Cluster), name: 'alpha', health: 'critical' }

describe('fleet i18n (plan 006 F3)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    server.use(
      ok('getClusters', [critical]),
      ok('getAlerts', []),
      ok('getReadinessSummary', { instances: [], totals: { full: 0, partial: 0 } }),
    )
  })

  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-030 fleet page reads in Italian when server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<FleetPage />, { route: '/' })

    expect(
      await screen.findByRole('heading', { name: 'Panoramica della flotta' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Filtra cluster' })).toBeInTheDocument()
    expect(screen.getByRole('list', { name: 'Schede dei cluster' })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Critici 1 cluster: Filtra cluster critici (1)' }),
    ).toBeInTheDocument()
    expect(screen.getByText('critico')).toBeInTheDocument()
  })
})
