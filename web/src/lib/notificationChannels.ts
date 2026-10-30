import type { ApiFailure } from '@/api/client'
import type { Schemas } from '@/api/types'

/**
 * The alert notification channels and the server environment variables that
 * configure them (N6): one list shared by the Alerts page (Delivery) and the
 * Notifications tab of Settings. The server reads the variables in
 * `LoadAlertConfig` (`internal/server/config.go`); the console never receives
 * a webhook address (I-10).
 */

export type NotificationChannelKind = Schemas['NotificationChannelKind']
export type NotificationChannel = Schemas['NotificationChannel']
export type NotificationTestResult = Schemas['NotificationTestResult']

export interface NotificationChannelInfo {
  kind: NotificationChannelKind
  label: string
  /** The variable that holds the webhook address. */
  variable: string
  /** The variable naming a file that holds the address, when the server reads one. */
  fileVariable?: string
  /** Older names the server still accepts. */
  aliases: readonly string[]
}

/** The three channels, in the order the server lists them. */
export const NOTIFICATION_CHANNELS: readonly NotificationChannelInfo[] = [
  {
    kind: 'slack',
    label: 'Slack',
    variable: 'PGLENS_ALERT_SLACK_WEBHOOK_URL',
    fileVariable: 'PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE',
    aliases: ['PGLENS_SLACK_WEBHOOK_URL', 'PGLENS_SLACK_WEBHOOK_URL_FILE'],
  },
  {
    kind: 'webhook',
    label: 'Generic webhook',
    variable: 'PGLENS_WEBHOOK_URL',
    aliases: [],
  },
  {
    kind: 'google_chat',
    label: 'Google Chat',
    variable: 'PGLENS_GOOGLE_CHAT_WEBHOOK_URL',
    fileVariable: 'PGLENS_GOOGLE_CHAT_WEBHOOK_URL_FILE',
    aliases: [],
  },
]

export function notificationChannelInfo(kind: NotificationChannelKind): NotificationChannelInfo {
  const info = NOTIFICATION_CHANNELS.find((channel) => channel.kind === kind)
  if (info === undefined) throw new Error(`unknown notification channel ${kind}`)
  return info
}

/** Every variable that configures the channel: the main one, its file form, then the aliases. */
export function channelVariables(info: NotificationChannelInfo): string[] {
  return [
    info.variable,
    ...(info.fileVariable === undefined ? [] : [info.fileVariable]),
    ...info.aliases,
  ]
}

/** How to configure a channel that is not configured. */
export function channelInstructions(info: NotificationChannelInfo): string {
  const file = info.fileVariable === undefined ? '' : ' (or _FILE)'
  return `Set ${info.variable}${file} in the server environment, then restart the server.`
}

/** The outcome of a test send, as the Notifications tab shows it. */
export function testResultText(result: NotificationTestResult): string {
  switch (result.status) {
    case 'delivered':
      return 'Delivered'
    case 'timeout':
      return `No answer within 10 s${result.error ? `: ${result.error}` : ''}`
    case 'failed':
      return `Failed${result.error ? `: ${result.error}` : ''}`
  }
}

function isApiFailure(error: unknown): error is ApiFailure {
  return (
    typeof error === 'object' && error !== null && 'kind' in error && typeof error.kind === 'string'
  )
}

/** Why a test send could not be made at all (the request failed, not the delivery). */
export function testFailureMessage(error: unknown): string {
  if (!isApiFailure(error)) return 'Not sent. Try again.'
  switch (error.kind) {
    case 'server':
      if (error.status === 409) {
        return 'Not sent: the channel is not configured on the server. Reload the page.'
      }
      if (error.status === 429) {
        return 'Not sent: a test of this channel is already running. Wait for its result.'
      }
      return `Not sent: ${error.detail ? `${error.error}: ${error.detail}` : error.error}`
    case 'unprocessable':
      return `Not sent: ${error.detail ? `${error.error}: ${error.detail}` : error.error}`
    case 'not_found':
      return 'Not sent: the server does not know this channel. Reload the page.'
    case 'unauthorized':
      return 'Your session has expired. Sign in again to send a test message.'
    case 'forbidden':
      return 'Not sent: the server refused the request.'
    case 'network':
    case 'malformed':
    case 'client':
      return `Not sent: ${error.message}`
  }
}
