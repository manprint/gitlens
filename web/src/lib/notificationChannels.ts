import type { ApiFailure } from '@/api/client'
import type { Schemas } from '@/api/types'
import type { MessageKey } from '@/i18n/messages/en'
import { translate } from '@/i18n/store'

/**
 * The alert notification channels and the server environment variables that
 * configure them (N6): one list shared by the Alerts page (Delivery) and the
 * Notifications tab of Settings. The server reads the variables in
 * `LoadAlertConfig` (`internal/server/config.go`); an address can also be
 * stored from the console (11.5b), write-only: the console never receives a
 * webhook address (I-10).
 */

export type NotificationChannelKind = Schemas['NotificationChannelKind']
export type NotificationChannel = Schemas['NotificationChannel']
export type NotificationTestResult = Schemas['NotificationTestResult']

export interface NotificationChannelInfo {
  kind: NotificationChannelKind
  /** English name; the console shows `channelLabel(info)`. */
  label: string
  labelKey: MessageKey
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
    labelKey: 'alerts.channel.label.slack',
    variable: 'PGLENS_ALERT_SLACK_WEBHOOK_URL',
    fileVariable: 'PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE',
    aliases: ['PGLENS_SLACK_WEBHOOK_URL', 'PGLENS_SLACK_WEBHOOK_URL_FILE'],
  },
  {
    kind: 'webhook',
    label: 'Generic webhook',
    labelKey: 'alerts.channel.label.webhook',
    variable: 'PGLENS_WEBHOOK_URL',
    aliases: [],
  },
  {
    kind: 'google_chat',
    label: 'Google Chat',
    labelKey: 'alerts.channel.label.google_chat',
    variable: 'PGLENS_GOOGLE_CHAT_WEBHOOK_URL',
    fileVariable: 'PGLENS_GOOGLE_CHAT_WEBHOOK_URL_FILE',
    aliases: [],
  },
]

/** The channel's name in the console language. */
export function channelLabel(info: NotificationChannelInfo): string {
  return translate(info.labelKey)
}

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
  return translate(
    info.fileVariable === undefined
      ? 'alerts.channel.instructions'
      : 'alerts.channel.instructionsFile',
    { variable: info.variable },
  )
}

/** The longest address the server stores (setNotificationChannel). */
export const MAX_WEBHOOK_URL_LENGTH = 2048

/** An IPv4 address as Go's net.ParseIP reads it: four decimal parts, no leading zero. */
function parseIPv4(text: string): number[] | null {
  const parts = text.split('.')
  if (parts.length !== 4) return null
  const bytes: number[] = []
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part) || (part.length > 1 && part.startsWith('0'))) return null
    const value = Number(part)
    if (value > 255) return null
    bytes.push(value)
  }
  return bytes
}

/** The 16-bit groups of an IPv6 address part, the last one possibly dotted IPv4. */
function ipv6Groups(part: string): number[] | null {
  if (part === '') return []
  const items = part.split(':')
  const groups: number[] = []
  for (const [index, item] of items.entries()) {
    if (index === items.length - 1 && item.includes('.')) {
      const v4 = parseIPv4(item)
      if (v4 === null) return null
      groups.push(((v4[0] ?? 0) << 8) | (v4[1] ?? 0), ((v4[2] ?? 0) << 8) | (v4[3] ?? 0))
    } else if (/^[0-9a-f]{1,4}$/i.test(item)) {
      groups.push(Number.parseInt(item, 16))
    } else {
      return null
    }
  }
  return groups
}

/** The eight groups of an IPv6 address, or null; no zone. */
function parseIPv6(text: string): number[] | null {
  const halves = text.split('::')
  if (halves.length > 2) return null
  if (halves.length === 1) {
    const groups = ipv6Groups(text)
    return groups !== null && groups.length === 8 ? groups : null
  }
  const [head = '', tail = ''] = halves
  if (head.includes('.')) return null
  const first = ipv6Groups(head)
  const last = ipv6Groups(tail)
  if (first === null || last === null || first.length + last.length > 7) return null
  return [...first, ...Array<number>(8 - first.length - last.length).fill(0), ...last]
}

