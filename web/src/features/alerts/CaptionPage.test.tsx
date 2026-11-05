import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AlertRule } from '@/api/alerts'
import type { Schemas } from '@/api/types'
import { captionOf, type CaptionId } from '@/lib/captions'
import { expectCaptionedCards } from '@/test/clearCard'
import { flushNetwork } from '@/test/network'
import { ok } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { AlertsPage } from './AlertsPage'
import { EventsPage } from './EventsPage'
import { RulesPage } from './RulesPage'
import { SilencesPage } from './SilencesPage'

type Alert = Schemas['Alert']
type Silence = Schemas['Silence']

const instant = '2030-01-01T00:00:00Z'

const ALERT: Alert = {
  alert_key: 'replication-lag',
  rule_id: 'replication-lag',
  severity: 'warning',
  state: 'firing',
  cluster_id: '9007199254740993',
  instance_id: '00000000-0000-4000-8000-000000000010',
  datname: 'app',
  labels: { team: 'database' },
  value: 12,
  summary: 'Replication lag is above the threshold',
  started_at: instant,
  last_eval_at: instant,
  resolved_at: null,
  suppressed: false,
}

const SILENCE: Silence = {
  silence_id: '00000000-0000-4000-8000-000000000001',
  matchers: [{ name: 'rule_id', value: 'replication-lag' }],
  reason: 'maintenance window',
  starts_at: '2020-01-01T00:00:00Z',
  ends_at: '2090-01-01T00:00:00Z',
}

const RULE: AlertRule = {
  enabled: true,
  for_seconds: 60,
  id: 'replication-lag',
  min_tier: 'T1',
  needs: ['metric'],
  scope: 'instance',
  severity: 'warning',
  threshold: 10,
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

async function renderAlerts(route = '/alerts') {
  server.use(ok('getAlerts', [ALERT]), ok('getSilences', [SILENCE]))
  const view = renderWithProviders(<AlertsPage />, { route })
  await settle()
  return view
}

async function renderEvents(route = '/events?range=1h') {
  server.use(
    ok('getEvents', [
      {
        cluster_id: '1',
        event_id: 1,
        instance_id: null,
        payload: {},
        ts: '2026-09-01T04:00:00Z',
        type: 'agent_up',
      },
    ]),
    ok('getAlerts', [ALERT]),
    ok('getSilences', []),
  )
  const view = renderWithProviders(<EventsPage />, { route })
  await settle()
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

function expectHints(labels: string[]) {
  for (const label of labels) {
    expect(screen.getByRole('button', { name: `About ${label}` }), label).toBeInTheDocument()
  }
}

describe('alerts captions', () => {
  it('CAPTION-PAGE-001 the Alerts section, its filters and the summary are captioned', async () => {
    const { container } = await renderAlerts()

    expect(expectCaptionedCards(container)).toEqual(['alerts.list'])
    expect(visibleCaptions('alerts.list')).toHaveLength(1)
    expectHints([
      'Alerts',
      'State filter',
      'Severity filter',
      'Cluster filter',
      'Suppression filter',
      'Alert summary counts',
    ])
  })

  it('CAPTION-PAGE-002 the cluster filter says it matches the cluster ID exactly', async () => {
    await renderAlerts()

    expect(captionOf('alerts.filter-cluster').what).toMatch(/cluster ID equals/)
    expect(screen.getByLabelText('Cluster')).toHaveAttribute('placeholder', 'Cluster ID')
    expect(screen.getByRole('button', { name: 'About Cluster filter' })).toBeInTheDocument()
  })

  it('CAPTION-PAGE-003 an opened alert shows its detail caption', async () => {
    server.use(ok('getAlert', ALERT))
    const { container } = await renderAlerts()

    fireEvent.click(screen.getByRole('button', { name: 'replication-lag' }))
    await settle()

    expect(expectCaptionedCards(container)).toEqual(['alerts.detail', 'alerts.list'])
    expect(visibleCaptions('alerts.detail')).toHaveLength(1)
  })

  it('CAPTION-PAGE-004 the delivery tab is captioned', async () => {
    const { container } = await renderAlerts('/alerts?section=delivery')

    expect(expectCaptionedCards(container)).toEqual(['alerts.delivery'])
    expect(visibleCaptions('alerts.delivery')).toHaveLength(1)
  })

  it('CAPTION-PAGE-005 the event timeline and its filters are captioned', async () => {
    const { container } = await renderEvents()

    expect(expectCaptionedCards(container)).toEqual(['events.timeline'])
    expect(visibleCaptions('events.timeline')).toHaveLength(1)
    expectHints(['Event timeline', 'Cluster filter', 'Event type filter', 'Event limit filter'])
    expect(captionOf('events.filter-cluster').what).toMatch(/cluster whose ID is typed/)
  })

  it('CAPTION-PAGE-006 the alert status tab is captioned', async () => {
    const { container } = await renderEvents('/events?range=1h&section=alerts')

    expect(expectCaptionedCards(container)).toEqual(['events.alert-status'])
    expect(visibleCaptions('events.alert-status')).toHaveLength(1)
  })

  it('CAPTION-PAGE-007 the rule list and the rule editor are captioned', async () => {
    server.use(ok('getAlertRules', [RULE]))
    const { container } = renderWithProviders(<RulesPage />, { route: '/alerts/rules' })
    await flushNetwork()

    expect(expectCaptionedCards(container)).toEqual(['rules.list'])
    expect(visibleCaptions('rules.list')).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: 'Edit replication-lag' }))

    expect(expectCaptionedCards(container)).toEqual(['rules.edit', 'rules.list'])
    expect(visibleCaptions('rules.edit')).toHaveLength(1)
  })

  it('CAPTION-PAGE-008 every rule field states its effect', async () => {
    server.use(ok('getAlertRules', [RULE]))
    renderWithProviders(<RulesPage />, { route: '/alerts/rules' })
    await flushNetwork()
    fireEvent.click(screen.getByRole('button', { name: 'Edit replication-lag' }))

    expect(screen.getByLabelText('Threshold')).toHaveAccessibleDescription(/metric crosses/)
    expect(screen.getByLabelText('Duration (seconds)')).toHaveAccessibleDescription(
      /must hold before the alert fires/,
    )
    expect(screen.getByLabelText('Severity')).toHaveAccessibleDescription(/ranks them/)
    expect(screen.getByLabelText('Enabled')).toHaveAccessibleDescription(/not evaluated/)
  })

  it('CAPTION-PAGE-009 the stored silences are captioned and the editor explains its fields', async () => {
    server.use(ok('getSilences', [SILENCE]), ok('getAlerts', [ALERT]))
    const { container } = renderWithProviders(<SilencesPage />, { route: '/alerts/silences' })
    await flushNetwork()

    expect(expectCaptionedCards(container)).toEqual(['silences.list'])
    expect(visibleCaptions('silences.list')).toHaveLength(1)

    fireEvent.click(screen.getByRole('button', { name: 'Create silence' }))

    expect(screen.getByLabelText('Matcher 1 name')).toBeInTheDocument()
    expect(screen.getByText(/matches every matcher/)).toBeVisible()
    expect(screen.getByLabelText('Start')).toHaveAccessibleDescription(/in effect from Start/)
  })
})
