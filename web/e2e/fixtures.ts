import { expect, test as base } from '@playwright/test'
import type { APIRequestContext, BrowserContext, Page } from '@playwright/test'

const DEFAULT_UI_BASE_URL = 'http://127.0.0.1:8080'
const FRESH_DATA_TIMEOUT = 30_000

interface TestFixtures {
  api: APIRequestContext
  signedInPage: Page
  waitForFreshData: typeof waitForFreshData
}

interface WorkerFixtures {
  authenticatedStorageState: StorageState
}

type StorageState = Awaited<ReturnType<BrowserContext['storageState']>>

const uiBaseURL = () => process.env.PGLENS_UI_BASE_URL ?? DEFAULT_UI_BASE_URL

/** Poll a domain-specific freshness predicate without introducing arbitrary sleeps. */
export async function waitForFreshData(
  page: Page,
  predicate: () => Promise<boolean> | boolean,
  options: { timeout?: number } = {},
): Promise<void> {
  void page
  await expect.poll(predicate, { timeout: options.timeout ?? FRESH_DATA_TIMEOUT }).toBe(true)
}

export const test = base.extend<TestFixtures, WorkerFixtures>({
  authenticatedStorageState: [
    async ({ browser }, use) => {
      const password = process.env.PGLENS_UI_PASSWORD
      if (!password) {
        throw new Error('PGLENS_UI_PASSWORD is required by the signedInPage fixture')
      }

      const context = await browser.newContext({
        baseURL: uiBaseURL(),
        locale: 'en-US',
        timezoneId: 'UTC',
      })
      try {
        const page = await context.newPage()
        await page.goto('/login')
        await page.getByLabel(/password/i).fill(password)
        await page.getByRole('button', { name: /sign in|log in/i }).click()
        await expect(page).not.toHaveURL(/\/login(?:[/?#]|$)/)
        await use(await context.storageState())
      } finally {
        await context.close()
      }
    },
    { scope: 'worker' },
  ],

  signedInPage: async ({ browser, authenticatedStorageState }, use) => {
    const context = await browser.newContext({
      baseURL: uiBaseURL(),
      storageState: authenticatedStorageState,
      locale: 'en-US',
      timezoneId: 'UTC',
    })
    try {
      await use(await context.newPage())
    } finally {
      await context.close()
    }
  },

  api: async ({ playwright }, use) => {
    const token = process.env.PGLENS_BOOTSTRAP_TOKEN
    if (!token) {
      throw new Error('PGLENS_BOOTSTRAP_TOKEN is required by the api fixture')
    }

    const api = await playwright.request.newContext({
      baseURL: uiBaseURL(),
      extraHTTPHeaders: { Authorization: `Bearer ${token}` },
    })
    try {
      await use(api)
    } finally {
      await api.dispose()
    }
  },

  waitForFreshData: async ({}, use) => {
    await use(waitForFreshData)
  },
})

export { expect }
