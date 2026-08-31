import { expect } from '@playwright/test'

import { test } from './fixtures'

test('SYS-UI-000: the server serves the application shell', async ({ page }) => {
  const response = await page.goto('/')

  expect(response?.status()).toBe(200)
  await expect(page).toHaveTitle('pglens')
})
