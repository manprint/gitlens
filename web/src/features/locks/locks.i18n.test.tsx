import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { CLUSTER_ID, INSTANCE_ID, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { LocksPage } from './BlockingTree'

const locksResponse = {
  instance_id: INSTANCE_ID,
  sampled_at: '2026-09-01T10:00:00Z',
  stale: false,
  nodes: [],
  sessions: [],
}

describe('locks i18n (plan 006 F3)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    server.use(
      ok('getInstance', {
        ...INSTANCE_SUMMARY,
        cluster_id: CLUSTER_ID,
        databases: [],
        databases_not_monitored: 0,
      }),
      ok('getLocks', locksResponse),
      ok('getInstanceActivity', {
        instance_id: INSTANCE_ID,
        stale: false,
        metrics: {},
        xmin_horizon: null,
      }),
      ok('getEvents', []),
    )
  })
  afterEach(async () => {
    server.resetHandlers()
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-035 the locks page reads in Italian when the server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<LocksPage instanceId="instance-1" />, {
      route: '/instances/instance-1/locks',
    })

    expect(await screen.findByRole('heading', { name: 'Lock e attività' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Albero di blocco' })).toBeInTheDocument()
    expect(
      await screen.findByText('Nessuna contesa di lock nell’ultimo campione'),
    ).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Attività' })).toBeInTheDocument()
    expect(await screen.findByText('Connessioni in uso')).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Incidenti catturati' })).toBeInTheDocument()
  })
})
