import { REFRESH } from '@/api/policy'
import type { Cluster, Schemas } from '@/api/types'

export type FleetInstance = Cluster['instances'][number]
export type FleetAlert = Schemas['Alert']

export const AGENT_STALE_AFTER_SECONDS = (REFRESH.fleet.interval * 3) / 1000

export interface InstanceStaleness {
  ageSeconds: number
  isDown: boolean
}

export interface AgentHealthIssue {
  ageSeconds: number
  cluster: Cluster
  instance: FleetInstance
  reason: string
}

/**
 * The instance's explicit up flag is authoritative. A missing or old report
 * also marks the agent down after three expected fleet pushes.
 */
export function instanceStaleness(
  instance: FleetInstance,
  now: number | Date = Date.now(),
): InstanceStaleness {
  const nowMs = typeof now === 'number' ? now : now.getTime()
  const lastSeenMs = Date.parse(instance.last_seen)
  const ageSeconds = Number.isFinite(lastSeenMs)
    ? Math.max(0, (nowMs - lastSeenMs) / 1000)
    : Number.POSITIVE_INFINITY

  return {
    ageSeconds,
    isDown: instance.up === false || ageSeconds >= AGENT_STALE_AFTER_SECONDS,
  }
}

function isAgentDownAlert(alert: FleetAlert): boolean {
  if (alert.state !== 'firing') return false
  return (
    alert.rule_id === 'agent_down' ||
    alert.rule_id === 'instance_unreachable' ||
    alert.alert_key.startsWith('agent_down/') ||
    alert.alert_key.startsWith('instance_unreachable/')
  )
}

function alertReason(alert: FleetAlert): string {
  const preferredLabels = ['cause', 'reason', 'error', 'message', 'check', 'target']
  for (const label of preferredLabels) {
    const value = alert.labels[label]
    if (typeof value === 'string' && value.trim()) return `${label}: ${value}`
  }
  return alert.summary || alert.rule_id
}

/** Derive the fleet's agent-health strip from joined instances and alerts. */
export function deriveAgentHealth(
  clusters: readonly Cluster[],
  alerts: readonly FleetAlert[],
  now: number | Date = Date.now(),
): AgentHealthIssue[] {
  const alertByInstance = new Map<string, FleetAlert>()
  for (const alert of alerts) {
    if (!isAgentDownAlert(alert) || !alert.instance_id) continue
    const existing = alertByInstance.get(alert.instance_id)
    if (!existing || (alert.rule_id === 'agent_down' && existing.rule_id !== 'agent_down')) {
      alertByInstance.set(alert.instance_id, alert)
    }
  }

  const issues: AgentHealthIssue[] = []
  for (const cluster of clusters) {
    for (const instance of cluster.instances) {
      const staleness = instanceStaleness(instance, now)
      const alert = alertByInstance.get(instance.instance_id)
      if (!alert && !staleness.isDown) continue

      issues.push({
        ageSeconds: staleness.ageSeconds,
        cluster,
        instance,
        reason: alert ? alertReason(alert) : 'The agent has not reported recently.',
      })
    }
  }

  return issues
}

/**
 * These explanations mirror README.md's GET /api/v1/clusters “Fields
 * explained” rules: ok means a primary exists, every instance is up, and no
 * replication lag is measurable; degraded means a primary exists but an
 * instance is down or a replica has measurable replay lag; critical means no
 * primary or more than one primary (split brain). The health field itself is
 * always supplied by the server and is never recomputed here.
 */
// Server source of truth: README.md, `GET /api/v1/clusters` health field.
// `ok` means a primary exists, all instances are up, and there is no replay lag;
// `degraded` means a primary exists but an instance is down or replay lag exists;
// `critical` means there is no primary or there is more than one (split brain).
// Keep these explanatory sentences aligned with the server's documented rules.
export function describeHealth(cluster: Cluster): string {
  const primaryCount = cluster.instances.filter((instance) => instance.role === 'primary').length

  switch (cluster.health) {
    case 'ok':
      return 'Healthy: a primary is present, all instances are up, and no replication lag is measurable.'
    case 'degraded': {
      const downInstance = cluster.instances.find((instance) => !instance.up)
      if (downInstance) return `Degraded: instance ${downInstance.addr} is down.`

      if (cluster.max_replay_lag_seconds != null && cluster.max_replay_lag_seconds > 0) {
        return `Degraded: standby replay lag is ${cluster.max_replay_lag_seconds} seconds.`
      }

      return 'Degraded: the cluster has a primary, but a health check requires attention.'
    }
    case 'critical':
      if (primaryCount > 1)
        return 'Critical: split brain detected; more than one primary is reporting.'
      return 'Critical: no primary is reporting.'
  }
}
