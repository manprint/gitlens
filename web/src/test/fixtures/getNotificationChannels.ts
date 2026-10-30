import type { Schemas } from '@/api/types'

type NotificationChannel = Schemas['NotificationChannel']

export const responseStatus = 200

/** Google Chat configured in the server environment (the dev stack, NOTI-E2E-001). */
export const base: NotificationChannel[] = [
  { kind: 'slack', configured: false, source: 'env', host: null },
  { kind: 'webhook', configured: false, source: 'env', host: null },
  { kind: 'google_chat', configured: true, source: 'env', host: 'chat.googleapis.com' },
]

/** No channel configured: alerts are stored but not delivered. */
export const none: NotificationChannel[] = [
  { kind: 'slack', configured: false, source: 'env', host: null },
  { kind: 'webhook', configured: false, source: 'env', host: null },
  { kind: 'google_chat', configured: false, source: 'env', host: null },
]

/** Every channel configured. */
export const all: NotificationChannel[] = [
  { kind: 'slack', configured: true, source: 'env', host: 'hooks.slack.com' },
  { kind: 'webhook', configured: true, source: 'env', host: 'alerts.example.net' },
  { kind: 'google_chat', configured: true, source: 'env', host: 'chat.googleapis.com' },
]
