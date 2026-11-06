import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { captionOf, type CaptionId } from '@/lib/captions'
import { expectCaptionedCards, expectClearCard } from '@/test/clearCard'
import { withOrigins, withTruncatedConnections } from '@/test/fixtures/getLocks'
import { renderWithProviders } from '@/test/render'
import { NOW } from '@/test/time'

const mocks = vi.hoisted(() => ({
  instance: vi.fn(),
  locks: vi.fn(),
  activity: vi.fn(),
}))

vi.mock('@/api/queries', () => ({
  useInstance: mocks.instance,
  useLocks: mocks.locks,
  useInstanceActivity: mocks.activity,
}))

import { ActivitySection, type ActivityResponseLike } from './ActivitySection'
import { BlockingTree, LocksPage } from './BlockingTree'

function success<T>(data: T) {
  return {
    data,
    dataAge: 0,
    dataUpdatedAt: Date.now(),
    error: null,
    isError: false,
    isPending: false,
    refetch: vi.fn(),
  }
}

function metric(value: number | null, labels: Record<string, string> = {}) {
  return { ts: NOW.toISOString(), value, labels }
}

const fullMetrics: ActivityResponseLike['metrics'] = {
  pg_connections_used: [metric(81)],
  pg_connections_limit: [metric(100)],
  pg_connections_used_ratio: [metric(0.81)],
  pg_max_xact_age_seconds: [metric(120)],
  pg_max_idle_in_transaction_seconds: [metric(45)],
  pg_max_state_age_seconds: [metric(30, { state: 'idle' })],
  pg_prepared_xacts: [metric(2)],
  pg_oldest_prepared_xact_seconds: [metric(900)],
  pg_max_datfrozenxid_age: [metric(150_000_000)],
  pg_deadlocks_total: [metric(3)],
  pg_activity_by_state: [metric(4, { state: 'active' }), metric(9, { state: 'idle' })],
  pg_activity_by_database: [metric(13, { datname: 'app' })],
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

function expectAllTilesClear() {
  const tiles = screen.getAllByRole('article')
  expect(tiles.length).toBeGreaterThan(0)
  tiles.forEach(expectClearCard)
}

beforeEach(() => {
  mocks.instance.mockReset()
  mocks.locks.mockReset()
  mocks.activity.mockReset()
})

describe('locks and activity captions', () => {
  it('CAPTION-PAGE-040 the activity section captions its tiles and blocks', () => {
    const { container } = render(
      <ActivitySection
        connections={withOrigins.connections}
        now={NOW}
        response={{ stale: false, metrics: fullMetrics }}
      />,
    )

    const ids = expectCaptionedCards(container)
    expect(ids).toContain('activity.section')
    expect(ids).toContain('activity.longest-transaction')
    expect(visibleCaptions('activity.section').length).toBeGreaterThan(0)
  })

  it('CAPTION-PAGE-041 every activity tile is a clear card (CARD-ALL-001)', () => {
    render(
      <ActivitySection
        connections={withOrigins.connections}
        now={NOW}
        response={{ stale: false, metrics: fullMetrics }}
      />,
    )

    expect(screen.getAllByRole('article')).toHaveLength(8)
    expectAllTilesClear()
  })

  it('CAPTION-PAGE-045 the prepared transactions tile names its noun, and an unknown figure carries none', () => {
    const view = render(
      <ActivitySection
        now={NOW}
        response={{ stale: false, metrics: { pg_prepared_xacts: [metric(0)] } }}
      />,
    )
    expect(screen.getByRole('article', { name: 'Prepared transactions' })).toHaveTextContent(
      /0\s*transactions$/,
    )
    view.unmount()

    const one = render(
      <ActivitySection
        now={NOW}
        response={{ stale: false, metrics: { pg_prepared_xacts: [metric(1)] } }}
      />,
    )
    expect(screen.getByRole('article', { name: 'Prepared transactions' })).toHaveTextContent(
      /1\s*transaction$/,
    )
    one.unmount()

    render(<ActivitySection now={NOW} response={{ stale: false, metrics: {} }} />)
    const unknown = screen.getByRole('article', { name: 'Prepared transactions' })
    expect(unknown).not.toHaveTextContent(/\d/)
    expect(unknown).toHaveTextContent(/^Prepared transactions—$/)
  })

  it('CAPTION-PAGE-042 tiles stay clear without data and with a cut sample', () => {
    const empty = render(<ActivitySection now={NOW} response={{ stale: false, metrics: {} }} />)
    expect(expectCaptionedCards(empty.container)).toContain('activity.section')
    expectAllTilesClear()
    empty.unmount()

    const cut = render(
      <ActivitySection
        connections={withTruncatedConnections.connections}
        now={NOW}
        response={{ stale: false, metrics: {} }}
      />,
    )
    expect(expectCaptionedCards(cut.container)).toContain('activity.section')
    expectAllTilesClear()
  })

  it('CAPTION-PAGE-043 the blocking tree is captioned', () => {
    const { container } = render(
      <BlockingTree
        now={NOW}
        response={{ sampled_at: NOW.toISOString(), stale: false, nodes: [] }}
      />,
    )

    expect(expectCaptionedCards(container)).toContain('locks.blocking-tree')
    expect(visibleCaptions('locks.blocking-tree').length).toBeGreaterThan(0)
  })

  it('CAPTION-PAGE-044 the whole locks page is captioned and its tiles are clear', () => {
    mocks.instance.mockReturnValue(success({ perm_tier: 'T2' }))
    mocks.locks.mockReturnValue(success({ ...withOrigins, sampled_at: new Date().toISOString() }))
    mocks.activity.mockReturnValue(success({ stale: false, metrics: fullMetrics }))
    const { container } = renderWithProviders(<LocksPage instanceId="instance-1" />, {
      route: '/instances/instance-1/locks',
    })

    const ids = expectCaptionedCards(container)
    expect(ids).toContain('locks.blocking-tree')
    expect(ids).toContain('activity.section')
    expectAllTilesClear()
  })
})
