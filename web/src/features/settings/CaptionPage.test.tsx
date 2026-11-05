import { act, screen, waitFor } from '@testing-library/react'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import type { Schemas } from '@/api/types'
import { captionOf, type CaptionId } from '@/lib/captions'
import { base as hostBase } from '@/test/fixtures/getInstanceHost'
import { CLUSTER, INSTANCE_ID, INSTANCE_SUMMARY, NOW } from '@/test/fixture-helpers'
import { expectCaptionedCards } from '@/test/clearCard'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { preloadLazyRoutes, renderWithProviders } from '@/test/render'

import { SettingsPage } from './SettingsPage'

type AdvisorRule = Schemas['AdvisorRule']

const RULES: AdvisorRule[] = [
  {
    id: 'connections',
    severity: 'warning',
    scope: 'instance',
    needs: ['activity'],
    min_tier: 'T0',
    enabled: true,
    overridden: false,
  },
]

const AUDIT_ENTRY = {
  audit_id: 5,
  command_id: '00000000-0000-4000-8000-000000000005',
  instance_id: INSTANCE_ID,
  kind: 'cancel',
  args: { pid: 44 },
  requested_by: 'ui-session 9f86d081 from 192.0.2.10',
  executed_at: '2026-09-01T02:00:00Z',
  outcome: 'requested',
}

async function settle() {
  vi.useRealTimers()
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 25))
  })
}

async function renderSettings(route: string) {
  server.use(
    ok('getInstances', [INSTANCE_SUMMARY]),
    ok('getClusters', [CLUSTER]),
    ok('getAlerts', []),
    ok('getAdvisorRules', RULES),
    ok('getInstanceCommandAudit', [AUDIT_ENTRY]),
    ok('getSession', { authenticated: true, configured: true, expires_at: NOW }),
    ok('getInstanceDatabases', {
      instance_id: INSTANCE_ID,
      databases: [
        { datname: 'app', monitored: true },
        { datname: 'scratch', monitored: false, skip_reason: 'db_budget' },
      ],
      not_monitored_count: 1,
    }),
    ok('getInstanceHost', hostBase),
  )
  const view = renderWithProviders(<SettingsPage />, { route })
  await settle()
  await waitFor(() => {
    expect(view.container.querySelector('[data-card]')).not.toBeNull()
    expect(view.container.querySelector('[aria-busy="true"]')).toBeNull()
  })
  return view
}

function visibleCaptions(id: CaptionId) {
  return screen.getAllByText(captionOf(id).what).filter((element) => {
    try {
      expect(element).toBeVisible()
      return true
    } catch {
      return false
    }
  })
}

describe('settings captions', () => {
  beforeAll(preloadLazyRoutes)

  it('CAPTION-PAGE-001 the Instances view is captioned', async () => {
    const { container } = await renderSettings('/')

    expect(expectCaptionedCards(container)).toEqual([
      'settings.inventory-instances',
      'settings.inventory-instances-table',
    ])
    expect(visibleCaptions('settings.inventory-instances')).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'About Instances' })).toBeInTheDocument()
  })

  it('CAPTION-PAGE-002 the Permission tiers view is captioned', async () => {
    const { container } = await renderSettings('/?view=tiers')

    expect(expectCaptionedCards(container)).toEqual(['settings.permission-tiers'])
    expect(visibleCaptions('settings.permission-tiers')).toHaveLength(1)
  })

  it('CAPTION-PAGE-003 the Agents view is captioned and still says it is derived', async () => {
    const { container } = await renderSettings('/?view=agents')

    expect(expectCaptionedCards(container)).toEqual([
      'settings.inventory-agents',
      'settings.inventory-agents-table',
    ])
    expect(visibleCaptions('settings.inventory-agents')).toHaveLength(1)
    expect(screen.getByText(/no separate list of agents/i)).toBeVisible()
  })

  it('CAPTION-PAGE-004 the Databases view is captioned for each instance', async () => {
    const { container } = await renderSettings('/?view=databases')
    await screen.findByRole('table', { name: /^Databases for / })

    expect(expectCaptionedCards(container)).toEqual([
      'settings.inventory-databases',
      'settings.inventory-databases-instance',
      'settings.inventory-databases-table',
    ])
    expect(visibleCaptions('settings.inventory-databases-instance')).toHaveLength(1)
    expect(screen.getByText(/Databases not monitored: 1/)).toBeVisible()
  })

  it('CAPTION-PAGE-005 the Command audit view is captioned', async () => {
    const { container } = await renderSettings('/?tab=audit')
    await screen.findByRole('table', { name: /^Command audit for / })

    expect(expectCaptionedCards(container)).toEqual(
      expect.arrayContaining([
        'settings.command-audit',
        'settings.command-audit-instance',
        'settings.command-audit-table',
      ]),
    )
    expect(visibleCaptions('settings.command-audit')).toHaveLength(1)
    expect(visibleCaptions('settings.command-audit-table')).toHaveLength(1)
  })

  it('CAPTION-PAGE-006 the Server info view captions each of its four sections', async () => {
    const { container } = await renderSettings('/?tab=audit&view=server')

    expect(expectCaptionedCards(container)).toEqual([
      'settings.server-info',
      'settings.product-limits',
      'settings.authentication',
      'settings.references',
    ])
    for (const id of [
      'settings.server-info',
      'settings.product-limits',
      'settings.authentication',
      'settings.references',
    ] as const) {
      expect(visibleCaptions(id), id).toHaveLength(1)
    }
    expect(screen.getByRole('link', { name: 'Product limits documentation' })).toBeInTheDocument()
  })
})
