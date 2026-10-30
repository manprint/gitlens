import type { Schemas } from '@/api/types'

type NotificationChannel = Schemas['NotificationChannel']

export const responseStatus = 200

/** Google Chat back to the server environment after its stored URL is removed. */
export const base: NotificationChannel = {
  kind: 'google_chat',
  configured: true,
  source: 'env',
  host: 'chat.googleapis.com',
}

/** Slack with no environment value: not configured once its stored URL is removed. */
export const none: NotificationChannel = {
  kind: 'slack',
  configured: false,
  source: 'env',
  host: null,
}
