import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { assertMatchesContract } from '@/test/contract'
import { all, base, none } from '@/test/fixtures/getNotificationChannels'
import {
  base as delivered,
  failed,
  timeout as timedOut,
} from '@/test/fixtures/testNotificationChannel'
import { ok, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { NotificationsPanel } from './NotificationsPanel'

async function renderPanel() {
  const view = renderWithProviders(<NotificationsPanel />, { route: '/settings?tab=notifications' })
  await flushNetwork()
  return view
}

function channelRow(name: string) {
  return within(screen.getByRole('list', { name: 'Notification channels' })).getByRole('listitem', {
    name,
  })
}

/** Captures the kinds of testNotificationChannel and answers each with `answer`. */
function captureTests(answer: (kind: string) => Response) {
  const kinds: string[] = []
  server.use(
    http.post('*/api/v1/notification-channels/:kind/test', ({ params }) => {
      kinds.push(String(params.kind))
      return answer(String(params.kind))
    }),
  )
  return kinds
}

describe('NotificationsPanel', () => {
  it('NOTI-UI-001 lists the three channels with their configured badge, host and variable', async () => {
    server.use(ok('getNotificationChannels', base))
    await renderPanel()

    const rows = within(screen.getByRole('list', { name: 'Notification channels' })).getAllByRole(
      'listitem',
    )
    expect(rows.map((row) => within(row).getByRole('heading', { level: 3 }).textContent)).toEqual([
      'Slack',
      'Generic webhook',
      'Google Chat',
    ])

    const chat = channelRow('Google Chat')
    expect(within(chat).getByText('configured')).toBeInTheDocument()
    expect(within(chat).getByText('chat.googleapis.com')).toBeInTheDocument()
    expect(within(chat).getByRole('code')).toHaveTextContent('PGLENS_GOOGLE_CHAT_WEBHOOK_URL')
    expect(chat).toHaveTextContent('Set by PGLENS_GOOGLE_CHAT_WEBHOOK_URL (or _FILE)')
    expect(within(chat).getByRole('button', { name: 'Send test to Google Chat' })).toBeEnabled()

    for (const name of ['Slack', 'Generic webhook']) {
      const row = channelRow(name)
      expect(within(row).getByText('not configured')).toBeInTheDocument()
      expect(within(row).getByText('none')).toBeInTheDocument()
      expect(within(row).getByRole('button', { name: `Send test to ${name}` })).toBeDisabled()
    }
    expect(screen.getByTestId('notification-channels-summary')).toHaveTextContent(
      '1 of 3 channels configured.',
    )
    // The panel never shows a webhook address: only host names.
    expect(screen.getByTestId('notification-channel-google_chat')).not.toHaveTextContent('https://')
    // The section carries its caption (I-16).
    expect(screen.getByRole('region', { name: 'Notifications' })).toHaveAttribute(
      'data-caption-id',
      'settings.notifications',
    )
  })

  it('NOTI-UI-001 a configured channel names its variable, its _FILE form and the older names', async () => {
    server.use(ok('getNotificationChannels', all))
    await renderPanel()

    const slack = channelRow('Slack')
    expect(
      within(slack)
        .getAllByRole('code')
        .map((code) => code.textContent),
    ).toEqual([
      'PGLENS_ALERT_SLACK_WEBHOOK_URL',
      'PGLENS_SLACK_WEBHOOK_URL',
      'PGLENS_SLACK_WEBHOOK_URL_FILE',
    ])
    expect(slack).toHaveTextContent(
      'Set by PGLENS_ALERT_SLACK_WEBHOOK_URL (or _FILE), or the older PGLENS_SLACK_WEBHOOK_URL, PGLENS_SLACK_WEBHOOK_URL_FILE',
    )
    // The generic webhook has no _FILE variable and no older name.
    const webhook = channelRow('Generic webhook')
    expect(within(webhook).getByRole('code')).toHaveTextContent('PGLENS_WEBHOOK_URL')
    expect(webhook).not.toHaveTextContent('_FILE')
    expect(webhook).not.toHaveTextContent('older')
  })

  it('NOTI-UI-002 Send test posts once and shows the result: delivered, failed with the redacted error, timeout, or why it was not sent', async () => {
    for (const result of [delivered, failed, timedOut]) {
      assertMatchesContract('testNotificationChannel', 200, result)
    }
    server.use(ok('getNotificationChannels', all))
    const answers: Record<string, Response> = {
      google_chat: HttpResponse.json(delivered),
      slack: HttpResponse.json(failed),
      webhook: HttpResponse.json(timedOut),
    }
    const kinds = captureTests((kind) => answers[kind] ?? HttpResponse.json(delivered))
    await renderPanel()

    const button = within(channelRow('Google Chat')).getByRole('button', {
      name: 'Send test to Google Chat',
    })
    // Every Send test button states its effect (I-16).
    expect(button).toHaveAccessibleDescription(/pglens test notification/)
    fireEvent.click(button)
    await flushNetwork()
    expect(kinds).toEqual(['google_chat'])
    expect(screen.getByTestId('notification-test-result-google_chat')).toHaveTextContent(
      /^Delivered$/,
    )

    fireEvent.click(within(channelRow('Slack')).getByRole('button', { name: 'Send test to Slack' }))
    await flushNetwork()
    expect(screen.getByTestId('notification-test-result-slack')).toHaveTextContent(
      'Failed: notification to [redacted webhook] returned HTTP 404: Not Found',
    )

    fireEvent.click(
      within(channelRow('Generic webhook')).getByRole('button', {
        name: 'Send test to Generic webhook',
      }),
    )
    await flushNetwork()
    expect(screen.getByTestId('notification-test-result-webhook')).toHaveTextContent(
      'No answer within 10 s: context deadline exceeded',
    )
    expect(kinds).toEqual(['google_chat', 'slack', 'webhook'])

    // A refused request (the server lost the channel since the page loaded).
    server.use(
      status('testNotificationChannel', 409, {
        error: 'channel not configured',
        detail: 'configure the channel in the server environment, then restart the server',
      }),
    )
    fireEvent.click(button)
    await flushNetwork()
    expect(screen.getByTestId('notification-test-result-google_chat')).toHaveTextContent(
      'Not sent: the channel is not configured on the server. Reload the page.',
    )

    // A test of the channel already in flight (another tab or user).
    server.use(
      status('testNotificationChannel', 429, {
        error: 'test already running',
        detail: 'a test message of this channel is being sent; wait for its result',
      }),
    )
    fireEvent.click(button)
    await flushNetwork()
    expect(screen.getByTestId('notification-test-result-google_chat')).toHaveTextContent(
      'Not sent: a test of this channel is already running. Wait for its result.',
    )
  })

  it('NOTI-UI-003 an unconfigured channel shows how to configure it and cannot be tested', async () => {
    server.use(ok('getNotificationChannels', none))
    const kinds = captureTests(() => HttpResponse.json(delivered))
    await renderPanel()

    const chat = channelRow('Google Chat')
    expect(chat).toHaveTextContent(
      'Set PGLENS_GOOGLE_CHAT_WEBHOOK_URL (or _FILE) in the server environment, then restart the server.',
    )
    expect(channelRow('Slack')).toHaveTextContent(
      'Set PGLENS_ALERT_SLACK_WEBHOOK_URL (or _FILE) in the server environment, then restart the server.',
    )
    // The generic webhook has no _FILE variable.
    expect(channelRow('Generic webhook')).toHaveTextContent(
      'Set PGLENS_WEBHOOK_URL in the server environment, then restart the server.',
    )
    const button = within(chat).getByRole('button', { name: 'Send test to Google Chat' })
    expect(button).toBeDisabled()
    expect(button).toHaveAccessibleDescription(/Set PGLENS_GOOGLE_CHAT_WEBHOOK_URL/)
    fireEvent.click(button)
    await flushNetwork()
    expect(kinds).toEqual([])
    expect(screen.getByTestId('notification-channels-summary')).toHaveTextContent(
      'No channel is configured: alerts are stored but not delivered.',
    )
  })

  it('NOTI-UI-004 has no serious accessibility violations, before and after a test send', async () => {
    server.use(ok('getNotificationChannels', base))
    captureTests(() => HttpResponse.json(failed))
    const view = await renderPanel()
    // axe schedules its work on real timers.
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
    fireEvent.click(
      within(channelRow('Google Chat')).getByRole('button', { name: 'Send test to Google Chat' }),
    )
    await waitFor(() =>
      expect(screen.getByTestId('notification-test-result-google_chat')).toHaveTextContent(
        /^Failed: /,
      ),
    )
    await expectNoA11yViolations(view.container)
  }, 15000)
})
