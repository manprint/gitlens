import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw/http'
import { describe, expect, it, vi } from 'vitest'

import { expectNoA11yViolations } from '@/test/a11y'
import { assertMatchesContract } from '@/test/contract'
import { webhookUrlProblem } from '@/lib/notificationChannels'
import {
  base as removedToEnv,
  none as removedToNone,
} from '@/test/fixtures/deleteNotificationChannel'
import { all, base, none, stored } from '@/test/fixtures/getNotificationChannels'
import { base as setRow } from '@/test/fixtures/setNotificationChannel'
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

type Channel = (typeof base)[number]

/**
 * Serves getNotificationChannels from `rows`, and setNotificationChannel and
 * deleteNotificationChannel by changing them; captures each write.
 */
function serveChannels(initial: Channel[]) {
  let rows = initial.map((row) => ({ ...row }))
  const puts: { kind: string; body: unknown }[] = []
  const deletes: string[] = []
  server.use(
    http.get('*/api/v1/notification-channels', () => HttpResponse.json(rows)),
    http.put('*/api/v1/notification-channels/:kind', async ({ params, request }) => {
      const kind = String(params.kind)
      puts.push({ kind, body: await request.json() })
      const row: Channel = { ...setRow, kind: kind as Channel['kind'] }
      rows = rows.map((current) => (current.kind === kind ? row : current))
      return HttpResponse.json(row)
    }),
    http.delete('*/api/v1/notification-channels/:kind', ({ params }) => {
      const kind = String(params.kind)
      deletes.push(kind)
      const row: Channel =
        kind === 'google_chat' ? removedToEnv : { ...removedToNone, kind: kind as Channel['kind'] }
      rows = rows.map((current) => (current.kind === kind ? row : current))
      return HttpResponse.json(row)
    }),
  )
  return { puts, deletes }
}

/** A canary address: the page must never show it once it is sent. */
const CANARY_URL =
  'https://chat.googleapis.com/v1/spaces/UICANARYSPACE/messages?key=uiCanaryKey0001&token=uiCanaryToken0002'

