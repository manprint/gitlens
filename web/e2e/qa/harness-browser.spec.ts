import { expect, test } from '@playwright/test'

import { test as live } from '../fixtures'
import { pickInstance } from '../visual_helpers'
import { chartTable, consoleGuard } from './observe'

const FIXTURE = `<table aria-label="Fixture data"><tbody>${[
  'Unknown',
  'Unknown',
  'Unknown',
  '1',
  '2',
  '3',
  '4',
  '5',
  '6',
  '7',
]
  .map((value, index) => `<tr><th>m</th><td>t${index}</td><td>${value}</td></tr>`)
  .join('')}</tbody></table>`

live(
  'QA-HARNESS-010 chartTable reads completeness from the sr-only data table',
  async ({ api, signedInPage: page }) => {
    await page.setContent(FIXTURE)
    const fixture = await chartTable(page, 'Fixture')
    expect({ ...fixture, values: undefined }).toEqual({
      total: 10,
      nonNull: 7,
      ratio: 0.7,
      leadingNull: 3,
      observedRatio: 1,
      values: undefined,
    })

    await page.goto(`/instances/${await pickInstance(api)}`)
    const label = await page
      .getByRole('table')
      .filter({ has: page.locator('caption') })
      .first()
      .getAttribute('aria-label')
    expect(label).toMatch(/ data$/)
    const live = await chartTable(page, (label ?? '').replace(/ data$/, ''))
    expect(live.total).toBeGreaterThan(0)
  },
)

test('QA-HARNESS-011 consoleGuard flags console errors and accepts a whitelisted 404', async ({
  page,
}) => {
  await page.route('http://qa.invalid/**', (route) =>
    route.fulfill({ status: 200, contentType: 'text/html', body: '<p>ok</p>' }),
  )
  const failing = consoleGuard(page)
  await page.goto('http://qa.invalid/page')
  await page.evaluate(() => console.error('qa deliberate error'))
  expect(() => failing.assertClean()).toThrow(/qa deliberate error/)

  const other = await page.context().newPage()
  await other.route('http://qa.invalid/missing', (route) =>
    route.fulfill({ status: 404, contentType: 'text/plain', body: 'missing' }),
  )
  await other.route('http://qa.invalid/page', (route) =>
    route.fulfill({ status: 200, contentType: 'text/html', body: '<p>ok</p>' }),
  )
  const guarded = consoleGuard(other, [{ url: /\/missing$/, status: 404 }])
  await other.goto('http://qa.invalid/page')
  await other.evaluate(() => fetch('http://qa.invalid/missing').then((response) => response.status))
  expect(() => guarded.assertClean()).not.toThrow()
  await other.close()
})
