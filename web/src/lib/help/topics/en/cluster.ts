import type { HelpTopic } from '../../types'

export const clusterTopic: HelpTopic = {
  id: 'cluster',
  group: 'monitoring',
  title: 'Cluster detail',
  summary:
    'The detail page of one cluster: its replication topology, the replication lag over time, the replication slots, the configuration drift between its instances and the failovers detected. How to read each tab and what to do with a lag or a missing replica.',
  page: {
    path: '/',
    label: 'Open Fleet',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'Cluster detail shows one cluster: how its instances replicate, how far behind the standbys are, which replication slots hold WAL back, where the configuration of its instances differs and which failovers were detected. It opens from a card of [{{ui:shell.nav.fleet}}](/help/fleet) and its address is `/clusters/<cluster_id>`. Each tab reads its own data from the server and refreshes by itself.',
        },
        {
          kind: 'steps',
          items: [
            'Open the cluster from its card in [{{ui:shell.nav.fleet}}](/help/fleet).',
            'Read the Cluster identity line and, in the subtitle, the number of monitored instances and the health of the cluster.',
            'Choose the tab that answers your question: [{{ui:cluster.tabs.topology}}](/help/cluster#topology), [{{ui:cluster.tabs.lag}}](/help/cluster#lag), [{{ui:cluster.tabs.slots}}](/help/cluster#slots), [{{ui:cluster.tabs.drift}}](/help/cluster#drift) or [{{ui:cluster.tabs.events}}](/help/cluster#events).',
            'If a problem is shown, follow [What to do with a problem](/help/cluster#what-to-do).',
          ],
        },
      ],
      subsections: [
        {
          id: 'page-controls',
          title: 'Page controls',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Time range',
                  meaning:
                    'The time range picker in the header sets the window of the replication lag charts.',
                },
                {
                  term: 'Cluster identity',
                  meaning:
                    'The cluster identifier, shown as code and followed by its source. When it comes from the PostgreSQL system identifier (`system_identifier`), pglens uses that to tell clusters apart. When the source is `manual`, it comes from a cluster name supplied by the user and not from the system identifier.',
                  action:
                    'If the identity does not come from the system identifier, follow the grant instructions linked under the cluster card in [{{ui:shell.nav.fleet}}](/help/fleet).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'topology',
      title: 'Topology',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:cluster.tabs.topology}} tab draws the replication links of the cluster: each arrow runs from a standby, a read-only copy, to the server it copies changes from. Above the graph, a line counts the instances and the replication edges. Selecting an instance node opens its instance page.',
        },
      ],
      subsections: [
        {
          id: 'graph-nodes',
          title: 'Instance nodes',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Role badge',
                  meaning:
                    'Role of the instance. `primary` is the primary; `standby` is a read-only copy. `unresolved` marks an edge endpoint that is not a known instance, and `unknown` marks an instance whose role is missing.',
                },
                {
                  term: '{{ui:shell.topology.state.down}}, {{ui:shell.topology.state.up}} or {{ui:shell.topology.state.unknown}}',
                  meaning:
                    'Whether the agent of the instance has reported recently: {{ui:shell.topology.state.up}} when its last report is at most 60 seconds old, {{ui:shell.topology.state.down}} when it is older, {{ui:shell.topology.state.unknown}} when the server has no state for it. A {{ui:shell.topology.state.down}} node has a border in the destructive colour.',
                  action:
                    'Check the agent of the instance; see [{{ui:shell.nav.fleet}}](/help/fleet).',
                },
                {
                  term: '{{ui:shell.topology.port}}',
                  meaning: 'Port the instance listens on.',
                },
                {
                  term: '{{ui:shell.topology.version}}',
                  meaning: 'PostgreSQL version the instance runs.',
                },
                {
                  term: '{{ui:shell.topology.tier}}',
                  meaning:
                    'Permission tier the agent holds on the instance: T0 reads statistics, T1 also runs EXPLAIN for query plans, T2 also cancels or ends sessions.',
                },
                {
                  term: '{{ui:shell.topology.replayLag}}',
                  meaning:
                    'On a standby: its replay lag in seconds, with three decimals, or Unknown when it has not been reported.',
                },
              ],
            },
          ],
        },
        {
          id: 'graph-arcs',
          title: 'Arcs',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Arc label',
                  meaning:
                    'The sync state of the standby with its upstream, as the primary reports it. {{ui:cluster.common.unknown}} when it is missing.',
                },
                {
                  term: 'Dashed arc',
                  meaning:
                    'Low confidence: the standby is not streaming from that upstream right now, or it has not reported for more than 60 seconds.',
                  action: 'See [What to do with a problem](/help/cluster#what-to-do).',
                },
                {
                  term: 'Selecting an arc',
                  meaning:
                    'Marks the arc as the selected edge. The graph shows no other detail: the values are in the edge table.',
                },
              ],
            },
          ],
        },
        {
          id: 'anomalies',
          title: 'Anomaly banners',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:cluster.topology.anomalyTitle}}',
                  meaning:
                    'Alert banner shown when the graph has no primary or more than one primary. It reads either Split brain: more than one primary is reported. or No primary is reported.',
                  action: 'See [What to do with a problem](/help/cluster#what-to-do).',
                },
                {
                  term: '{{ui:cluster.topology.warningTitle}}',
                  meaning:
                    'Status banner shown when one or more standbys have no upstream, or one or more edges have low confidence. It reads One or more standbys have no upstream. and, when they apply, One or more edges have low confidence.',
                  action:
                    'A standby with no upstream is not copying changes from any server; see [What to do with a problem](/help/cluster#what-to-do).',
                },
              ],
            },
          ],
        },
        {
          id: 'edge-table',
          title: 'Edge table',
          blocks: [
            {
              kind: 'text',
              text: 'Below the graph, the {{ui:cluster.topology.showTable}} button opens the same links as a table, for keyboard and screen reader use. {{ui:cluster.topology.hideTable}} closes it.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:cluster.topology.colFrom}}',
                  meaning: 'The standby that copies changes.',
                },
                {
                  term: '{{ui:cluster.topology.colTo}}',
                  meaning: 'The server the standby copies changes from, its upstream.',
                },
                {
                  term: '{{ui:cluster.topology.colType}}',
                  meaning: 'Kind of replication link, as the agent reports it.',
                },
                {
                  term: '{{ui:cluster.topology.colSync}}',
                  meaning:
                    'Sync state of the link. {{ui:cluster.common.unknown}} when it is missing.',
                },
                {
                  term: '{{ui:cluster.topology.colConfidence}}',
                  meaning:
                    '`high` for a solid arc, `low` for a dashed arc: the standby is not streaming from that upstream right now, or has not reported for more than 60 seconds.',
                },
                {
                  term: '{{ui:cluster.topology.colNote}}',
                  meaning:
                    'A note from the server about the link, when it has one, for example on a low confidence link.',
                },
                {
                  term: 'Type and Sync state filters',
                  meaning:
                    'Two selects narrow the table by link type and by sync state. The search box matches the {{ui:cluster.topology.colFrom}}, {{ui:cluster.topology.colTo}} and {{ui:cluster.topology.colNote}} columns.',
                },
                {
                  term: '{{ui:cluster.topology.noEdges}}',
                  meaning: 'The table has no link to show.',
                  action:
                    'Check that each standby is monitored and reports to the server; see [What to do with a problem](/help/cluster#what-to-do).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'lag',
      title: 'Replication lag',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:cluster.tabs.lag}} tab draws one chart for each standby and its upstream, over the selected time range. Each chart names the link from the standby to its upstream, with its sync state, and plots three lines in seconds: write lag, flush lag and replay lag. The charts use raw samples, taken every 10 seconds, and cover at most 30 days.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'write_lag_sec',
              meaning:
                'Seconds between the upstream flushing WAL and the standby confirming it has written it.',
            },
            {
              term: 'flush_lag_sec',
              meaning:
                'Seconds between the upstream flushing WAL and the standby confirming it has flushed it to disk.',
            },
            {
              term: 'replay_lag_sec',
              meaning:
                'Seconds between the upstream flushing WAL and the standby confirming it has replayed it. When the standby reports it alone, the replay lag is an estimate.',
              action:
                'If the replay lag stays above 30 seconds, the standby is behind; see [What to do with a problem](/help/cluster#what-to-do).',
            },
            {
              term: '{{ui:cluster.lag.noneTitle}}',
              meaning:
                'No lag data in this range, so nothing is charted. Lag is recorded for each standby that copies changes from a primary of this cluster.',
              action:
                'Widen the time range in the header, or check the standby; see [What to do with a problem](/help/cluster#what-to-do).',
            },
            {
              term: '{{ui:cluster.lag.noData}}',
              meaning:
                'The chart of one link has no sample: the message No replication lag sample was returned for this edge. appears.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'A gap in a line means a missing, unmeasured sample: nothing is filled in. When the selected range is longer than the 30-day raw retention window, the tab shows the notice Selected range exceeds the 30-day raw retention window. Choose a shorter range to see the raw samples.',
        },
        {
          kind: 'sql',
          label: 'Replication lag as the primary sees it (run on the primary)',
          code: 'SELECT application_name, client_addr, state, sync_state, write_lag, flush_lag, replay_lag FROM pg_stat_replication;',
        },
      ],
      subsections: [
        {
          id: 'replay-lag-bytes',
          title: 'Replay lag (bytes)',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:cluster.lag.replayBytes}}',
                  meaning:
                    'Lag of the standby in bytes, as the primary reports it. It needs a sample from the last 60 seconds. Otherwise a dash (—) appears, whose hover text says No fresh byte lag sample: the primary has not reported this standby in the last 60 seconds.',
                  action:
                    'If the dash stays, check that the primary reports this standby; see [What to do with a problem](/help/cluster#what-to-do).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'slots',
      title: 'Replication slots',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:cluster.tabs.slots}} tab lists the replication slots of the cluster: each one keeps the WAL a consumer still needs. Inactive slots come first, because they are the ones that can hold WAL back. The table shows the newest reading of each slot from the last 5 minutes.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:cluster.slots.colName}}',
              meaning: 'Name of the replication slot, as PostgreSQL shows it.',
            },
            {
              term: '{{ui:cluster.slots.colInstance}}',
              meaning: 'The instance that holds the slot.',
            },
            {
              term: 'Type',
              meaning: 'Slot type as the agent reports it. Unknown when it is missing.',
            },
            {
              term: '{{ui:cluster.slots.filterActive}}',
              meaning:
                '{{ui:cluster.slots.stateActive}} when a consumer is connected to the slot, {{ui:cluster.slots.stateInactive}} when none is, {{ui:cluster.common.unknown}} when it is not reported.',
              action:
                'For an inactive slot, check whether the standby or subscriber behind it is down or disconnected, and reconnect it.',
            },
            {
              term: '{{ui:cluster.slots.filterWal}}',
              meaning:
                'The `wal_status` value reported by PostgreSQL. When it is missing, {{ui:cluster.slots.walRetaining}} is shown for an inactive slot and {{ui:cluster.slots.walStreaming}} for an active one.',
            },
            {
              term: '{{ui:cluster.slots.colRetained}}',
              meaning:
                "WAL kept on disk for the slot, since its restart position. Above 1 GiB the value is highlighted: a disconnected standby retains WAL and can fill the primary's disk.",
              action:
                'Bring the standby back, or check whether the slot is still needed before removing it. Removing a slot cannot be undone.',
            },
            {
              term: '{{ui:cluster.slots.colSafeWal}}',
              meaning:
                'Room left before the slot loses WAL, as set by `max_slot_wal_keep_size`. {{ui:cluster.slots.unlimited}} means no value is reported, as when `max_slot_wal_keep_size` is -1. None: required WAL already lost appears when the slot has lost the WAL it needs.',
            },
            {
              term: '{{ui:cluster.slots.colAge}}',
              meaning:
                'Time since the slot was last active, counted from its last active reading. A dash (—) means the slot is active.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: "Retained WAL can fill the primary's disk. Look first at the {{ui:cluster.slots.colRetained}} of the inactive slots.",
        },
        {
          kind: 'sql',
          label: 'Slots and the WAL each one holds back (run on the primary)',
          code: 'SELECT slot_name, slot_type, active, wal_status, pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained FROM pg_replication_slots;',
        },
      ],
    },
    {
      id: 'drift',
      title: 'Configuration drift',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:cluster.tabs.drift}} tab compares the settings that each instance of the cluster last reported, with one column per instance. A cell is highlighted when its value differs from the reference, which is the value most instances report. Settings expected to differ, such as port and listen_addresses, are left out.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:cluster.drift.colSetting}}',
              meaning: 'Name of the PostgreSQL setting. The search box filters on it.',
            },
            {
              term: 'Instance columns',
              meaning:
                'Each column is one instance, named by its label, and shows the value it last reported. Only instances that reported in the last hour are included.',
            },
            {
              term: 'Highlighted cell',
              meaning:
                'A value different from the reference. On a tie the primary value is the reference; without a primary, the value of the instance with the lowest id.',
              action:
                'Check whether the difference is expected for that instance. If it is not, align the setting on that instance.',
            },
            {
              term: '{{ui:cluster.drift.noneAloneTitle}}',
              meaning:
                'Only one instance of this cluster is monitored, so there is nothing to compare yet.',
              action: 'Monitor a second instance of the cluster to compare its settings.',
            },
            {
              term: '{{ui:cluster.drift.noneTitle}}',
              meaning:
                'No setting differs between the instances of this cluster that reported in the last hour.',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Read one setting on an instance (run on each instance)',
          code: "SELECT name, setting, unit, source FROM pg_settings WHERE name = 'max_connections';",
        },
      ],
    },
    {
      id: 'events',
      title: 'Event timeline',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:cluster.tabs.events}} tab lists the failovers detected in this cluster, newest first, with the old and the new primary. This tab shows failover events only, so its {{ui:cluster.events.typeLabel}} filter holds only that type. For the other event types, open the {{ui:shell.nav.events}} page at `/events`.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:cluster.events.colTime}}',
              meaning:
                'When the event happened, in the console time zone. {{ui:cluster.events.unknownTime}} appears when the time cannot be read.',
            },
            {
              term: '{{ui:cluster.events.colEvent}}',
              meaning: 'Title of the event. A failover is titled Failover detected.',
            },
            {
              term: '{{ui:cluster.events.colSeverity}}',
              meaning:
                'Severity of the event, shown as a pill: critical, warning or info. A failover is a warning.',
            },
            {
              term: '{{ui:cluster.events.colType}}',
              meaning:
                'The raw event name, always shown, so an event the console does not know stays visible.',
            },
            {
              term: '{{ui:cluster.events.colDetails}}',
              meaning:
                'Names the old primary and the new primary, each a link to its instance, followed by what to check. For a failover it reads: Check whether the promotion was planned or caused by a primary failure.',
              action:
                'Check whether the promotion was planned or caused by a primary failure, then confirm in the Topology tab which instance is the primary.',
            },
            {
              term: '{{ui:cluster.events.noMatch}}',
              meaning:
                'Shown when the list is empty. With no failover on record, the tab shows this message too.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'A planned switchover is also recorded as a failover, and the event does not tell the two apart. Check the promotion to know whether it was planned.',
        },
      ],
    },
    {
      id: 'what-to-do',
      title: 'What to do with a problem',
      blocks: [
        {
          kind: 'text',
          text: 'Use this section when the topology, the lag or the slots of a cluster show a problem. Each step names the check to make first. pglens does not change the database: the commands below are for you to review and run yourself.',
        },
        {
          kind: 'steps',
          items: [
            'Open the {{ui:cluster.tabs.topology}} tab and read the banner, if one is shown.',
            'Zero primaries: find out why no instance is primary, and promote a standby if needed.',
            'Two or more primaries: identify the authoritative primary, and demote the other one.',
            'Standby with no upstream, or a dashed arc: check that the standby is streaming from its upstream, then check its agent in [{{ui:shell.nav.fleet}}](/help/fleet).',
            'Lag above 30 seconds: open the {{ui:cluster.tabs.lag}} tab and check whether the lag keeps growing.',
            'Inactive slot with large {{ui:cluster.slots.colRetained}}: check the standby behind it and reconnect it.',
          ],
        },
        {
          kind: 'sql',
          label: 'Role of this instance (run on each instance): false on a primary',
          code: 'SELECT pg_is_in_recovery();',
        },
        {
          kind: 'sql',
          label: 'Streaming status of a standby (run on the standby)',
          code: 'SELECT status, sender_host, sender_port, last_msg_receipt_time FROM pg_stat_wal_receiver;',
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Confirm which instance is authoritative before you promote or demote anything: a wrong choice can discard writes.',
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Troubleshooting',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:cluster.notFound.title}}',
              meaning:
                'No monitored cluster matches the identifier in the address. The page offers a link back to the fleet overview.',
              action:
                'Open the cluster from [{{ui:shell.nav.fleet}}](/help/fleet), and check that the identifier in the address is complete.',
            },
            {
              term: 'Cluster topology is showing the last successful snapshot while the latest refresh failed.',
              meaning:
                'The latest refresh of the topology failed, so the Topology tab shows the last snapshot that loaded. Its values may be old.',
              action:
                'Wait for the next refresh, or reload the page. If it keeps failing, check the server and its logs.',
            },
            {
              term: 'No replication observed for this cluster.',
              meaning: 'The graph has no edge, so no replication link is shown.',
              action:
                'Check that each standby is monitored and reports to the server; see [What to do with a problem](/help/cluster#what-to-do).',
            },
          ],
        },
      ],
    },
  ],
}
