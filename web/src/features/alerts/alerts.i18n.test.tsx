import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { AlertsPage } from './AlertsPage'
import { SilencesPage } from './SilencesPage'

describe('alerts i18n (plan 006 F3)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    server.use(ok('getAlerts', []), ok('getSilences', []))
  })
  afterEach(async () => {
    server.resetHandlers()
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-038 the alerts page reads in Italian when the server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<AlertsPage />, { route: '/alerts' })

    expect(await screen.findByText('Alert ed eventi')).toBeInTheDocument()
    expect(screen.getAllByLabelText('Riepilogo degli alert').length).toBeGreaterThan(0)
    expect(screen.getByLabelText('Stato')).toBeInTheDocument()
    expect(screen.getByText('Nessun alert')).toBeInTheDocument()
  })

  it('I18N-038 the silences page reads in Italian when the server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<SilencesPage />, { route: '/alerts/silences' })

    expect(await screen.findByText('Silenziamenti degli alert')).toBeInTheDocument()
    expect(screen.getByText('Nessun silenziamento salvato')).toBeInTheDocument()
  })
})
