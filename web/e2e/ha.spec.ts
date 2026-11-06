import { expect, test } from './fixtures'
import { login } from './qa/odoo'
import { queryInt } from './qa/pg'
import { run } from './qa/stack'
import { liveTimeout } from './visual_helpers'

/** HA lab (plan 005 phase 7): Odoo DB behind hactl, three PostgreSQL 18 nodes. */
const ODOO_URL = () => (process.env.ODOO_URL ?? 'http://127.0.0.1:9091').replace(/\/$/, '')
const ODOO_CONTAINER = 'pglens-odoo-odoo-1'
const HACTL_CONTAINER = 'pglens-odoo-hactl-1'
const HACTL_URL = () => process.env.PGLENS_QA_HACTL_URL ?? 'http://127.0.0.1:8008'

interface InstanceRow {
  instance_id: string
  cluster_id: string
  addr: string
  role: string
  up: boolean
}

async function inspect(container: string, format: string): Promise<string> {
  return (await run('docker', ['inspect', '-f', format, container])).stdout.trim()
}

test('LIVE-HA-001 three instances in one cluster, roles primary/standby, Odoo through hactl', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 60_000))

  let up: InstanceRow[] = []
  await expect
    .poll(
      async () => {
        const response = await api.get('/api/v1/instances')
        expect(response.ok()).toBe(true)
        up = ((await response.json()) as InstanceRow[]).filter((row) => row.up)
        return up.length
      },
      { timeout: 60_000, message: 'three instances up' },
    )
    .toBe(3)
  const clusters = [...new Set(up.map((row) => row.cluster_id))]
  expect(clusters, 'the three instances share one cluster').toHaveLength(1)
  expect(up.map((row) => row.role).sort()).toEqual(['primary', 'standby', 'standby'])
  const hactl = await page.request.get(`${HACTL_URL()}/status`)
  expect(hactl.ok()).toBe(true)
  const status = (await hactl.json()) as { leader: string; split_brain: boolean }
  expect(status.split_brain).toBe(false)
  const primary = up.find((row) => row.role === 'primary')
  expect(primary?.addr, 'the pglens primary is the hactl leader').toBe(status.leader)

  await page.goto('/')
  const card = page
    .getByRole('listitem')
    .filter({ has: page.getByRole('link', { name: new RegExp(`\\(${clusters[0]}\\)`) }) })
  await expect(card).toHaveCount(1)
  await expect(card.locator('dt:text-is("Instances") + dd')).toHaveText('3')
  await expect(card.locator('dt:text-is("Standbys") + dd')).toHaveText('2')

  expect(await inspect(ODOO_CONTAINER, '{{range .Config.Env}}{{println .}}{{end}}')).toMatch(
    /^HOST=hactl$/m,
  )
  const hactlIp = await inspect(
    HACTL_CONTAINER,
    '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}',
  )
  expect(hactlIp).toMatch(/^\d+\.\d+\.\d+\.\d+$/)
  const odoo = await page.request.get(`${ODOO_URL()}/web/login`)
  expect(odoo.status()).toBe(200)
  expect((await login()).uid).toBeGreaterThan(0)
  expect(
    await queryInt(
      `SELECT count(*) FROM pg_stat_activity WHERE usename = 'odoo' AND client_addr = '${hactlIp}'::inet;`,
    ),
    'Odoo sessions on the leader come through hactl',
  ).toBeGreaterThan(0)
})
