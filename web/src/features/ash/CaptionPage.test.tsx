import { screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { captionOf, type CaptionId } from '@/lib/captions'
import { expectCaptionedCards } from '@/test/clearCard'
import { renderWithProviders } from '@/test/render'

const mocks = vi.hoisted(() => ({
  ash: vi.fn(),
  ashTop: vi.fn(),
  settings: vi.fn(),
}))

vi.mock('@/api/queries', () => ({
  useAsh: mocks.ash,
  useAshTop: mocks.ashTop,
  useInstanceSettings: mocks.settings,
}))

vi.mock('echarts-for-react/esm/core', () => ({
  default: () => <div data-testid="mock-echarts" />,
}))

import { AshPage } from './AshPage'

const buckets = [
  {
    avg_active_sessions: 1.5,
    samples: 15,
    ticks: 10,
    ts: '2026-09-01T10:00:00Z',
    wait_event: 'relation',
    wait_event_type: 'Lock',
    queryid: '9007199254740993',
  },
  {
    avg_active_sessions: 1,
    samples: 10,
    ticks: 10,
    ts: '2026-09-01T10:00:00Z',
    wait_event: 'DataFileRead',
    wait_event_type: 'IO',
    queryid: '9007199254740993',
  },
]

const ashResponse = { buckets, enabled: true, resolution_seconds: 30, statistical: false }
const ashTopResponse = {
  enabled: true,
  entries: [
    {
      avg_active_sessions: 2.5,
      query_text: 'SELECT * FROM users WHERE id = $1',
      queryid: '9007199254740993',
      samples: 25,
      ticks: 10,
    },
  ],
  resolution_seconds: 30,
  statistical: false,
}

function success<T>(data: T) {
  return {
    data,
    dataAge: 0,
    dataUpdatedAt: Date.now(),
    error: null,
    isPending: false,
    refetch: vi.fn(),
  }
}

function renderAsh(route: string) {
  mocks.ash.mockReturnValue(success(ashResponse))
  mocks.ashTop.mockReturnValue(success(ashTopResponse))
  mocks.settings.mockReturnValue(success({ instance_id: 'instance-1', settings: [] }))
  return renderWithProviders(<AshPage instanceId="instance-1" />, { route })
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

beforeEach(() => {
  mocks.ash.mockReset()
  mocks.ashTop.mockReset()
  mocks.settings.mockReset()
})

describe('ASH page captions', () => {
  it('CAPTION-PAGE-034 the page states what ASH is and has its hint', () => {
    renderAsh('/instances/instance-1/ash?range=1h')

    expect(visibleCaptions('ash.page')).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'About ASH and wait analysis' })).toBeInTheDocument()
  })

  it('CAPTION-PAGE-030 the stacked wait types view is captioned', () => {
    const { container } = renderAsh('/instances/instance-1/ash?range=1h&view=stacked')

    const ids = expectCaptionedCards(container)
    expect(ids).toContain('ash.breakdown-types')
    expect(visibleCaptions('ash.breakdown-types').length).toBeGreaterThan(0)
  })

  it('CAPTION-PAGE-031 the default view captions every wait group chart', () => {
    const { container } = renderAsh('/instances/instance-1/ash?range=1h')

    const ids = expectCaptionedCards(container)
    expect(ids).toContain('ash.breakdown-types')
    expect(ids.some((id) => id.startsWith('ash.chart-'))).toBe(true)
  })

  it('CAPTION-PAGE-032 the wait events view is captioned', () => {
    const { container } = renderAsh(
      '/instances/instance-1/ash?group=wait_event&wait_event_type=Lock&view=stacked',
    )

    expect(expectCaptionedCards(container)).toContain('ash.breakdown-events')
    expect(visibleCaptions('ash.breakdown-events').length).toBeGreaterThan(0)
  })

  it('CAPTION-PAGE-033 the queries view and the top queries list are captioned', () => {
    const { container } = renderAsh(
      '/instances/instance-1/ash?group=queryid&wait_event=relation&view=stacked',
    )

    const ids = expectCaptionedCards(container)
    expect(ids).toContain('ash.breakdown-queries')
    expect(ids).toContain('ash.top-queries')
    expect(visibleCaptions('ash.top-queries').length).toBeGreaterThan(0)
  })
})
