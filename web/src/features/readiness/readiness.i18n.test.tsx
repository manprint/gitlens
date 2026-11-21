import { screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { partial as partialReport } from '@/test/fixtures/getReadiness'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { ReadinessSection } from './ReadinessSection'

describe('readiness i18n (plan 006 F3)', () => {
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-041 readiness section reads in Italian when server language is it', async () => {
    await setServerLanguage('it')
    server.use(ok('getReadiness', partialReport))
    renderWithProviders(<ReadinessSection instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?section=readiness`,
    })
    await flushNetwork()

    expect(screen.getByRole('heading', { name: 'Readiness' })).toBeInTheDocument()
    expect(screen.getAllByText('Avviso').length).toBeGreaterThan(0)
    expect(
      screen.getAllByRole('button', { name: /Nascondi dettagli|Mostra dettagli/ }).length,
    ).toBeGreaterThan(0)
  })
})
