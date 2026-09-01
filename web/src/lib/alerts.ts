import type { Schemas } from '@/api/types'

export type Alert = Schemas['Alert']
export type Silence = Schemas['Silence']

type AlertSeverity = Alert['severity']
type AlertState = Alert['state']

export interface AlertSummary {
  firing: Record<AlertSeverity, number>
  suppressed: number
  resolved: number
}

export type SilenceWindowState = 'pending' | 'active' | 'expired'

export interface SilenceWindow {
  state: SilenceWindowState
  remainingSeconds: number
}

interface SilenceMatcher {
  name: string
  value: string
  is_regex?: boolean
  negate?: boolean
}

const severityOrder: Record<AlertSeverity, number> = {
  critical: 0,
  warning: 1,
  info: 2,
}

const stateOrder: Record<AlertState, number> = {
  firing: 0,
  pending: 1,
  resolved: 2,
}

function compareText(left: string, right: string): number {
  if (left === right) return 0
  return left < right ? -1 : 1
}

function clusterKey(alert: Alert): string {
  return alert.cluster_id === undefined ? '' : String(alert.cluster_id)
}

function compareAlerts(left: Alert, right: Alert): number {
  const state = stateOrder[left.state] - stateOrder[right.state]
  if (state !== 0) return state

  const severity = severityOrder[left.severity] - severityOrder[right.severity]
  if (severity !== 0) return severity

  const suppressed = Number(left.suppressed) - Number(right.suppressed)
  if (suppressed !== 0) return suppressed

  const cluster = compareText(clusterKey(left), clusterKey(right))
  if (cluster !== 0) return cluster

  return compareText(left.alert_key, right.alert_key)
}

/** Return a deterministically ranked copy without mutating the API response. */
export function rankAlerts<T extends Alert>(alerts: readonly T[]): T[] {
  return [...alerts].sort(compareAlerts)
}

/** Count unsuppressed firing alerts while reporting suppressed and resolved separately. */
export function summariseAlerts(alerts: readonly Alert[]): AlertSummary {
  const summary: AlertSummary = {
    firing: { critical: 0, warning: 0, info: 0 },
    suppressed: 0,
    resolved: 0,
  }

  for (const alert of alerts) {
    if (alert.suppressed) summary.suppressed += 1
    if (alert.state === 'resolved') summary.resolved += 1
    if (alert.state === 'firing' && !alert.suppressed) summary.firing[alert.severity] += 1
  }

  return summary
}

function reservedValue(name: string, alert: Alert): string | undefined {
  switch (name) {
    case 'rule_id':
      return alert.rule_id
    case 'severity':
      return alert.severity
    case 'cluster_id':
      return alert.cluster_id === undefined ? undefined : String(alert.cluster_id)
    case 'instance_id':
      return alert.instance_id === undefined ? undefined : String(alert.instance_id)
    case 'datname':
      return alert.datname
    default:
      return undefined
  }
}

function matcherValue(matcher: SilenceMatcher, alert: Alert): string {
  const reserved = reservedValue(matcher.name, alert)
  if (reserved !== undefined && reserved !== '') return reserved

  const label = alert.labels[matcher.name]
  return typeof label === 'string' ? label : ''
}

function matchesMatcher(matcher: SilenceMatcher, alert: Alert): boolean {
  const value = matcherValue(matcher, alert)
  let matches = value === matcher.value

  if (matcher.is_regex) {
    try {
      matches = new RegExp(`^(?:${matcher.value})$`).test(value)
    } catch {
      matches = false
    }
  }

  return matcher.negate ? !matches : matches
}

/** Mirror the server matcher semantics so a silence can be previewed safely. */
export function matchSilence(alert: Alert, silence: Silence): boolean {
  const matchers = silence.matchers as unknown as SilenceMatcher[]
  if (matchers.length === 0) return false
  return matchers.every((matcher) => matchesMatcher(matcher, alert))
}

/** Classify a silence using the server's half-open [starts_at, ends_at) window. */
export function silenceWindow(silence: Silence, now: Date): SilenceWindow {
  const startsAt = Date.parse(silence.starts_at)
  const endsAt = Date.parse(silence.ends_at)
  const current = now.getTime()

  if (!Number.isFinite(startsAt) || !Number.isFinite(endsAt)) {
    return { state: 'expired', remainingSeconds: 0 }
  }
  if (current < startsAt) {
    return { state: 'pending', remainingSeconds: (startsAt - current) / 1000 }
  }
  if (current >= endsAt) {
    return { state: 'expired', remainingSeconds: 0 }
  }
  return { state: 'active', remainingSeconds: (endsAt - current) / 1000 }
}
