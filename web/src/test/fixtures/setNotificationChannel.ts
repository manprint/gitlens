import type { Schemas } from '@/api/types'

type NotificationChannel = Schemas['NotificationChannel']

export const responseStatus = 200

/** Google Chat after a URL is stored from the console: host only, source ui. */
export const base: NotificationChannel = {
  kind: 'google_chat',
  configured: true,
  source: 'ui',
  host: 'chat.googleapis.com',
}