function urlField(name: string) {
  return within(channelRow(name)).getByLabelText(`Console URL of ${name}`)
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
  it('NOTI-UI-005 the URL form is write-only: a masked field never filled, that posts the URL once and clears', async () => {
    for (const row of [setRow, removedToEnv, removedToNone]) {
      assertMatchesContract('setNotificationChannel', 200, row)
    }
    const { puts } = serveChannels(base)
    const view = await renderPanel()

    for (const name of ['Slack', 'Generic webhook', 'Google Chat']) {
      const field = urlField(name)
      // A masked text field outside any form: no password manager offers to
      // save it.
      expect(field).toHaveAttribute('type', 'text')
      expect(field).toHaveClass('[-webkit-text-security:disc]')
      expect(field).toHaveAttribute('autocomplete', 'off')
      expect(field).toHaveAttribute('data-1p-ignore')
      expect(field).toHaveAttribute('data-lpignore', 'true')
      expect(
        within(channelRow(name)).getByRole('group', { name: `URL form of ${name}` }),
      ).toContainElement(field)
      expect(field).toHaveValue('')
      // The field states what storing a URL does (I-16).
      expect(field).toHaveAccessibleDescription(/write-only/)
    }
    const chat = channelRow('Google Chat')
    expect(within(chat).getByTestId('notification-source-google_chat')).toHaveTextContent(
      'Environment',
    )
    expect(within(chat).queryByRole('button', { name: /Remove stored URL/ })).toBeNull()

    // No form and no password field anywhere in the tab.
    expect(view.container.innerHTML).not.toMatch(/<form[\s>]|type="password"/)
    /** Whether the mutation cache still holds a part of the canary address. */
    const cacheHoldsCanary = () =>
      view.queryClient
        .getMutationCache()
        .getAll()
        .some((mutation) => JSON.stringify(mutation.state).includes('uiCanaryKey0001'))

    // A refused save leaves nothing of the address in the mutation cache either.
    server.use(status('setNotificationChannel', 503, { error: 'store unavailable', detail: '' }))
    fireEvent.change(urlField('Google Chat'), { target: { value: CANARY_URL } })
    fireEvent.click(within(chat).getByRole('button', { name: 'Set URL' }))
    await flushNetwork()
    expect(puts).toEqual([])
    expect(screen.getByTestId('notification-change-result-google_chat')).toHaveTextContent(
      /^Not saved/,
    )
    expect(urlField('Google Chat')).toHaveValue(CANARY_URL)
    await flushNetwork(2)
    expect(cacheHoldsCanary()).toBe(false)
    const { puts: saved } = serveChannels(base)

    // Enter stores it; spaces around a pasted URL are dropped.
    fireEvent.change(urlField('Google Chat'), { target: { value: `  ${CANARY_URL} ` } })
    fireEvent.keyDown(urlField('Google Chat'), { key: 'Enter' })
    await flushNetwork()

    expect(saved).toEqual([{ kind: 'google_chat', body: { url: CANARY_URL } }])
    await flushNetwork(2)
    expect(cacheHoldsCanary()).toBe(false)
    expect(urlField('Google Chat')).toHaveValue('')
    expect(screen.getByTestId('notification-change-result-google_chat')).toHaveTextContent(
      'Saved. The next alert and test use this URL.',
    )
    expect(screen.getByTestId('notification-source-google_chat')).toHaveTextContent('Console')
    expect(channelRow('Google Chat')).toHaveTextContent(
      'Stored from the console; overrides PGLENS_GOOGLE_CHAT_WEBHOOK_URL',
    )
    expect(
      within(channelRow('Google Chat')).getByRole('button', { name: 'Replace URL' }),
    ).toBeEnabled()
    // Nothing of the address stays in the page.
    for (const part of [CANARY_URL, 'UICANARYSPACE', 'uiCanaryKey0001', 'uiCanaryToken0002']) {
      expect(view.container.innerHTML).not.toContain(part)
    }
  })

  it('NOTI-UI-006 Remove stored URL shows only for a URL stored from the console and asks first', async () => {
    const { deletes } = serveChannels(stored)
    await renderPanel()

    expect(
      within(channelRow('Generic webhook')).queryByRole('button', { name: /Remove stored URL/ }),
    ).toBeNull()
    const remove = within(channelRow('Google Chat')).getByRole('button', {
      name: 'Remove stored URL of Google Chat',
    })

    // Cancel sends nothing.
    fireEvent.click(remove)
    await flushNetwork(2)
    let dialog = screen.getByRole('dialog')
    expect(dialog).toHaveTextContent('Remove the stored URL of Google Chat?')
    expect(dialog).toHaveTextContent(
      'falls back to the server environment’s URL (PGLENS_GOOGLE_CHAT_WEBHOOK_URL) if one is set',
    )
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    await flushNetwork()
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(deletes).toEqual([])

    // Confirmed: back to the environment.
    fireEvent.click(remove)
    await flushNetwork(2)
    dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove stored URL' }))
    await flushNetwork()
    expect(deletes).toEqual(['google_chat'])
    expect(screen.getByTestId('notification-change-result-google_chat')).toHaveTextContent(
      'Removed. The channel uses the server environment’s URL again.',
    )
    expect(screen.getByTestId('notification-source-google_chat')).toHaveTextContent('Environment')
    expect(
      within(channelRow('Google Chat')).queryByRole('button', { name: /Remove stored URL/ }),
    ).toBeNull()

    // Without an environment value the channel is not configured afterwards.
    fireEvent.click(
      within(channelRow('Slack')).getByRole('button', { name: 'Remove stored URL of Slack' }),
    )
    await flushNetwork(2)
    dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove stored URL' }))
    await flushNetwork()
    expect(deletes).toEqual(['google_chat', 'slack'])
    expect(screen.getByTestId('notification-change-result-slack')).toHaveTextContent(
      'Removed. The channel is not configured.',
    )
    expect(within(channelRow('Slack')).getByText('not configured')).toBeInTheDocument()
  })

  it('NOTI-UI-007 an address the server would refuse shows why before any request, and a refusal shows the server detail', async () => {
    // The cases of the server's NOTI-007, and more: the client decides as the
    // server does, on the address as typed.
    const accepted = [
      'https://chat.googleapis.com/v1/spaces/X',
      'https://hooks.canary.example/canaryPath0202?token=canaryTok0203',
      'HTTPS://hooks.canary.example/x',
      'http://127.0.0.1:8080/x',
      'http://127.1.2.3/x',
      'http://10.20.30.40/x',
      'http://172.16.0.1/x',
      'http://172.31.255.1/x',
      'http://192.168.1.10:9000/x',
      'http://localhost:8080/x',
      'HTTP://LocalHost:8080/x',
      'http://[::1]:8080/x',
      'http://[fd00::1]/x',
      'http://[::ffff:10.0.0.1]/x',
      'http://[::ffff:7f00:1]/x',
      'http://user:pw@127.0.0.1/x',
    ]
    for (const address of accepted) {
      expect(webhookUrlProblem(address), address).toBeNull()
    }
    const httpOnlyPrivate = /http is accepted only for a loopback or private address/
    const notAUrl = /This is not a URL/
    const refused: [string, RegExp][] = [
      ['', /Enter the webhook URL/],
      ['http://hooks.canary.example/x', httpOnlyPrivate],
      ['http://8.8.8.8/x', httpOnlyPrivate],
      ['http://172.32.0.1/x', httpOnlyPrivate],
      // Not a literal address for the server: no WHATWG normalisation.
      ['http://127.1/x', httpOnlyPrivate],
      ['http://2130706433/x', httpOnlyPrivate],
      ['http://0x0a.1.2.3/x', httpOnlyPrivate],
      ['http://010.0.0.1/x', httpOnlyPrivate],
      ['http://[::ffff:8.8.8.8]/x', httpOnlyPrivate],
      ['http://[::]/x', httpOnlyPrivate],
      ['http://127.0.0.1@8.8.8.8/x', httpOnlyPrivate],
      ['ftp://hooks.canary.example/x', /The scheme must be https/],
      ['hooks.canary.example/x', notAUrl],
      ['not a url', /spaces, tabs or line breaks/],
      ['https:///x', /names no host/],
      ['https:canaryOpaque0205', /names no host/],
      ['https://canary%zzhost/x', notAUrl],
      ['https://hooks.canary.example:8o/x', notAUrl],
      ['https://hooks.canary.example/a%zz', notAUrl],
      ['https://hooks.canary.example/canary Space', /spaces, tabs or line breaks/],
      [' https://hooks.canary.example/x', /spaces, tabs or line breaks/],
      ['https://hooks.canary.example/x ', /spaces, tabs or line breaks/],
      ['https://hooks.canary.example/\tx', /spaces, tabs or line breaks/],
      ['https://hooks.canary.example/x\r\n', /spaces, tabs or line breaks/],
      [`https://hooks.canary.example/${'a'.repeat(2048)}`, /longer than 2048/],
      // The bound counts bytes, as the server does.
      [`https://hooks.canary.example/${'é'.repeat(1010)}`, /longer than 2048/],
      ['https://hooks.canary.example/x#frag', /fragment/],
    ]
    for (const [address, message] of refused) {
      expect(webhookUrlProblem(address), address).toMatch(message)
    }

    const { puts } = serveChannels(base)
    await renderPanel()
    const chat = channelRow('Google Chat')
    const field = urlField('Google Chat')
    fireEvent.change(field, { target: { value: 'http://hooks.example.com/uiSecretPath0003' } })
    fireEvent.click(within(chat).getByRole('button', { name: 'Set URL' }))
    await flushNetwork()
    expect(puts).toEqual([])
    const result = screen.getByTestId('notification-change-result-google_chat')
    expect(result).toHaveTextContent(
      'http is accepted only for a loopback or private address; use https.',
    )
    expect(result).not.toHaveTextContent('uiSecretPath0003')
    expect(field).toHaveAttribute('aria-invalid', 'true')
    expect(field).toHaveAccessibleDescription(/http is accepted only/)
    // Typing again clears the message.
    fireEvent.change(field, { target: { value: 'https://hooks.example.com/x' } })
    expect(field).not.toHaveAttribute('aria-invalid')
    expect(result).toHaveTextContent(/^$/)

    // The server refuses: its detail is shown and nothing changes.
    server.use(
      status('setNotificationChannel', 400, {
        error: 'invalid url',
        detail: 'the URL names no host',
      }),
    )
    fireEvent.click(within(chat).getByRole('button', { name: 'Set URL' }))
    await flushNetwork()
    expect(result).toHaveTextContent('Not saved: invalid url: the URL names no host')
    expect(screen.getByTestId('notification-source-google_chat')).toHaveTextContent('Environment')
  })

  it('NOTI-UI-008 has no serious accessibility violations with stored URLs, a validation message and the remove dialog', async () => {
    serveChannels(stored)
    const view = await renderPanel()
    vi.useRealTimers()
    await expectNoA11yViolations(view.container)
    fireEvent.change(urlField('Generic webhook'), { target: { value: 'ftp://x' } })
    fireEvent.click(within(channelRow('Generic webhook')).getByRole('button', { name: 'Set URL' }))
    await waitFor(() =>
      expect(screen.getByTestId('notification-change-result-webhook')).toHaveTextContent(/scheme/),
    )
    await expectNoA11yViolations(view.container)
    fireEvent.click(
      within(channelRow('Google Chat')).getByRole('button', {
        name: 'Remove stored URL of Google Chat',
      }),
    )
    const dialog = await screen.findByRole('dialog')
    await expectNoA11yViolations(dialog)
  }, 20000)
})
