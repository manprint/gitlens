import { screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/api/types'
import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { CLUSTER, INSTANCE_ID } from '@/test/fixture-helpers'
import { makeGetInstance } from '@/test/fixtures/getInstance'
import { makeGetInstanceDatabases } from '@/test/fixtures/getInstanceDatabases'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { InstancePage } from './InstancePage'

vi.mock('echarts-for-react/esm/core', () => ({
  default: () => null,
}))

describe('instance i18n (plan 006 F3)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    server.use(
      ok('getInstance', makeGetInstance()),
      ok('getClusters', [CLUSTER as unknown as Cluster]),
      ok('getInstanceDatabases', makeGetInstanceDatabases()),
      ok('queryMetrics', { series: [] }),
    )
  })
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-032 the instance page reads in Italian when the server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<InstancePage instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}`,
    })

    const tabs = await screen.findByRole('tablist', { name: 'Sezioni dell’istanza' })
    expect(within(tabs).getByRole('tab', { name: 'Impostazioni' })).toBeInTheDocument()
    expect(within(tabs).getByRole('tab', { name: 'Interni' })).toBeInTheDocument()
    expect(
      within(tabs).getByRole('tab', { name: 'Panoramica', selected: true }),
    ).toBeInTheDocument()
  })
})
