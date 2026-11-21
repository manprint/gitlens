import type { APIRequestContext, Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { headerLineProblems, measurePage, open, routeUrl, routeValues } from './fullhd_fit'
import { FULLHD_VIEWPORTS, fullHdRoute } from './fullhd_routes'
import { collectConsoleErrors, liveTimeout } from './visual_helpers'

const LANGUAGE = /^(Language|Lingua)$/
// The last three are the routes plan 007 added (logs, workbook, retention).
const FIT_PATHS = [
  '/',
  '/indexes?tab=indexes',
  '/instances/:instanceId/ash',
  '/alerts/rules',
  '/instances/:instanceId/logs',
  '/instances/:instanceId/queries/:queryid/workbooks/:workbookId',
  '/settings?tab=retention',
]

const NIL_WORKBOOK = '00000000-0000-0000-0000-000000000000'

async function scratchWorkbook(
  api: APIRequestContext,
  values: { instanceId: string; queryid: string },
): Promise<string> {
  const statements = (await (
    await api.get(`/api/v1/statements?instance_id=${values.instanceId}&limit=50`)
  ).json()) as { statements?: { datname: string; queryid: string | number }[] }
  const statement = statements.statements?.find((s) => String(s.queryid) === values.queryid)
  if (!statement) throw new Error(`no statement ${values.queryid} for the workbook route`)
  const created = await api.post('/api/v1/workbooks', {
    data: {
      database: statement.datname,
      instance_id: values.instanceId,
      name: 'I18N-E2E-001',
      queryid: values.queryid,
    },
  })
  expect(created.status(), 'workbook for the fit check').toBe(201)
  return ((await created.json()) as { workbook_id: string }).workbook_id
}

async function chooseLanguage(page: Page, code: string): Promise<void> {
  await page.goto('/settings?tab=preferences')
  const select = page.getByLabel(LANGUAGE, { exact: true })
  await expect(select).toBeVisible({ timeout: 30_000 })
  await select.selectOption(code)
  await expect(page.locator('html')).toHaveAttribute('lang', code)
}

/** Same stored preference as Settings › Preferences, without the round trip. */
async function storeLanguage(page: Page, code: string): Promise<void> {
  await page.evaluate((language) => {
    const raw = localStorage.getItem('pglens.preferences')
    const stored = raw ? (JSON.parse(raw) as Record<string, unknown>) : {}
    localStorage.setItem('pglens.preferences', JSON.stringify({ ...stored, language }))
  }, code)
}

// I18N-E2E-001 (plan 006 F8): the console chosen in Italian from Preferences
// shows Italian navigation, asks the server for Italian texts, keeps the
// choice across a reload, fits Full HD wherever English fits, and goes back to
// English. Preferences live in this browser only: nothing is written on the server.
test('I18N-E2E-001 the console in Italian: navigation, server texts, reload, Full HD fit like English, back to English', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 8 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const languages = new Set<string>()
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/')) {
      languages.add(request.headers()['x-pglens-lang'] ?? '')
    }
  })

  await page.setViewportSize({ width: 1920, height: 1080 })
  await chooseLanguage(page, 'it')
  const nav = page.getByRole('navigation', { name: 'Navigazione principale' })
  for (const label of ['Flotta', 'Segnalazioni', 'Lock e attività']) {
    await expect(nav.getByRole('link', { name: label, exact: true })).toBeVisible()
  }
  await expect(page.getByRole('link', { name: 'Impostazioni', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Impostazioni e inventario')

  languages.clear()
  await page.goto('/findings')
  await expect(page.locator('html')).toHaveAttribute('lang', 'it')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Segnalazioni dell’advisor')
  await expect.poll(() => languages.size, { timeout: 30_000 }).toBeGreaterThan(0)
  expect([...languages], 'every API call asks for Italian').toEqual(['it'])

  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'it')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Segnalazioni dell’advisor')

  // The live data decides whether a page fits at all; the translation must not
  // be what breaks it: Italian fits wherever English fits.
  const values = await routeValues(api)
  // The workbook route needs a workbook: the placeholder id of routeUrl answers
  // 404, which the browser logs as an error. One is made for the check and removed.
  const workbookId = await scratchWorkbook(api, values)
  try {
    for (const path of FIT_PATHS) {
      const url = routeUrl(fullHdRoute(path), values).replace(NIL_WORKBOOK, workbookId)
      for (const viewport of FULLHD_VIEWPORTS) {
        const where = `${path} at ${viewport.width}×${viewport.height}`
        await storeLanguage(page, 'en')
        await open(page, url, viewport)
        await expect(page.locator('html')).toHaveAttribute('lang', 'en')
        const english = await measurePage(page)
        await storeLanguage(page, 'it')
        await open(page, url, viewport)
        await expect(page.locator('html')).toHaveAttribute('lang', 'it')
        const italian = await measurePage(page)
        expect(italian.scrollWidth, `${where}: horizontal scrollbar`).toBeLessThanOrEqual(
          italian.innerWidth,
        )
        expect(italian.scrollingTableBoxes, `${where}: table box scrolls`).toBe(0)
        const headerProblems = await headerLineProblems(page)
        expect(headerProblems, `${where}: header line: ${headerProblems.join(' | ')}`).toEqual([])
        if (english.scrollHeight <= english.innerHeight) {
          expect(
            italian.scrollHeight,
            `${where}: page scrollbar in Italian only`,
          ).toBeLessThanOrEqual(italian.innerHeight)
        }
      }
    }
  } finally {
    await api.delete(`/api/v1/workbooks/${workbookId}`)
  }

  await chooseLanguage(page, 'en')
  await expect(
    page
      .getByRole('navigation', { name: 'Primary navigation' })
      .getByRole('link', { name: 'Fleet', exact: true }),
  ).toBeVisible()
  browserErrors.assertClean()
})
