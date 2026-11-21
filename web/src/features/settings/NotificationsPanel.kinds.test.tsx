import { fireEvent, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import type { Schemas } from '@/api/types'
import { refreshLocale, setServerLanguage } from '@/i18n/store'
import { recipientsProblem } from '@/lib/notificationChannels'
import { ok, status } from '@/test/msw/handlers'
import { server } from '@/test/msw/server'
import { flushNetwork } from '@/test/network'
import { renderWithProviders } from '@/test/render'

import { NotificationsPanel } from './NotificationsPanel'

type NotificationChannel = Schemas['NotificationChannel']

/** The six kinds, none configured. */
const six: NotificationChannel[] = (
  ['slack', 'webhook', 'google_chat', 'teams', 'pagerduty', 'email'] as const
).map((kind) => ({ kind, configured: false, source: 'env', host: null, language: null }))

async function renderPanel() {
  const view = renderWithProviders(<NotificationsPanel />, { route: '/settings?tab=notifications' })
  await flushNetwork()
  return view
}

function row(kind: string) {
  return screen.getByTestId(`notification-channel-${kind}`)
}

describe('NotificationsPanel kinds (plan 007 phase 3)', () => {
  afterEach(async () => {
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('UI-NCH-001 lists the six kinds with their labels in English and Italian', async () => {
    server.use(ok('getNotificationChannels', six))
    const view = await renderPanel()
    const headings = () =>
      within(screen.getByRole('list', { name: 'Notification channels' }))
        .getAllByRole('heading', { level: 3 })
        .map((heading) => heading.textContent)
    expect(headings()).toEqual([
      'Slack',
      'Generic webhook',
      'Google Chat',
      'Microsoft Teams',
      'PagerDuty',
      'Email',
    ])
    expect(
      within(row('teams')).getByLabelText('Console URL of Microsoft Teams'),
    ).toBeInTheDocument()
    expect(within(row('pagerduty')).getByLabelText('Routing key of PagerDuty')).toBeInTheDocument()
    expect(within(row('email')).getByLabelText('Recipients of Email')).toBeInTheDocument()
    expect(row('teams')).toHaveTextContent('Post to a channel when a webhook request is received')
    expect(row('pagerduty')).toHaveTextContent('Events API v2 integration key')
    expect(row('email')).toHaveTextContent('PGLENS_SMTP_*')
    view.unmount()

    await setServerLanguage('it')
    await refreshLocale()
    await renderPanel()
    expect(row('email')).toHaveTextContent('Destinatari')
    expect(row('email')).toHaveTextContent('separati da virgole')
    expect(row('pagerduty')).toHaveTextContent('Routing key')
    expect(row('teams')).toHaveTextContent('Microsoft Teams')
  })

  it('UI-NCH-002 a routing key of 31 characters shows the problem and disables Save', async () => {
    server.use(ok('getNotificationChannels', six))
    await renderPanel()
    const pagerduty = row('pagerduty')
    const field = within(pagerduty).getByLabelText('Routing key of PagerDuty')
    const save = within(pagerduty).getByRole('button', { name: 'Set URL' })
    fireEvent.change(field, { target: { value: 'a'.repeat(31) } })
    expect(screen.getByTestId('notification-change-result-pagerduty')).toHaveTextContent(
      'The routing key must be 32 letters or digits.',
    )
    expect(save).toBeDisabled()
    expect(field).toHaveAttribute('aria-invalid', 'true')
    fireEvent.change(field, { target: { value: 'a'.repeat(32) } })
    expect(save).toBeEnabled()
    expect(screen.getByTestId('notification-change-result-pagerduty')).toHaveTextContent('')
  })

  it('UI-NCH-003 21 recipients show the problem text', async () => {
    server.use(ok('getNotificationChannels', six))
    await renderPanel()
    const email = row('email')
    const many = Array.from({ length: 21 }, (_, i) => `ops${i}@canary.example`).join(', ')
    fireEvent.change(within(email).getByLabelText('Recipients of Email'), {
      target: { value: many },
    })
    expect(screen.getByTestId('notification-change-result-email')).toHaveTextContent(
      'Enter between 1 and 20 email addresses, separated by commas.',
    )
    expect(within(email).getByRole('button', { name: 'Set URL' })).toBeDisabled()
    expect(recipientsProblem('Ops <ops@canary.example>, dba@canary.example')).toBeNull()
    expect(recipientsProblem('not an address')).not.toBeNull()
    expect(recipientsProblem('ops@canary.example,')).not.toBeNull()
  })

  it('UI-NCH-004 a 409 smtp_not_configured shows the SMTP text', async () => {
    server.use(
      ok('getNotificationChannels', six),
      status('setNotificationChannel', 409, {
        error: 'smtp_not_configured',
        detail: 'set PGLENS_SMTP_HOST and PGLENS_SMTP_FROM on the server',
      }),
    )
    await renderPanel()
    const email = row('email')
    fireEvent.change(within(email).getByLabelText('Recipients of Email'), {
      target: { value: 'ops@canary.example' },
    })
    fireEvent.click(within(email).getByRole('button', { name: 'Set URL' }))
    await flushNetwork()
    const result = screen.getByTestId('notification-change-result-email')
    expect(result).toHaveTextContent(
      'Email needs the SMTP settings on the server (PGLENS_SMTP_HOST, PGLENS_SMTP_FROM).',
    )
    expect(result).not.toHaveTextContent('ops@canary.example')
  })

  it('UI-NCH-005 the routing key input is masked', async () => {
    server.use(ok('getNotificationChannels', six))
    await renderPanel()
    const field = within(row('pagerduty')).getByLabelText('Routing key of PagerDuty')
    expect(field.className).toContain('[-webkit-text-security:disc]')
    expect(field).toHaveAttribute('autocomplete', 'off')
    expect(field).toHaveAttribute('placeholder', '32 letters or digits')
  })
})
