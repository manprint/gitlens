import type { APIRequestContext, Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { liveTimeout } from './visual_helpers'

// Console steps of the plan 007 reference scenario (test/parity/README.md).
// It runs against a server fed by `test/parity/scenario.sh workload`: an
// instance with a `parity` database, tagged statements from the `checkout`
// and `billing` services, a deadlock, auto_explain samples and autovacuum
// runs on `churn`.

interface Instance {
  instance_id: string
}

interface Statement {
  query_text?: string
  queryid: string
}

async function parityTarget(
  api: APIRequestContext,
): Promise<{ instanceId: string; queryid: string }> {
  const instances = (await (await api.get('/api/v1/instances')).json()) as Instance[]
  const instanceId = instances[0]?.instance_id
  if (!instanceId) throw new Error('no instance: run test/parity/scenario.sh up and workload first')
  const from = new Date(Date.now() - 60 * 60_000).toISOString()
  const statements = (await (
    await api.get(
      `/api/v1/statements?instance_id=${instanceId}&database=parity&from=${from}&limit=50`,
    )
  ).json()) as { statements: Statement[] }
  const queryid = statements.statements.find((s) => s.query_text?.includes('customer = '))?.queryid
  if (!queryid) throw new Error('no customer statement: run test/parity/scenario.sh workload first')
  return { instanceId, queryid }
}

async function card(page: Page, url: string, captionId: string) {
  await page.goto(url)
  const section = page.locator(`[data-caption-id="${captionId}"]`).first()
  await expect(section, `${captionId} on ${url}`).toBeVisible({ timeout: 30_000 })
  return section
}

test('PARITY-E2E-001 the console shows every step of the reference scenario', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 8 * 45_000))
  const { instanceId, queryid } = await parityTarget(api)
  const base = `/instances/${instanceId}`

  await test.step('Activity grouped by service', async () => {
    const section = await card(page, `${base}/ash?group=service`, 'ash.breakdown-dimension')
    await expect(section).toContainText('checkout')
    await expect(section).toContainText('billing')
  })

  await test.step('Logs with the deadlock class', async () => {
    const section = await card(page, `${base}/logs`, 'logs.events')
    await expect(section).toContainText('lock.deadlock')
  })

  await test.step('Plan samples of the tagged statement', async () => {
    await card(page, `${base}/queries/${queryid}?db=parity`, 'queries.plan-captures')
  })

  await test.step('Autovacuum activity of churn', async () => {
    const section = await card(
      page,
      `${base}?section=relations&view=autovacuum&db=parity`,
      'relations.autovacuum',
    )
    await expect(section).toContainText('churn')
  })

  await test.step('Processes panel', async () => {
    await card(page, `${base}?section=host`, 'host.processes')
  })

  await test.step('Missing indexes', async () => {
    await card(page, '/indexes?tab=missing', 'indexes.missing')
  })

  await test.step('Workbook', async () => {
    const created = await api.post('/api/v1/workbooks', {
      data: { database: 'parity', instance_id: instanceId, name: 'parity e2e', queryid },
    })
    expect(created.status()).toBe(201)
    const { workbook_id: workbookId } = (await created.json()) as { workbook_id: string }
    try {
      await card(
        page,
        `${base}/queries/${queryid}/workbooks/${workbookId}`,
        'queries.workbook-variants',
      )
    } finally {
      await api.delete(`/api/v1/workbooks/${workbookId}`)
    }
  })

  await test.step('Settings: notification channels and retention groups', async () => {
    await page.goto('/settings?tab=notifications')
    for (const kind of ['teams', 'pagerduty', 'email']) {
      await expect(page.getByTestId(`notification-channel-${kind}`)).toBeVisible({
        timeout: 30_000,
      })
    }
    await card(page, '/settings?tab=retention', 'settings.retention-rollup-table')
  })
})
