import type { HelpTopic } from '../../types'

export const eventsTopic: HelpTopic = {
  id: 'events',
  group: 'alerting',
  title: 'Events',
  summary:
    'The timeline of the fleet events the server recorded: failovers, agents that stopped or resumed reporting, identity conflicts, counter resets. Each event has a severity and what to check.',
  page: {
    path: '/events',
    label: 'Open {{ui:alerts.section.events}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:alerts.section.events}} page answers one question: what changed in the fleet, and when. The server records an event when it sees a change that matters for operations, such as a new primary or an agent that stops reporting. The page shows these events newest first, next to the alert status, so a change and the alerts it caused can be read together. The time range of the page, kept in the address, selects the events shown; its default comes from the preferences.',
        },
        {
          kind: 'steps',
          items: [
            'Open {{ui:alerts.section.events}} from the {{ui:shell.nav.alerting}} group of the sidebar. It has two tabs: {{ui:alerts.section.events}} and {{ui:alerts.section.alertStatus}}.',
            'Narrow the view with {{ui:alerts.column.cluster}} and {{ui:alerts.events.typeLabel}}, then read the {{ui:alerts.column.severity}} column from the top.',
            'For an event that matters, read its {{ui:alerts.column.details}} and follow the link to the instance in the {{ui:alerts.column.instance}} column.',
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
                  term: '{{ui:alerts.column.cluster}}',
                  meaning:
                    'Shows the events of one cluster. The default, {{ui:alerts.events.allClusters}}, shows every cluster.',
                },
                {
                  term: '{{ui:alerts.events.typeLabel}}',
                  meaning:
                    'Shows one kind of event. The default, {{ui:alerts.events.allTypes}}, shows every kind. The list holds the kinds documented in [{{ui:alerts.events.timelineTitle}}](/help/events#event-kinds).',
                },
                {
                  term: '{{ui:alerts.events.limitLabel}}',
                  meaning:
                    'How many events to load: 100 events (default), 500 events or 1,000 events. The server returns at most 1,000 events.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'timeline',
      title: 'Event timeline',
      blocks: [
        {
          kind: 'text',
          text: 'The timeline lists the events newest first, by time and then by event id. Each row is one event. The {{ui:alerts.column.event}} column names its kind, the {{ui:alerts.column.severity}} column shows its weight, and the {{ui:alerts.column.details}} column says what to check.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.column.time}}',
              meaning: 'When the server recorded the event, in the console time zone.',
            },
            {
              term: '{{ui:alerts.column.event}}',
              meaning:
                'The title of the event kind, such as Failover detected. A kind the console does not document shows Unknown event type.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning:
                'info, warning or critical, set by the event kind. The colour of the badge follows the severity.',
            },
            {
              term: '{{ui:alerts.column.type}}',
              meaning:
                'The raw event type as the server stores it, such as `failover_detected`. Use it to search for the event elsewhere.',
            },
            {
              term: '{{ui:alerts.column.cluster}}',
              meaning: 'The cluster the event belongs to.',
            },
            {
              term: '{{ui:alerts.column.instance}}',
              meaning:
                'The instance concerned, as a link to its page. Unknown when the event names no instance.',
            },
            {
              term: '{{ui:alerts.column.details}}',
              meaning:
                'What to check for this kind of event. Some events add facts: failover shows the Old primary and the New primary; a counter reset shows how many counters reset and in which database; a dropped-series event explains that metrics were dropped.',
            },
          ],
        },
      ],
      subsections: [
        {
          id: 'event-kinds',
          title: 'Event kinds',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Failover detected',
                  meaning:
                    'The primary changed: another instance was promoted. The details name the old and the new primary.',
                  action:
                    'Check whether the promotion was planned or caused by a primary failure. Confirm the roles with the checks in [Read-only checks](/help/events#checks).',
                },
                {
                  term: 'Split brain detected',
                  meaning:
                    'More than one instance of the cluster reports the primary role. Two primaries can accept writes that cannot be merged.',
                  action: 'Identify the authoritative primary and demote the other primary.',
                },
                {
                  term: 'Orphan standby',
                  meaning: 'A standby is not attached to the primary of its cluster.',
                  action:
                    'Check the upstream primary, then promote or reconnect the standby if needed.',
                },
                {
                  term: 'Role changed',
                  meaning: 'An instance changed role, for example from standby to primary.',
                  action: "Check the instance's new role and confirm the change was expected.",
                },
                {
                  term: 'Replication slot inactive',
                  meaning:
                    'A replication slot has no active consumer, and the primary keeps the WAL the slot needs.',
                  action:
                    'Check whether the standby is down or disconnected and needs reconnecting. Run the slot check in [Read-only checks](/help/events#checks).',
                },
                {
                  term: 'Replication slot lost WAL',
                  meaning:
                    'The slot lost WAL that its consumer still needed. The consumer cannot resume from the slot.',
                  action:
                    'Rebuild a physical standby, or recreate a logical slot and resynchronise its subscriber. Then size `max_slot_wal_keep_size` so the slot is not lost again.',
                },
                {
                  term: 'No primary in cluster',
                  meaning: 'No instance of the cluster reports the primary role.',
                  action:
                    'Investigate why no instance is primary, and promote a standby if needed.',
                },
                {
                  term: 'Counter reset detected',
                  meaning:
                    'Cumulative counters went back to zero, for example after `pg_stat_reset()` or an instance restart. The event says how many counters reset and in which database.',
                  action:
                    'Check for `pg_stat_reset()` or an instance restart. This is usually expected.',
                },
                {
                  term: 'Agent down',
                  meaning: 'The agent of an instance has stopped reporting.',
                  action:
                    'Check the agent logs, the container, network reachability and the server.',
                },
                {
                  term: 'Agent recovered',
                  meaning: 'An agent that had stopped reporting is reporting again.',
                  action: 'Confirm the instance operates normally after the reporting gap.',
                },
                {
                  term: 'Duplicate instance suspected',
                  meaning: 'Two agents may be reporting the same instance identity.',
                  action: 'Verify that the agent keeps its `instance_id` in persistent storage.',
                },
                {
                  term: '{{ui:alerts.column.cluster}} identity changed',
                  meaning: 'Critical invariant violation I-1: cluster identity must never change.',
                  action:
                    'Investigate invariant I-1, and review the agent system identifier and the logs.',
                },
                {
                  term: '{{ui:alerts.column.instance}} unreachable',
                  meaning: 'The server stopped receiving reports from the instance.',
                  action: 'Check its agent and the network path.',
                },
                {
                  term: '{{ui:alerts.column.instance}} reachable',
                  meaning: 'Reports from an unreachable instance have resumed.',
                  action: 'Confirm the instance operates normally after the reporting gap.',
                },
                {
                  term: 'Series truncated by cardinality budget',
                  meaning:
                    'Some metrics were dropped because the instance sent more distinct series than its limit.',
                  action:
                    'The details name the check and the database. Reduce the objects it reports, or review its budget.',
                },
                {
                  term: '{{ui:alerts.column.cluster}} identity conflict',
                  meaning:
                    'The instance reported its configured `cluster_name` instead of its system identifier.',
                  action: 'Apply `monitoring_user.sql`, then align `cluster_name`.',
                },
                {
                  term: 'Lock wait captured',
                  meaning:
                    'The agent saw sessions waiting for a lock longer than its watch threshold. The details name the table, the row (`ctid`), the lock mode and, for every session, its user, application, statement and the session blocking it.',
                  action:
                    'Find the root blocker, the session that waits for nobody. End it only if its work can be lost; otherwise wait or fix the application that holds the lock.',
                },
                {
                  term: 'Deadlock captured',
                  meaning:
                    'Sessions waited for each other in a cycle. PostgreSQL cancels one of them after `deadlock_timeout`; the agent captured the cycle before that: tables, rows and statements.',
                  action:
                    'Nothing to end: the deadlock is already broken. Make every transaction lock rows in the same order, for example by primary key.',
                },
                {
                  term: 'Long query captured',
                  meaning:
                    'A statement has run longer than the watch threshold. The details hold the statement, its duration and the session.',
                  action: 'Check its plan in Query plans. Cancel it only if it must not finish.',
                },
                {
                  term: 'Idle in transaction captured',
                  meaning:
                    'A session opened a transaction and then did nothing. It keeps its locks and holds back vacuum.',
                  action:
                    'Fix the application that does not commit, or set `idle_in_transaction_session_timeout`.',
                },
                {
                  term: 'Long transaction captured',
                  meaning:
                    'A transaction has stayed open longer than the watch threshold. The details name the session, the tables it locks and how far it holds back vacuum (xmin age).',
                  action:
                    'Ask the owner of the session to commit, or end it if its work can be lost.',
                },
                {
                  term: 'Unknown event type',
                  meaning:
                    'The kind is not documented in the console. The {{ui:alerts.column.type}} column shows the raw kind.',
                  action: 'Review the raw event type and payload before taking action.',
                },
              ],
            },
          ],
        },
        {
          id: 'checks',
          title: 'Read-only checks',
          blocks: [
            {
              kind: 'text',
              text: 'These statements only read state. Run each one on the instance named in the label.',
            },
            {
              kind: 'sql',
              label: 'Confirm the role of an instance (run on that instance)',
              code: 'SELECT pg_is_in_recovery();',
            },
            {
              kind: 'sql',
              label: 'List the standbys seen by the primary (run on the primary)',
              code: 'SELECT application_name, state, sync_state FROM pg_stat_replication;',
            },
            {
              kind: 'sql',
              label: 'Check the state of replication slots (run on the primary)',
              code: 'SELECT slot_name, active, restart_lsn, wal_status FROM pg_replication_slots;',
            },
          ],
        },
      ],
    },
    {
      id: 'alert-status',
      title: '{{ui:alerts.section.alertStatus}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:alerts.section.alertStatus}} tab lists the alerts loaded for the page, each with its severity, state and suppression. Use it to see at once which alerts are firing and whether they are silenced. It uses the same alert list as the {{ui:alerts.section.alerts}} page, so the 500-alert limit applies here too.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.column.alert}}',
              meaning: 'The alert key: the rule and the object it fires for.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning: 'critical, warning or info, set by the rule.',
            },
            {
              term: '{{ui:alerts.column.state}}',
              meaning: 'firing or resolved, as on the {{ui:alerts.section.alerts}} page.',
            },
            {
              term: '{{ui:alerts.filter.suppression}}',
              meaning:
                '{{ui:alerts.status.suppressed}} when an active silence matches the alert, {{ui:alerts.filter.notSuppressed}} otherwise. Unknown when the silences could not be loaded.',
              action:
                'For a {{ui:alerts.status.suppressed}} alert, read its silence in [{{ui:shell.nav.silences}}](/help/silences#overview) and decide whether it still applies.',
            },
          ],
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
              term: '{{ui:alerts.events.emptyTitle}}',
              meaning:
                'No event matches the selected time range and filters. Widen the range, or clear {{ui:alerts.column.cluster}} and {{ui:alerts.events.typeLabel}}.',
            },
            {
              term: '{{ui:alerts.events.noAlerts}}',
              meaning:
                'The {{ui:alerts.section.alertStatus}} tab has no alerts to show. Check the {{ui:alerts.section.alerts}} page: if it is empty too, no alert has been raised.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'The event list is capped at 1,000 results; older events may exist outside this view. Narrow the time range to reach them.',
        },
      ],
    },
  ],
}
