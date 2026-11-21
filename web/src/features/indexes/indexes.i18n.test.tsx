import { screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { base as clusters } from '@/test/fixtures/getClusters'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import IndexAnalysisPage from './IndexAnalysisPage'

vi.mock('echarts-for-react/esm/core', () => ({ default: () => null }))

const INDEX = {
  datname: 'app',
  schemaname: 'public',
  relname: 'orders',
  indexrelname: 'orders_customer_idx',
  index_bytes: 8192,
  idx_scan_rate: 0.5,
  is_primary: false,
  is_unique: false,
  is_valid: true,
  last_scan_status: 'scanned',
  last_scan_at: '2026-08-27T01:00:00Z',
  definition: 'CREATE INDEX orders_customer_idx ON public.orders USING btree (customer_id)',
  ts: '2026-08-27T02:00:00Z',
}

describe('indexes i18n (plan 006 F3)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    server.use(
      ok('getClusters', clusters),
      ok('getInstanceIndexes', {
        instance_id: INSTANCE_ID,
        items: [INDEX],
        truncated: false,
        relations_not_reported: null,
      }),
      ok('getFindings', []),
    )
  })
  afterEach(async () => {
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-034 the index analysis page reads in Italian when the server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<IndexAnalysisPage />, { route: '/indexes' })

    expect(await screen.findByRole('heading', { name: 'Analisi degli indici' })).toBeInTheDocument()
    expect(screen.getByText('Gestione degli indici')).toBeInTheDocument()
    expect(await screen.findByRole('tab', { name: 'Indici mancanti' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Pulizia' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Costo degli indici' })).toBeInTheDocument()
  })
})
