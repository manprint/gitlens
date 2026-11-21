import { fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { windowLabel } from '@/lib/advisorSettings'
import { channelLabel, notificationChannelInfo } from '@/lib/notificationChannels'
import { resetPreferences } from '@/lib/preferences'
import { retentionFamilyLabel, validateRetentionDays } from '@/lib/retention'
import { CLUSTER, INSTANCE_ID, INSTANCE_SUMMARY, NOW } from '@/test/fixture-helpers'
import { base as hostBase } from '@/test/fixtures/getInstanceHost'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { preloadLazyRoutes, renderWithProviders } from '@/test/render'

import { SettingsPage } from './SettingsPage'

describe('settings i18n (plan 006 F3)', () => {
  beforeAll(preloadLazyRoutes)
  beforeEach(() => {
    vi.useRealTimers()
    server.use(
      ok('getInstances', [INSTANCE_SUMMARY]),
      ok('getClusters', [CLUSTER]),
      ok('getAlerts', []),
      ok('getAdvisorRules', []),
      ok('getInstanceCommandAudit', []),
      ok('getSession', { authenticated: true, configured: true, expires_at: NOW }),
      ok('getInstanceDatabases', {
        instance_id: INSTANCE_ID,
        databases: [],
        not_monitored_count: 0,
      }),
      ok('getInstanceHost', hostBase),
    )
  })
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-039 the settings page reads in Italian when the server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<SettingsPage />)

    expect(
      await screen.findByRole('heading', { name: 'Impostazioni e inventario' }),
    ).toBeInTheDocument()
    const views = screen.getByRole('tablist', { name: 'Viste dell’inventario' })
    for (const name of ['Istanze', 'Livelli di permessi', 'Agent', 'Database per istanza']) {
      expect(within(views).getByRole('tab', { name })).toBeInTheDocument()
    }

    fireEvent.mouseDown(within(views).getByRole('tab', { name: 'Livelli di permessi' }))
    expect(await screen.findByRole('heading', { name: 'Livello T0' })).toBeInTheDocument()
    expect(screen.getByText('Legge solo le statistiche.')).toBeInTheDocument()
  })

  it('I18N-040 the retention, advisor and notification texts read in Italian', async () => {
    await setServerLanguage('it')
    expect(retentionFamilyLabel('metrics_ash')).toBe('Active Session History')
    expect(validateRetentionDays('2', 3, 365)).toEqual({
      ok: false,
      error: 'Inserisci un numero di giorni tra 3 e 365.',
    })
    expect(windowLabel('window.unused_index')).toBe('Storico indici inutilizzati')
    expect(channelLabel(notificationChannelInfo('webhook'))).toBe('Webhook generico')
  })
})
