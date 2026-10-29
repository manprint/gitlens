/** pglens API ground truth, authenticated with the bootstrap token. */
const baseUrl = () => process.env.PGLENS_UI_BASE_URL ?? 'http://127.0.0.1:9090'

function token(): string {
  const value = process.env.PGLENS_BOOTSTRAP_TOKEN
  if (!value) throw new Error('PGLENS_BOOTSTRAP_TOKEN is required by qa/api')
  return value
}

export async function get<T = unknown>(path: string): Promise<T> {
  const response = await fetch(`${baseUrl()}${path}`, {
    headers: { Authorization: `Bearer ${token()}` },
    signal: AbortSignal.timeout(30_000),
  })
  if (!response.ok) throw new Error(`GET ${path} answered ${response.status}`)
  return (await response.json()) as T
}

export interface SeriesPoint {
  ts: string
  value: number | null
}

export async function series(
  metric: string,
  instanceId: string,
  from: Date,
  to: Date,
  step = '60s',
): Promise<SeriesPoint[]> {
  const query = new URLSearchParams({
    metric,
    instance_id: instanceId,
    from: from.toISOString(),
    to: to.toISOString(),
    step,
  })
  const body = await get<{ series?: SeriesPoint[] }>(`/api/v1/metrics/query?${query.toString()}`)
  return body.series ?? []
}

export const instances = () =>
  get<{ instance_id: string; role?: string | null; up?: boolean; last_seen?: string | null }[]>(
    '/api/v1/instances',
  )
export const activity = (instanceId: string) => get(`/api/v1/instances/${instanceId}/activity`)
export const findings = () => get('/api/v1/findings')
