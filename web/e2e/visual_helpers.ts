import type { APIRequestContext, Locator, Page } from '@playwright/test'

/** Widths every visual screenshot is taken at (the height follows the current viewport). */
export const SHOOT_WIDTHS = [1440, 1024, 390] as const

const ARTIFACT_DIR = '../test/e2e/_artifacts/ui/visual'
const HOST_FRESH_MS = 5 * 60_000

interface InstanceRow {
  instance_id: string
  role?: string | null
  up?: boolean
  last_seen?: string | null
}

interface HostResponse {
  available?: boolean
  host_cpu_used_ratio?: number | null
  sampled_at?: string | null
}

async function listInstances(api: APIRequestContext): Promise<InstanceRow[]> {
  const response = await api.get('/api/v1/instances')
  if (!response.ok()) throw new Error(`GET /api/v1/instances answered ${response.status()}`)
  return (await response.json()) as InstanceRow[]
}

const lastSeen = (row: InstanceRow) => (row.last_seen ? Date.parse(row.last_seen) : 0)

/**
 * The instance the live specs look at: PGLENS_UI_INSTANCE_ID when set, otherwise
 * the `up` primary (the current leader on the HA topology), otherwise the
 * instance seen most recently.
 */
export async function pickInstance(api: APIRequestContext): Promise<string> {
  const fixed = process.env.PGLENS_UI_INSTANCE_ID
  if (fixed) return fixed
  const rows = await listInstances(api)
  if (rows.length === 0) throw new Error('pickInstance: the server lists no instance')
  const up = rows.filter((row) => row.up === true)
  const primary = up.find((row) => row.role === 'primary')
  if (primary) return primary.instance_id
  const [newest] = [...(up.length > 0 ? up : rows)].sort((a, b) => lastSeen(b) - lastSeen(a))
  if (!newest) throw new Error('pickInstance: the server lists no instance')
  return newest.instance_id
}

/** The instance whose host metrics are collected (the `host_local` target). */
export async function pickHostInstance(api: APIRequestContext): Promise<string> {
  const rows = await listInstances(api)
  for (const row of rows) {
    const response = await api.get(`/api/v1/instances/${row.instance_id}/host`)
    if (!response.ok()) continue
    const host = (await response.json()) as HostResponse
    const sampled = host.sampled_at ? Date.parse(host.sampled_at) : 0
    if (
      host.available === true &&
      typeof host.host_cpu_used_ratio === 'number' &&
      Date.now() - sampled < HOST_FRESH_MS
    ) {
      return row.instance_id
    }
  }
  throw new Error('pickHostInstance: no instance reports host metrics in the last 5 minutes')
}

export interface Completeness {
  total: number
  nonNull: number
  ratio: number
  leadingNull: number
  observedRatio: number
}

/** Completeness of a series from its value cells; the text `Unknown` is a null cell. */
export function completenessOf(values: readonly string[]): Completeness {
  const isNull = (value: string) => value.trim() === 'Unknown'
  const total = values.length
  const nonNull = values.filter((value) => !isNull(value)).length
  const first = values.findIndex((value) => !isNull(value))
  const leadingNull = first === -1 ? total : first
  const observed = total - leadingNull
  return {
    total,
    nonNull,
    ratio: total === 0 ? 0 : nonNull / total,
    leadingNull,
    observedRatio: observed === 0 ? 0 : nonNull / observed,
  }
}

/** Reads the sr-only data table `<label> data` of a chart (last cell of each row). */
export async function chartCompleteness(page: Page, label: string): Promise<Completeness> {
  const table = page.getByRole('table', { name: `${label} data`, exact: true })
  const values = await table.locator('tbody tr').evaluateAll((rows) =>
    rows.map((row) => {
      const cells = row.querySelectorAll('td')
      return cells.item(cells.length - 1)?.textContent ?? ''
    }),
  )
  return completenessOf(values)
}

/** True when the canvas inside `locator` has >= 3 colours and > 0.5 % non-background pixels. */
export async function canvasNotBlank(locator: Locator): Promise<boolean> {
  const canvas = locator.locator('canvas').first()
  return canvas.evaluate((element) => {
    const node = element as HTMLCanvasElement
    const context = node.getContext('2d')
    if (!context || node.width === 0 || node.height === 0) return false
    const { data } = context.getImageData(0, 0, node.width, node.height)
    const pixel = (index: number) =>
      ((data[index] ?? 0) << 24) |
      ((data[index + 1] ?? 0) << 16) |
      ((data[index + 2] ?? 0) << 8) |
      (data[index + 3] ?? 0)
    const colours = new Set<number>()
    const background = pixel(0)
    let painted = 0
    for (let index = 0; index < data.length; index += 4) {
      const colour = pixel(index)
      if (colours.size < 3) colours.add(colour)
      if (colour !== background) painted += 1
    }
    return colours.size >= 3 && painted / (data.length / 4) > 0.005
  })
}

/** Collects console.error messages and page errors; call `assertClean()` at the end. */
export function collectConsoleErrors(page: Page): { errors: string[]; assertClean: () => void } {
  const errors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(`console.error: ${message.text()}`)
  })
  page.on('pageerror', (error) => errors.push(`pageerror: ${error.message}`))
  return {
    errors,
    assertClean: () => {
      if (errors.length > 0) throw new Error(`browser errors:\n${errors.join('\n')}`)
    },
  }
}

/** Full-page screenshots at every width of SHOOT_WIDTHS; the viewport is restored. */
export async function shoot(page: Page, name: string): Promise<string[]> {
  const original = page.viewportSize()
  const height = original?.height ?? 1080
  const files: string[] = []
  try {
    for (const width of SHOOT_WIDTHS) {
      await page.setViewportSize({ width, height })
      const file = `${ARTIFACT_DIR}/${name}-${width}.png`
      await page.screenshot({ fullPage: true, path: file })
      files.push(file)
    }
  } finally {
    if (original) await page.setViewportSize(original)
  }
  return files
}

/**
 * Timeout for a live test that waits for a stimulus and then for the data:
 * both durations plus one minute of slack. Pass it to `test.setTimeout`.
 */
export function liveTimeout(stimulusMs = 0, waitMs = 0): number {
  return stimulusMs + waitMs + 60_000
}