/** Whether an IPv4 address is loopback (127.0.0.0/8) or private (10/8, 172.16/12, 192.168/16). */
function isPrivateIPv4([a = 0, b = 0]: number[]): boolean {
  return a === 127 || a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)
}

/**
 * Whether a host is localhost or a literal loopback or private address, as
 * the server decides it (net.ParseIP, IsLoopback, IsPrivate): 127.0.0.0/8,
 * ::1, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7, and an IPv4 one
 * mapped in IPv6 (::ffff:a.b.c.d). `host` is the literal host of the address,
 * without brackets: never normalised, so 127.1 or 2130706433 is not an address
 * here, as on the server.
 */
function isPrivateHost(host: string): boolean {
  if (host.toLowerCase() === 'localhost') return true
  const v4 = parseIPv4(host)
  if (v4 !== null) return isPrivateIPv4(v4)
  const v6 = parseIPv6(host)
  if (v6 === null) return false
  const [g0, g1, g2, g3, g4, g5, g6 = 0, g7 = 0] = v6
  const zeros = g0 === 0 && g1 === 0 && g2 === 0 && g3 === 0 && g4 === 0
  if (zeros && g5 === 0 && g6 === 0 && g7 === 1) return true
  if (zeros && g5 === 0xffff) return isPrivateIPv4([g6 >> 8, g6 & 0xff])
  return ((g0 ?? 0) & 0xfe00) === 0xfc00
}

/** A percent sign that does not start an escape of two hex digits. */
const BAD_ESCAPE = /%(?![0-9a-f]{2})/i

