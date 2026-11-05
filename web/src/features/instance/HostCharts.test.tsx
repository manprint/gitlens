import { act, screen, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { EChartsOption } from 'echarts'

import { expectNoA11yViolations } from '@/test/a11y'
import { INSTANCE_ID } from '@/test/fixture-helpers'
import { never } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { renderWithProviders } from '@/test/render'

import { FILESYSTEM_HINT, HostCharts, NO_SWAP_TEXT } from './HostCharts'

const chartMock = vi.hoisted(() => ({ options: [] as EChartsOption[] }))

vi.mock('echarts-for-react/esm/core', () => ({
  default: (props: { option: EChartsOption }) => {
    chartMock.options.push(props.option)
    return null
  },
}))

const metricsUrl = 'http://localhost/api/v1/metrics/query'
const at = (minute: number) => `2026-10-29T10:0${minute}:00Z`
const points = (...values: (number | null)[]) =>
  values.map((value, index) => ({ ts: at(index), value }))

const GIB = 1024 ** 3
const VALUES: Record<string, (number | null)[]> = {
  host_cpu_used_ratio: [0.2, 0.5, null],
  host_cpu_count: [8, 8, 8],
  host_load1: [1, 2, 3],
  host_load5: [1, 1, 2],
  host_load15: [1, 1, 1],
  host_mem_total_bytes: [16 * GIB, 16 * GIB, 16 * GIB],
  host_mem_available_bytes: [10 * GIB, 6 * GIB, null],
  host_swap_total_bytes: [2 * GIB, 2 * GIB, 2 * GIB],
  host_swap_used_bytes: [0, GIB, GIB],
  host_disk_total_bytes: [100 * GIB, 100 * GIB, 100 * GIB],
  host_disk_free_bytes: [40 * GIB, 30 * GIB, 20 * GIB],
  host_disk_free_ratio: [0.4, 0.3, 0.2],
}

function metricsHandler(requests: URL[] = []) {
  return http.get(metricsUrl, ({ request }) => {
    const url = new URL(request.url)
    requests.push(url)
    const values = VALUES[url.searchParams.get('metric') ?? ''] ?? []
    return HttpResponse.json({ series: points(...values) })
  })
}

async function settle() {
  await act(async () => {
    for (let attempt = 0; attempt < 10; attempt += 1) {
      await vi.advanceTimersByTimeAsync(0)
      await Promise.resolve()
    }
  })
}

interface LineSeries {
  name: string
  data: [string, number | null][]
  yAxisIndex?: number
  markLine?: { data: { name: string; yAxis: number }[] }
}

function optionWith(first: string, predicate: (series: LineSeries[]) => boolean = () => true) {
  const option = chartMock.options.findLast((candidate) => {
    const series = candidate.series as LineSeries[] | undefined
    return series?.[0]?.name === first && predicate(series)
  })
  expect(option).toBeDefined()
  return option as Omit<EChartsOption, 'series'> & { series: LineSeries[] }
}

const values = (line: LineSeries | undefined) => (line?.data ?? []).map(([, value]) => value)

async function renderCharts({ diskMeasured = true, swapAbsent = false } = {}) {
  const requests: URL[] = []
  server.use(metricsHandler(requests))
  const view = renderWithProviders(
    <HostCharts diskMeasured={diskMeasured} instanceId={INSTANCE_ID} swapAbsent={swapAbsent} />,
    { route: `/instances/${INSTANCE_ID}?range=1h` },
  )
  await settle()
  return { requests, view }
}

describe('HostCharts', () => {
  beforeEach(() => {
    chartMock.options = []
  })

  it('renders the five cards inside the host chart group', async () => {
    await renderCharts()

    const grid = screen.getByTestId('host-grid')
    for (const title of ['CPU used (%)', 'Load average', 'Memory', 'Swap', 'Filesystem']) {
      expect(
        within(grid).getByRole('heading', { name: (name) => name.startsWith(title) }),
      ).toBeVisible()
    }
    expect(screen.getByRole('region', { name: 'Host charts' })).toHaveAttribute('id', 'host')
  })

  it('HOSTCH-004 plots the CPU ratio as percent on a 0–100 axis', async () => {
    await renderCharts()

    const option = optionWith('CPU used')
    expect(values(option.series[0])).toEqual([20, 50, null])
    expect(option.yAxis).toMatchObject({ min: 0, max: 100 })
  })

  it('HOSTCH-005 draws a CPU count reference line on the load chart', async () => {
    await renderCharts()

    const option = optionWith('Load, 1 min', (series) => series.length === 4)
    expect(option.series.map(({ name }) => name)).toEqual([
      'Load, 1 min',
      'Load, 5 min',
      'Load, 15 min',
      'CPU count',
    ])
    // The line sits on its own empty series, which the legend does not list.
    expect(option.series[3]?.data).toEqual([])
    expect(option.series[3]?.markLine?.data).toEqual([{ name: 'CPU count', yAxis: 8 }])
    expect((option.legend as { data: string[] }).data).toEqual([
      'Load, 1 min',
      'Load, 5 min',
      'Load, 15 min',
    ])
    // The axis top is a round tick that always leaves room for the reference line.
    const max = (option.yAxis as { max: (extent: { max: number }) => number }).max
    expect(max({ max: 3 })).toBe(10)
    expect(max({ max: 20 })).toBe(25)
  })

  it('HOSTCH-005 keeps the load chart when only the CPU count query fails', async () => {
    server.use(
      http.get(metricsUrl, ({ request }) => {
        const metric = new URL(request.url).searchParams.get('metric') ?? ''
        if (metric === 'host_cpu_count') return HttpResponse.json({ error: 'x' }, { status: 500 })
        return HttpResponse.json({ series: points(...(VALUES[metric] ?? [])) })
      }),
    )
    renderWithProviders(<HostCharts diskMeasured instanceId={INSTANCE_ID} swapAbsent={false} />, {
      route: `/instances/${INSTANCE_ID}?range=1h`,
    })
    await settle()

    const load = screen.getByRole('region', { name: /^Load average/ })
    expect(within(load).queryByRole('button', { name: /retry/i })).not.toBeInTheDocument()
    const option = optionWith('Load, 1 min')
    expect(option.series.map(({ name }) => name)).toEqual([
      'Load, 1 min',
      'Load, 5 min',
      'Load, 15 min',
    ])
  })

  it('shows every card loading until its queries answer', async () => {
    server.use(never('queryMetrics'))
    renderWithProviders(<HostCharts diskMeasured instanceId={INSTANCE_ID} swapAbsent={false} />, {
      route: `/instances/${INSTANCE_ID}?range=1h`,
    })
    await settle()

    for (const title of ['CPU used (%)', 'Load average', 'Memory', 'Swap', 'Filesystem']) {
      expect(screen.getByRole('status', { name: `Loading ${title}` })).toBeInTheDocument()
    }
    expect(chartMock.options).toEqual([])
  })

  it('HOSTCH-006 plots used memory as total minus available, with the total line', async () => {
    await renderCharts()

    const option = optionWith('Used', (series) => values(series[1])[0] === 16 * GIB)
    expect(values(option.series[0])).toEqual([6 * GIB, 10 * GIB, null])
    expect(option.series[1]?.name).toBe('Total')
  })

  it('HOSTCH-006 pairs memory buckets that two queries placed a few milliseconds apart', async () => {
    server.use(
      http.get(metricsUrl, ({ request }) => {
        const metric = new URL(request.url).searchParams.get('metric') ?? ''
        const offset = metric === 'host_mem_available_bytes' ? 7 : 0
        const series = (VALUES[metric] ?? []).map((value, index) => ({
          ts: new Date(Date.parse(at(index)) + offset).toISOString(),
          value,
        }))
        return HttpResponse.json({ series })
      }),
    )
    renderWithProviders(<HostCharts diskMeasured instanceId={INSTANCE_ID} swapAbsent={false} />, {
      route: `/instances/${INSTANCE_ID}?range=1h`,
    })
    await settle()

    const option = optionWith('Used', (series) => values(series[1])[0] === 16 * GIB)
    expect(values(option.series[0])).toEqual([6 * GIB, 10 * GIB, null])
  })

  it('HOSTCH-007 replaces the swap chart with an explanation when the host has no swap', async () => {
    await renderCharts({ swapAbsent: true })

    const swap = screen.getByRole('region', { name: /^Swap/ })
    expect(within(swap).getByRole('note')).toHaveTextContent(NO_SWAP_TEXT)
    expect(
      chartMock.options.some((option) =>
        (option.series as LineSeries[]).some((line) => values(line)[0] === 2 * GIB),
      ),
    ).toBe(false)
  })

  it('HOSTCH-007 charts swap used against its total when swap exists', async () => {
    await renderCharts()

    const option = optionWith('Used', (series) => values(series[1])[0] === 2 * GIB)
    expect(values(option.series[0])).toEqual([0, GIB, GIB])
    expect(screen.queryByText(NO_SWAP_TEXT)).not.toBeInTheDocument()
  })

  it('HOSTCH-008 shows the mount hint when the filesystem is not measured', async () => {
    await renderCharts({ diskMeasured: false })

    const filesystem = screen.getByRole('region', { name: /^Filesystem/ })
    expect(within(filesystem).getByRole('note')).toHaveTextContent(FILESYSTEM_HINT)
  })

  it('HOSTCH-008 charts used bytes and the free share on a second 0–100 axis', async () => {
    await renderCharts()

    const option = optionWith('Used', (series) => series[1]?.name === 'Free (%)')
    expect(values(option.series[0])).toEqual([60 * GIB, 70 * GIB, 80 * GIB])
    expect(values(option.series[1])).toEqual([40, 30, 20])
    expect(option.series[1]?.yAxisIndex).toBe(1)
    expect((option.yAxis as object[])[1]).toMatchObject({ min: 0, max: 100 })
    const formatter = (option.tooltip as { formatter: (params: unknown) => string }).formatter
    const html = formatter([
      { seriesIndex: 0, seriesName: 'Used', value: [at(0), 60 * GIB], axisValue: at(0) },
      { seriesIndex: 1, seriesName: 'Free (%)', value: [at(0), 40], axisValue: at(0) },
    ])
    expect(html).toContain('2026-10-29 10:00:00 UTC')
    expect(html).toContain('60.0 GiB')
    expect(html).toContain('40.0%')
  })

  it('HOSTCH-010 requests every host metric at a step of at least 30 s', async () => {
    const { requests } = await renderCharts()

    expect(new Set(requests.map((url) => url.searchParams.get('metric')))).toEqual(
      new Set(Object.keys(VALUES)),
    )
    for (const url of requests) {
      expect(url.searchParams.get('step')).toBe('30s')
      expect(url.searchParams.get('instance_id')).toBe(INSTANCE_ID)
    }
  })

  it('shows an error state on the card whose query failed', async () => {
    server.use(
      http.get(metricsUrl, ({ request }) =>
        new URL(request.url).searchParams.get('metric') === 'host_load5'
          ? HttpResponse.json({ error: 'boom' }, { status: 500 })
          : HttpResponse.json({ series: [] }),
      ),
    )
    renderWithProviders(<HostCharts diskMeasured instanceId={INSTANCE_ID} swapAbsent={false} />, {
      route: `/instances/${INSTANCE_ID}?range=1h`,
    })
    await settle()

    const load = screen.getByRole('region', { name: /^Load average/ })
    expect(within(load).getByRole('button', { name: /retry/i })).toBeInTheDocument()
    const cpu = screen.getByRole('region', { name: /^CPU used/ })
    expect(within(cpu).getByText('No data in this range')).toBeInTheDocument()
  })

  it('HOSTCH-011 has no serious accessibility violations', async () => {
    const { view } = await renderCharts({ diskMeasured: false, swapAbsent: true })
    expect(screen.getByText(FILESYSTEM_HINT)).toBeInTheDocument()

    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
  })
})
