import { screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { CAPTIONS } from '@/lib/captions'
import type { AdvisorRule, FindingRecord } from '@/lib/findings'
import { CLUSTER } from '@/test/fixture-helpers'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'
import { expectCaptionedCards } from '@/test/clearCard'

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

beforeEach(() => {
  server.use(
    ok('getClusters', [CLUSTER]),
    ok('getFindings', [finding]),
    ok('getAdvisorRules', [rule]),
  )
})

/** The visible caption line: a one-line caption carries its sentence in `title`. */
function expectCaptionLine(what: string) {
  expect(screen.getAllByTitle(what)[0], what).toBeVisible()
}

describe('findings captions (CAPTION-PAGE)', () => {
  it('CAPTION-PAGE-004 the findings tab captions its section and its filters', async () => {
    const { container } = renderWithProviders(<FindingsPage />, { route: '/findings' })
    await flushNetwork(30)

    expect(expectCaptionedCards(container)).toEqual(['findings.list'])
    expectCaptionLine(CAPTIONS['findings.list'].what)
    for (const name of [
      'About State filter',
      'About Severity filter',
      'About Scope filter',
      'About Finding summary',
    ]) {
      expect(screen.getByRole('button', { name })).toBeVisible()
    }
  })

  it('CAPTION-PAGE-005 the rule catalogue captions its section, table and tier filter', async () => {
    const { container } = renderWithProviders(<FindingsPage />, {
      route: '/findings?view=rules',
    })
    await flushNetwork(30)

    expect(expectCaptionedCards(container)).toEqual([
      'findings.rule-catalogue',
      'findings.rule-catalogue-table',
    ])
    expectCaptionLine(CAPTIONS['findings.rule-catalogue-table'].what)
    expect(screen.getByRole('button', { name: 'About Available at or below filter' })).toBeVisible()
  })
})
