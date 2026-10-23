import type { components } from '@/api/generated'

export type ClusterEvent = components['schemas']['Event']
export type EventSeverity = 'info' | 'warning' | 'critical'

export const DOCUMENTED_EVENT_TYPES = [
  'failover_detected',
  'split_brain_detected',
  'orphan_standby',
  'role_change',
  'slot_inactive',
  'slot_lost',
  'no_primary_in_cluster',
  'counter_reset_detected',
  'agent_down',
  'agent_up',
  'duplicate_instance_suspected',
  'cluster_id_changed',
  'instance_unreachable',
  'instance_reachable',
  'cardinality_truncated',
  'cluster_identity_conflict',
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
    case 'slot_lost':
      return {
        severity: 'critical',
        title: 'Replication slot lost WAL',
        whatToCheck:
          'The consumer of this slot cannot resume from it. Rebuild a physical standby, or recreate a logical slot and resynchronise its subscriber, then size max_slot_wal_keep_size so the slot is not lost again.',
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
    case 'instance_unreachable':
      return {
        severity: 'warning',
        title: 'Instance unreachable',
        whatToCheck:
          'The server stopped receiving reports from the instance. Check its agent and network path.',
      }
    case 'instance_reachable':
      return {
        severity: 'info',
        title: 'Instance reachable',
        whatToCheck: 'Confirm the instance is operating normally after the reporting gap.',
      }
    case 'cardinality_truncated':
      return {
        severity: 'warning',
        title: 'Series truncated by cardinality budget',
        whatToCheck:
          'The payload names the check and database. Reduce the objects it reports, or review its budget.',
      }
    case 'cluster_identity_conflict':
      return {
        severity: 'warning',
        title: 'Cluster identity conflict',
        whatToCheck:
          'The instance reported its configured cluster_name, not its system identifier. Apply monitoring_user.sql, then align cluster_name.',
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
