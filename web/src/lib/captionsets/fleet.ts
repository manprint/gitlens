import type { CaptionEntry } from '../captions'

/** Captions of the fleet route group (D30); rules in the plan, phase 17. */
export const fleetCaptions = {
  'fleet.summary-all': {
    what: 'Number of monitored clusters in the fleet, whatever their health; selecting the card clears the health filter.',
    source:
      'The cluster list (handleClusters in internal/server/api.go), counted in the browser; a cluster with no instance is not listed.',
    unit: 'clusters',
    window: 'Latest list; refreshed every 15 s.',
  },
  'fleet.summary-healthy': {
    what: 'Clusters whose health is ok: exactly one primary, every instance up and replay lag of 30 seconds or less.',
    source:
      'The health field the server computes for each cluster (healthForCluster in internal/server/api.go), counted in the browser.',
    unit: 'clusters',
    window: 'Latest list; refreshed every 15 s.',
    howToRead:
      'Selecting the card lists only these clusters; selecting it again clears the filter. A cluster with no lag reading is not penalised.',
    thresholds: 'Replay lag above 30 seconds is no longer ok.',
  },
  'fleet.summary-degraded': {
    what: 'Clusters with one primary where an instance is down or the largest standby replay lag is above 30 seconds.',
    source:
      'The health field the server computes for each cluster (healthForCluster in internal/server/api.go), counted in the browser.',
    unit: 'clusters',
    window: 'Latest list; refreshed every 15 s.',
    howToRead:
      'Open a degraded cluster card to see which instance is down or how large the lag is.',
    thresholds: 'Down means no report for 60 seconds; lag threshold is 30 seconds.',
  },
  'fleet.summary-critical': {
    what: 'Clusters that report no primary, or more than one primary (split brain).',
    source:
      'The health field the server computes for each cluster (healthForCluster in internal/server/api.go), counted in the browser.',
    unit: 'clusters',
    window: 'Latest list; refreshed every 15 s.',
    howToRead:
      'Critical depends only on the number of primaries, not on lag; the cluster page shows which instances claim the role.',
  },
  'fleet.summary-instances-up': {
    what: 'Instances that reported in the last 60 seconds, counted over all clusters; selecting the card lists clusters with at least one.',
    source:
      'The up flag of each instance in the cluster list: its last report is at most 60 seconds old (upThreshold in internal/server/api.go).',
    unit: 'instances',
    window: 'Latest list; refreshed every 15 s.',
  },
  'fleet.summary-instances-down': {
    what: 'Instances that did not report in the last 60 seconds, counted over all clusters; selecting the card lists clusters with at least one.',
    source:
      'The up flag of each instance in the cluster list: its last report is older than 60 seconds (upThreshold in internal/server/api.go).',
    unit: 'instances',
    window: 'Latest list; refreshed every 15 s.',
    howToRead:
      'An instance replaced by a newer one at the same address and port stays listed as down here, but the cluster health ignores it.',
  },
  'fleet.summary-firing-alerts': {
    what: 'Alerts firing now across the fleet; the card is disabled when the alert list could not be loaded.',
    source: 'The alert list (/api/v1/alerts) filtered to state firing, counted in the browser.',
    unit: 'alerts',
    window: 'Latest list; refreshed every 15 s.',
    howToRead:
      'Selecting the card keeps the clusters with a firing alert, matched by cluster or by instance. A notice shows when the list was cut, and the count is then a lower bound.',
  },
  'fleet.clusters': {
    what: 'One card per monitored cluster with its health, primary, instance and standby counts, largest replay lag and permission tiers.',
    source:
      'The cluster list (handleClusters in internal/server/api.go): clusters and instances tables, topology edges, and replay lag readings of the last 60 seconds.',
    unit: 'one card per cluster; lag in seconds',
    window: 'Latest list; refreshed every 15 s.',
    howToRead:
      'Max replay lag is the largest lag any standby reported in the last 60 seconds, empty when none did. Permission tiers are the T0 to T3 levels the agents hold. Health ok, degraded and critical follow the summary cards above.',
  },
  'fleet.agent-health': {
    what: 'Instances that are down or have a firing agent-down or unreachable alert, with the likely cause and the time since they last reported.',
    source:
      'deriveAgentHealth in the browser: instances marked down or silent for 45 seconds, and firing alerts agent_down or instance_unreachable.',
    unit: 'instances',
    window: 'Latest list; refreshed every 15 s.',
    howToRead:
      'Values of the clusters listed here may be stale. The likely cause is the alert text when an alert fired, otherwise that the agent has not reported recently.',
  },
  'cluster.drift-table': {
    what: 'Settings whose value differs between the instances of this cluster, one row per setting and one column per instance.',
    source:
      'handleSettingsDrift in internal/server/api_settings.go: the settings each agent last reported (object_facts), for instances seen in the last hour.',
    unit: 'setting values',
    window: 'Latest scrape of each instance that reported in the last hour; refreshed every 15 s.',
    howToRead:
      'Highlighted cells differ from the reference, the value most instances report; on a tie the primary value is used. Settings expected to differ, such as port and listen_addresses, are left out.',
  },
  'cluster.slots-table': {
    what: 'Replication slots of this cluster with their state, the WAL each one holds back and how long inactive ones have been inactive.',
    source:
      'handleSlots in internal/server/api_topology.go: pg_replication_slots read by the replication_slots check, newest reading of each slot.',
    unit: 'bytes and durations',
    window: 'Newest reading of each slot in the last 5 minutes; refreshed every 15 s.',
    howToRead:
      'Retained bytes is the WAL kept since the slot restart position. WAL before invalidation is the room left before the slot loses WAL; unlimited means no size cap is set. Age inactive counts from the last active reading.',
    thresholds: 'Retained bytes above 1 GiB are highlighted.',
  },
  'topology.graph': {
    what: 'Replication links between the instances of this cluster; each arrow runs from a standby to the upstream it receives WAL from.',
    source:
      'The topology edges stored for the cluster (handleTopology in internal/server/api_topology.go), from each standby WAL receiver and the sync state on the primary.',
    unit: 'instances and replication edges',
    window: 'Latest edges; refreshed every 15 s.',
    howToRead:
      'A dashed edge has low confidence: the standby is not streaming from that upstream right now. A banner appears when no primary or more than one primary is reported.',
  },
  'cluster.events': {
    what: 'Failovers detected in this cluster, newest first and grouped by day, with the old and the new primary.',
    source:
      'handleTopology in internal/server/api_topology.go: events of type failover_detected, the newest 100 of the cluster.',
    unit: 'events',
    window: 'The newest 100 failover events on record.',
    howToRead:
      'Severity and the suggested check come from the console description of each event type. An empty list means no failover was detected.',
  },
  'lag.edge': {
    what: 'Write, flush and replay lag in seconds between one standby and its upstream over the selected range, one line each.',
    source:
      'pg_stat_replication write_lag, flush_lag and replay_lag read by the replication_streaming check every 10 seconds; a standby alone reports an estimated replay lag.',
    unit: 'seconds',
    window: 'Selected time range, raw samples, at most 30 days.',
    howToRead:
      'Each lag is the wait between flushing WAL on the upstream and the standby confirming it was written, flushed or replayed. A gap is a missing sample; nothing is filled in.',
  },
} as const satisfies Record<string, CaptionEntry>
