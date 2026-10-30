import type { Page } from '@playwright/test'

import { expect, test } from './fixtures'
import { assertFits } from './fullhd_fit'
import { fullHdRoute } from './fullhd_routes'
import { stackGoogleChatAddress, stackGoogleChatSecrets, typeSecret } from './notification_secrets'
import { ARTIFACT_DIR, collectConsoleErrors, liveTimeout } from './visual_helpers'

// The real webhook address is typed into the page: no trace, video or
// screenshot of a failure may keep it. Top level, since it changes the worker.
test.use({ trace: 'off', video: 'off', screenshot: 'off' })

test('NOTI-E2E-002 a Google Chat URL stored from the console overrides the environment, delivers, never comes back, and Remove stored URL falls back to the environment', async ({
  api,
  signedInPage: page,
}) => {
  test.setTimeout(liveTimeout(0, 3 * 30_000))
  const browserErrors = collectConsoleErrors(page)
  const address = await stackGoogleChatAddress()
  const secrets = await stackGoogleChatSecrets(address)
  expect(secrets.length, 'secret parts of the stack webhook').toBeGreaterThanOrEqual(3)
  const bodies: Promise<string>[] = []
  page.on('response', (response) => {
    if (new URL(response.url()).pathname.startsWith('/api/')) {
      bodies.push(response.text().catch(() => ''))
    }
  })
  const isEmpty = async (field: ReturnType<Page['getByLabel']>) =>
    field.evaluate((element) => (element as HTMLInputElement).value === '')

  try {
    await page.setViewportSize({ width: 1920, height: 1080 })
    await page.goto('/settings?tab=notifications')
    const row = page
      .getByRole('list', { name: 'Notification channels' })
      .getByRole('listitem', { name: 'Google Chat' })
    await expect(row).toBeVisible({ timeout: 30_000 })
    const source = page.getByTestId('notification-source-google_chat')
    await expect(source).toHaveText('Environment')
    const field = row.getByLabel('Console URL of Google Chat', { exact: true })
    // A masked text field outside any form: no password manager offers to save it.
    await expect(field).toHaveAttribute('type', 'text')
    await expect(field).toHaveAttribute('autocomplete', 'off')
    await expect(field).toHaveAttribute('data-lpignore', 'true')
    expect(
      await field.evaluate((element) => element.closest('form') === null),
      'the URL field is in no form',
    ).toBe(true)
    expect(await isEmpty(field), 'the URL field is never filled from the server').toBe(true)

    // Store the stack's own address from the console.
    await typeSecret(field, address)
    await row.getByRole('button', { name: 'Set URL' }).click()
    await expect(page.getByTestId('notification-change-result-google_chat')).toHaveText(
      'Saved. The next alert and test use this URL.',
      { timeout: 30_000 },
    )
    expect(await isEmpty(field), 'the URL field is cleared once saved').toBe(true)
    await expect(source).toHaveText('Console')
    await expect(row.getByText('chat.googleapis.com', { exact: true })).toBeVisible()
    await expect(row).toContainText(
      'Stored from the console; overrides PGLENS_GOOGLE_CHAT_WEBHOOK_URL',
    )

    const send = row.getByRole('button', { name: 'Send test to Google Chat' })
    await send.click()
    await expect(page.getByTestId('notification-test-result-google_chat')).toHaveText('Delivered', {
      timeout: 30_000,
    })
    await page.screenshot({ path: `${ARTIFACT_DIR}/noti-e2e-002-console.png` })

    // The tab still fits Full HD with the stored URL's controls (I-13).
    await assertFits(page, api, fullHdRoute('/settings?tab=notifications'))

    // Nothing the page or the API holds carries the address.
    await page.setViewportSize({ width: 1920, height: 1080 })
    await page.goto('/settings?tab=notifications')
    await expect(source).toHaveText('Console', { timeout: 30_000 })
    const listing = await api.get('/api/v1/notification-channels')
    expect(listing.ok(), `getNotificationChannels: HTTP ${listing.status()}`).toBe(true)
    const stored = (await listing.json()) as { kind: string; source: string }[]
    expect(stored.find((channel) => channel.kind === 'google_chat')?.source).toBe('ui')
    const scanned = [
      { what: 'page HTML', text: await page.content() },
      { what: 'page text', text: await page.locator('body').innerText() },
      { what: 'getNotificationChannels', text: await listing.text() },
      ...(await Promise.all(bodies)).map((text, index) => ({
        what: `API response ${index}`,
        text,
      })),
    ]
    expect(scanned.length, 'responses captured').toBeGreaterThan(3)
    for (const { what, text } of scanned) {
      for (const [index, secret] of secrets.entries()) {
        expect(text.includes(secret), `${what} holds secret part ${index}`).toBe(false)
      }
    }

    // Remove the stored URL: back to the environment, which still delivers.
    await page
      .getByRole('list', { name: 'Notification channels' })
      .getByRole('listitem', { name: 'Google Chat' })
      .getByRole('button', { name: 'Remove stored URL of Google Chat' })
      .click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toContainText('Remove the stored URL of Google Chat?')
    await dialog.getByRole('button', { name: 'Remove stored URL' }).click()
    await expect(page.getByTestId('notification-change-result-google_chat')).toHaveText(
      'Removed. The channel uses the server environment’s URL again.',
      { timeout: 30_000 },
    )
    await expect(source).toHaveText('Environment')
    await page.getByRole('button', { name: 'Send test to Google Chat' }).click()
    await expect(page.getByTestId('notification-test-result-google_chat')).toHaveText('Delivered', {
      timeout: 30_000,
    })
    const after = (await (await api.get('/api/v1/notification-channels')).json()) as {
      kind: string
      source: string
    }[]
    expect(after.find((channel) => channel.kind === 'google_chat')?.source).toBe('env')
  } finally {
    // Never leave the address stored on the dev stack, even when a step
    // failed. A soft assertion: a failed removal is reported, and never
    // replaces the failure of an earlier step.
    let message: string | null = null
    try {
      const response = await api.delete('/api/v1/notification-channels/google_chat')
      if (!response.ok()) message = `remove the stored URL: HTTP ${response.status()}`
    } catch (error) {
      message = `remove the stored URL: ${error instanceof Error ? error.name : 'error'}`
    }
    if (message !== null) {
      console.error(`NOTI-E2E-002 ${message}`)
      test.info().annotations.push({ type: 'RESTORE-FAILED', description: message })
    }
    expect.soft(message, 'the stored Google Chat URL must be removed').toBeNull()
  }
  browserErrors.assertClean()
})
