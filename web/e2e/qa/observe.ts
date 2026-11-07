import type { Page } from '@playwright/test'

import { completenessOf, type Completeness } from '../visual_helpers'

/** The sr-only data table of a chart (`<title> data`): values and completeness. */
export async function chartTable(
  page: Page,
  title: string,
): Promise<Completeness & { values: string[] }> {
  const values = await page
    .getByRole('table', { name: `${title} data`, exact: true })
    .locator('tbody tr')
    .evaluateAll((rows) =>
      rows.map((row) => {
        const cells = row.querySelectorAll('td')
        return cells.item(cells.length - 1)?.textContent ?? ''
      }),
    )
  return { ...completenessOf(values), values }
}

/** The text of every body row (cells joined by ` | `) of the table named `name`. */
export function tableRows(page: Page, name: string): Promise<string[]> {
  return page
    .getByRole('table', { name, exact: true })
    .locator('tbody tr')
    .evaluateAll((rows) =>
      rows.map((row) =>
        Array.from(row.querySelectorAll('th, td'))
          .map((cell) => (cell.textContent ?? '').trim())
          .join(' | '),
      ),
    )
}

/** The value text of a summary tile whose label is `label`. */
export async function tile(page: Page, label: string): Promise<string> {
  const term = page.getByText(label, { exact: true }).first()
  const card = term.locator('xpath=ancestor::*[self::div or self::section or self::li][1]')
  const text = (await card.textContent()) ?? ''
  return text.replace(label, '').trim()
}

/** The pager status text (e.g. `1–20 of 57`) of the pager labelled `name`. */
export async function pager(page: Page, name: string): Promise<string> {
  return ((await page.getByRole('navigation', { name }).textContent()) ?? '').trim()
}

export interface AllowedFailure {
  url: RegExp
  status: number
  /** Also accept `net::ERR_ABORTED` of this URL (a request cut short by a navigation). */
  aborted?: boolean
}

/**
 * Collects console.error, page errors, failed requests and 4xx/5xx responses.
 * `assertClean()` throws unless every response failure is whitelisted.
 */
export function consoleGuard(page: Page, allowed: AllowedFailure[] = []) {
  const problems: string[] = []
  const isAllowed = (url: string, status: number) =>
    allowed.some((rule) => rule.status === status && rule.url.test(url))
  page.on('console', (message) => {
    if (message.type() !== 'error') return
    // A whitelisted failed resource is also reported by Chromium as a console error.
    const location = message.location().url
    if (
      message.text().includes('Failed to load resource') &&
      allowed.some(
        (rule) => rule.url.test(location) && message.text().includes(`status of ${rule.status}`),
      )
    ) {
      return
    }
    problems.push(`console.error: ${message.text()}`)
  })
  page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`))
  page.on('requestfailed', (request) => {
    const error = request.failure()?.errorText ?? ''
    // A whitelisted request cut short by the next navigation (the sign-in after a restart).
    if (
      error.includes('ERR_ABORTED') &&
      allowed.some((rule) => rule.aborted === true && rule.url.test(request.url()))
    ) {
      return
    }
    problems.push(`requestfailed: ${request.url()} ${error}`)
  })
  page.on('response', (response) => {
    if (response.status() >= 400 && !isAllowed(response.url(), response.status())) {
      problems.push(`HTTP ${response.status()}: ${response.url()}`)
    }
  })
  return {
    problems,
    assertClean: () => {
      if (problems.length > 0) throw new Error(`browser problems:\n${problems.join('\n')}`)
    },
  }
}
