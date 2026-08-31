import { act, fireEvent, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useTimeRange } from '@/hooks/useTimeRange'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { renderWithProviders } from '@/test/render'
import { server } from '@/test/msw/server'

import { OverviewSection } from './OverviewSection'

const chartMock = vi.hoisted(() => ({ options: [] as unknown[] }))

vi.mock('echarts-for-react', () => ({
  default: (props: { option: unknown }) => {
    chartMock.options.push(props.option)
    return null
  },
}))

const metricsUrl = 'http://localhost/api/v1/metrics/query'
const resetSeries = {
  series: [
    { ts: '2026-01-01T00:00:00Z', value: 10 },
    { ts: '2026-01-01T00:01:00Z', value: null },
    { ts: '2026-01-01T00:02:00Z', value: 12 },
  ],
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

function metricsHandler(body: typeof resetSeries | { series: [] }, steps: string[] = []) {
  return http.get(metricsUrl, ({ request }) => {
    const url = new URL(request.url)
    steps.push(url.searchParams.get('step') ?? '')
    return HttpResponse.json(body)
  })
}

function RangeProbe() {
  const { setPreset } = useTimeRange()
  return <button onClick={() => setPreset('7d')}>Use seven days</button>
}

describe('OverviewSection', () => {
  beforeEach(() => {
    chartMock.options = []
  })

  it('UI-INST-024 renders a reset annotation while preserving the chart gap', async () => {
    server.use(metricsHandler(resetSeries))

    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
    })
    await settle()

    const option = chartMock.options.find((candidate) => {
      const series = (candidate as { series?: { markPoint?: unknown }[] }).series
      return series?.some((item) => item.markPoint !== undefined)
    }) as {
      series: {
        connectNulls: boolean
        data: [string, number | null][]
        markPoint?: { data: { name: string }[] }
      }[]
    }
    const series = option.series.find((item) => item.markPoint !== undefined)

    expect(series?.connectNulls).toBe(false)
    expect(series?.data[1]?.[1]).toBeNull()
    expect(series?.markPoint?.data).toEqual([expect.objectContaining({ name: 'Counter reset' })])
  })

  it('UI-INST-025 renders every tile as not measured when its value is null', async () => {
    server.use(metricsHandler({ series: [] }))

    renderWithProviders(<OverviewSection database="app" instanceId={INSTANCE_ID} />, {
      route: `/instances/${INSTANCE_ID}?db=app&range=1h`,
    })
    await settle()

    for (const label of [
      'Connections in use',
      'Transactions per second',
      'Cache hit ratio',
      'WAL generated in range',
      'Checkpoint frequency',
      'Temporary bytes',
      'Deadlocks',
    ]) {
      expect(
        within(screen.getByRole('article', { name: label })).getAllByLabelText('not measured'),
      ).not.toHaveLength(0)
    }
  })

  it('UI-INST-026 refetches all metric queries with a new step after a range change', async () => {
    const steps: string[] = []
    server.use(metricsHandler({ series: [] }, steps))

    renderWithProviders(
      <>
        <OverviewSection database="app" instanceId={INSTANCE_ID} />
        <RangeProbe />
      </>,
      { route: `/instances/${INSTANCE_ID}?db=app&range=15m` },
    )
    await settle()
    const initialCalls = steps.length

    fireEvent.click(screen.getByRole('button', { name: 'Use seven days' }))
    await settle()

    expect(initialCalls).toBeGreaterThan(0)
    expect(steps).toContain('15s')
    expect(steps).toContain('15m')
    expect(steps.length).toBeGreaterThan(initialCalls)
  })
})
