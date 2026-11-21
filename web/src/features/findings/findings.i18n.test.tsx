import { screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdvisorRule, FindingRecord } from '@/lib/findings'
import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { resetPreferences } from '@/lib/preferences'
import { CLUSTER } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { FindingsPage } from './FindingsPage'

const finding: FindingRecord = {
  finding_id: 'finding-1',
  rule_id: 'rule-1',
  severity: 'warning',
  state: 'open',
  scope: 'cluster',
  title: 'Replication lag',
  evidence: {},
}

const rule: AdvisorRule = {
  id: 'rule-1',
  severity: 'warning',
  scope: 'cluster',
  needs: [],
  min_tier: 'T0',
  enabled: true,
  overridden: false,
}

describe('findings i18n (plan 006 F3)', () => {
  beforeEach(() => {
    vi.useRealTimers()
    server.use(
      ok('getClusters', [CLUSTER]),
      ok('getFindings', [finding]),
      ok('getAdvisorRules', [rule]),
    )
  })
  afterEach(async () => {
    server.resetHandlers()
    resetPreferences()
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('I18N-037 the findings page reads in Italian when the server language is it', async () => {
    await setServerLanguage('it')
    renderWithProviders(<FindingsPage />, { route: '/findings' })

    expect(
      await screen.findByRole('heading', { name: 'Segnalazioni dell’advisor' }),
    ).toBeInTheDocument()
    const summary = screen.getByRole('group', { name: 'Riepilogo delle segnalazioni' })
    expect(within(summary).getByRole('button', { name: /Avviso\s+1/ })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Catalogo delle regole' })).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: 'Stato' })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Mostra i dettagli di Replication lag' }),
    ).toBeInTheDocument()
  })
})
