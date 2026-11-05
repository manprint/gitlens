import { act, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Cluster } from '@/api/types'
import { captionOf, type CaptionId } from '@/lib/captions'
import { partial as readinessPartial } from '@/test/fixtures/getReadinessSummary'
import { base as alertBase } from '@/test/fixtures/getAlert'
import { CLUSTER, INSTANCE_SUMMARY } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'
import { expectCaptionedCards, expectClearCard } from '@/test/clearCard'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'

import { FleetPage } from './FleetPage'

async function renderFleet() {
  const alert = {
    ...alertBase,
    alert_key: `agent_down/${INSTANCE_SUMMARY.instance_id}`,
    rule_id: 'agent_down',
    instance_id: INSTANCE_SUMMARY.instance_id,
    labels: { cause: 'agent stopped reporting' },
    summary: 'Agent is down',
  }
  server.use(ok('getClusters', [CLUSTER as unknown as Cluster]), ok('getAlerts', [alert]))
  const view = renderWithProviders(<FleetPage />)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0)
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

const SUMMARY_TILES: [CaptionId, string][] = [
  ['fleet.summary-all', 'All clusters'],
  ['fleet.summary-healthy', 'Healthy'],
  ['fleet.summary-degraded', 'Degraded'],
  ['fleet.summary-critical', 'Critical'],
  ['fleet.summary-instances-up', 'Instances up'],
  ['fleet.summary-instances-down', 'Instances down'],
  ['fleet.summary-firing-alerts', 'Firing alerts'],
]

describe('fleet captions', () => {
  beforeEach(() => {
    server.use(ok('getReadinessSummary', { instances: [], totals: { full: 0, partial: 0 } }))
  })

  it('CAPTION-PAGE-001 the Clusters section is captioned and its caption is visible', async () => {
    const { container } = await renderFleet()

    expect(expectCaptionedCards(container)).toContain('fleet.clusters')
    expect(visibleCaptions('fleet.clusters')).toHaveLength(1)
  })

  it('CAPTION-PAGE-002 every summary card has an About hint backed by the catalogue', async () => {
    await renderFleet()

    for (const [id, label] of SUMMARY_TILES) {
      expect(screen.getByRole('button', { name: `About ${label}` }), id).toBeInTheDocument()
      expect(captionOf(id).what.length).toBeGreaterThan(0)
    }
  })

  it('CARD-ALL-001 every summary card shows one figure and no bare second number', async () => {
    await renderFleet()

    for (const [, label] of SUMMARY_TILES) {
      expectClearCard(
        screen.getByRole('button', { name: new RegExp(`^${label} \\d+|^${label} Unavailable`) }),
      )
    }
    expect(screen.queryAllByRole('article')).toHaveLength(0)
  })

  it('CAPTION-PAGE-004 each cluster card explains its permission tiers and readiness', async () => {
    server.use(ok('getReadinessSummary', readinessPartial))
    await renderFleet()
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })

    expect(screen.getByRole('button', { name: 'About Permission tiers' })).toBeInTheDocument()
    expect(captionOf('fleet.permission-tiers').what.length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: 'About Readiness' })).toBeInTheDocument()
  })

  it('CAPTION-PAGE-003 the agent health strip carries its hint', async () => {
    await renderFleet()

    expect(
      screen.getByRole('button', { name: 'About Agents requiring attention' }),
    ).toBeInTheDocument()
  })
})