/** The host of an address as Go's url.Parse reads it, or why it does not parse. */
function literalHost(authority: string): { host: string } | { problem: 'parse' } {
  const at = authority.lastIndexOf('@')
  const userinfo = at < 0 ? '' : authority.slice(0, at)
  const hostport = authority.slice(at + 1)
  if (!/^[A-Za-z0-9\-._:~!$&'()*+,;=%@]*$/.test(userinfo) || BAD_ESCAPE.test(userinfo)) {
    return { problem: 'parse' }
  }
  let host: string
  let port: string
  if (hostport.startsWith('[')) {
    const close = hostport.indexOf(']')
    if (close < 0) return { problem: 'parse' }
    host = hostport.slice(1, close)
    const after = hostport.slice(close + 1)
    if (after !== '' && !after.startsWith(':')) return { problem: 'parse' }
    port = after.slice(1)
  } else {
    const colon = hostport.lastIndexOf(':')
    host = colon < 0 ? hostport : hostport.slice(0, colon)
    port = colon < 0 ? '' : hostport.slice(colon + 1)
  }
  if (!/^\d*$/.test(port)) return { problem: 'parse' }
  // An escape in a host name stands only for a byte of a non-ASCII character.
  if (/%(?!25|[89a-f][0-9a-f])/i.test(host)) return { problem: 'parse' }
  // Go refuses these in a host name; spaces and control characters are refused before.
  if (/[\\^`{|}]/.test(host)) return { problem: 'parse' }
  try {
    return { host: decodeURIComponent(host) }
  } catch {
    // Escaped bytes that are not UTF-8: the server takes them, and such a
    // host is never a loopback or private address.
    return { host }
  }
}

/**
 * Why the server would refuse a webhook address, or null when it accepts it:
 * the checks of setNotificationChannel (validateWebhookURL), on the address as
 * typed, so a mistake shows before the request. The message never quotes the
 * address.
 */
export function webhookUrlProblem(raw: string): string | null {
  if (raw === '') return translate('alerts.channel.url.empty')
  if (new TextEncoder().encode(raw).length > MAX_WEBHOOK_URL_LENGTH) {
    return translate('alerts.channel.url.tooLong', { max: String(MAX_WEBHOOK_URL_LENGTH) })
  }
  if (/[ \t\r\n]/.test(raw)) return translate('alerts.channel.url.spaces')
  if (raw.includes('#')) return translate('alerts.channel.url.fragment')
  const notAUrl = translate('alerts.channel.url.invalid')
  // eslint-disable-next-line no-control-regex
  if (/[\x00-\x1f\x7f]/.test(raw)) return notAUrl
  const scheme = /^([A-Za-z][A-Za-z0-9+.-]*):/.exec(raw)
  if (scheme === null) return notAUrl
  const rest = raw.slice(scheme[0].length)
  if (!rest.startsWith('//')) return translate('alerts.channel.url.noHost')
  const authorityEnd = rest.slice(2).search(/[/?]/)
  const authority = authorityEnd < 0 ? rest.slice(2) : rest.slice(2, 2 + authorityEnd)
  const path = authorityEnd < 0 ? '' : rest.slice(2 + authorityEnd).split('?')[0]
  if (BAD_ESCAPE.test(path ?? '')) return notAUrl
  const parsed = literalHost(authority)
  if ('problem' in parsed) return notAUrl
  if (parsed.host === '') return translate('alerts.channel.url.noHost')
  const name = (scheme[1] ?? '').toLowerCase()
  if (name === 'https') return null
  if (name === 'http') {
    return isPrivateHost(parsed.host) ? null : translate('alerts.channel.url.httpPublic')
  }
  return translate('alerts.channel.url.scheme')
}

/** Where a channel's address comes from, as its badge reads. */
export function channelSourceLabel(channel: NotificationChannel): string {
  return translate(
    channel.source === 'ui' ? 'alerts.channel.source.ui' : 'alerts.channel.source.env',
  )
}

/** Why an address could not be stored or removed (the request failed). */
export function channelChangeFailureMessage(error: unknown, action: 'save' | 'remove'): string {
  const verb = translate(
    action === 'save' ? 'alerts.channel.change.notSaved' : 'alerts.channel.change.notRemoved',
  )
  if (!isApiFailure(error)) return translate('alerts.channel.change.retry', { verb })
  switch (error.kind) {
    case 'server':
    case 'unprocessable':
      return translate('alerts.channel.change.error', { verb, error: failureText(error) })
    case 'not_found':
      return translate('alerts.channel.change.notFound', { verb })
    case 'unauthorized':
      return translate('alerts.channel.change.unauthorized')
    case 'forbidden':
      return translate('alerts.channel.change.forbidden', { verb })
    case 'network':
    case 'malformed':
    case 'client':
      return translate('alerts.channel.change.error', { verb, error: error.message })
  }
}

/** The outcome of a test send, as the Notifications tab shows it. */
export function testResultText(result: NotificationTestResult): string {
  switch (result.status) {
    case 'delivered':
      return translate('alerts.channel.result.delivered')
    case 'timeout':
      return result.error
        ? translate('alerts.channel.result.timeoutError', { error: result.error })
        : translate('alerts.channel.result.timeout')
    case 'failed':
      return result.error
        ? translate('alerts.channel.result.failedError', { error: result.error })
        : translate('alerts.channel.result.failed')
  }
}

function failureText(error: { error: string; detail?: string }): string {
  return error.detail ? `${error.error}: ${error.detail}` : error.error
}

function isApiFailure(error: unknown): error is ApiFailure {
  return (
    typeof error === 'object' && error !== null && 'kind' in error && typeof error.kind === 'string'
  )
}

/** Why a test send could not be made at all (the request failed, not the delivery). */
export function testFailureMessage(error: unknown): string {
  if (!isApiFailure(error)) return translate('alerts.channel.test.retry')
  switch (error.kind) {
    case 'server':
      if (error.status === 409) {
        return translate('alerts.channel.test.notConfigured')
      }
      if (error.status === 429) {
        return translate('alerts.channel.test.running')
      }
      return translate('alerts.channel.test.error', { error: failureText(error) })
    case 'unprocessable':
      return translate('alerts.channel.test.error', { error: failureText(error) })
    case 'not_found':
      return translate('alerts.channel.test.notFound')
    case 'unauthorized':
      return translate('alerts.channel.test.unauthorized')
    case 'forbidden':
      return translate('alerts.channel.test.forbidden')
    case 'network':
    case 'malformed':
    case 'client':
      return translate('alerts.channel.test.error', { error: error.message })
  }
}
