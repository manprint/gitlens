import type { Schemas } from '@/api/types'

type NotificationTestResult = Schemas['NotificationTestResult']

export const responseStatus = 200

/** The channel accepted the test message. */
export const base: NotificationTestResult = { delivered: true, status: 'delivered' }

/** The webhook refused the message; the error is redacted by the server. */
export const failed: NotificationTestResult = {
  delivered: false,
  status: 'failed',
  error: 'notification to [redacted webhook] returned HTTP 404: Not Found',
}

/** No delivery confirmed within 10 s. */
export const timeout: NotificationTestResult = {
  delivered: false,
  status: 'timeout',
  error: 'context deadline exceeded',
}
