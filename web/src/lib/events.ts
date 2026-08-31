import type { components } from '@/api/generated'

export type ClusterEvent = components['schemas']['Event']
export type EventSeverity = 'info' | 'warning' | 'critical'

export const DOCUMENTED_EVENT_TYPES = [
  'failover_detected',
  'split_brain_detected',
  'orphan_standby',
  'role_change',
  'slot_inactive',
  'slot_retained_bytes',
  'no_primary_in_cluster',
  'counter_reset_detected',
  'agent_down',
  'agent_up',
  'check_circuit_open',
  'duplicate_instance_suspected',
  'cluster_id_changed',
] as const

export type DocumentedEventType = (typeof DOCUMENTED_EVENT_TYPES)[number]

export interface EventDescription {
  severity: EventSeverity
  title: string
  whatToCheck: string
}

const UNKNOWN_EVENT: EventDescription = {
  severity: 'info',
  title: 'Unknown event type',
  whatToCheck: 'Review the raw event type and payload before taking action.',
}

export function isDocumentedEventType(type: string): type is DocumentedEventType {
  return (DOCUMENTED_EVENT_TYPES as readonly string[]).includes(type)
}

/**
 * Keep this switch exhaustive: adding a documented type without its operator
 * guidance must be a compile-time error.
 */
export function describeDocumentedEvent(type: DocumentedEventType): EventDescription {
  switch (type) {
    case 'failover_detected':
      return {
        severity: 'warning',
        title: 'Failover detected',
        whatToCheck: 'Check whether the promotion was planned or caused by a primary failure.',
      }
    case 'split_brain_detected':
      return {
        severity: 'critical',
        title: 'Split brain detected',
        whatToCheck: 'Identify the authoritative primary and demote the other primary.',
      }
    case 'orphan_standby':
      return {
        severity: 'warning',
        title: 'Orphan standby',
        whatToCheck: 'Check the upstream primary and promote or reconnect the standby if needed.',
      }
    case 'role_change':
      return {
        severity: 'info',
        title: 'Role changed',
        whatToCheck: "Check the instance's new role and confirm the change was expected.",
      }
    case 'slot_inactive':
      return {
        severity: 'warning',
        title: 'Replication slot inactive',
        whatToCheck: 'Check whether the standby is down or disconnected and needs reconnecting.',
      }
    case 'slot_retained_bytes':
      return {
        severity: 'warning',
        title: 'Replication slot retaining WAL',
        whatToCheck: 'Monitor disk usage and reconnect the lagging standby before WAL fills disk.',
      }
    case 'no_primary_in_cluster':
      return {
        severity: 'critical',
        title: 'No primary in cluster',
        whatToCheck: 'Investigate why no instance is primary and promote a standby if needed.',
      }
    case 'counter_reset_detected':
      return {
        severity: 'info',
        title: 'Counter reset detected',
        whatToCheck: 'Check for pg_stat_reset() or an instance restart; this is usually expected.',
      }
    case 'agent_down':
      return {
        severity: 'warning',
        title: 'Agent down',
        whatToCheck: 'Check agent logs, the container, network reachability and the server.',
      }
    case 'agent_up':
      return {
        severity: 'info',
        title: 'Agent recovered',
        whatToCheck: 'Confirm the instance is operating normally after the reporting gap.',
      }
    case 'check_circuit_open':
      return {
        severity: 'warning',
        title: 'Check circuit open',
        whatToCheck: 'Check permissions, query timeouts and the instance error log.',
      }
    case 'duplicate_instance_suspected':
      return {
        severity: 'warning',
        title: 'Duplicate instance suspected',
        whatToCheck: 'Verify persistent identity storage so the agent keeps its instance_id.',
      }
    case 'cluster_id_changed':
      return {
        severity: 'critical',
        title: 'Cluster identity changed',
        whatToCheck: 'Investigate invariant I-1 and review the agent system_identifier and logs.',
      }
    default: {
      const exhaustive: never = type
      return exhaustive
    }
  }
}

export function describeEvent(type: string): EventDescription {
  return isDocumentedEventType(type) ? describeDocumentedEvent(type) : UNKNOWN_EVENT
}

export function sortEventsNewestFirst(events: readonly ClusterEvent[]): ClusterEvent[] {
  return [...events].sort((left, right) => {
    const rightTime = Date.parse(right.ts)
    const leftTime = Date.parse(left.ts)
    const timeDifference =
      (Number.isNaN(rightTime) ? Number.NEGATIVE_INFINITY : rightTime) -
      (Number.isNaN(leftTime) ? Number.NEGATIVE_INFINITY : leftTime)
    return timeDifference || right.event_id - left.event_id
  })
}
