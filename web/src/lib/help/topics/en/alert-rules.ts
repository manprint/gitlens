import type { HelpTopic } from '../../types'

export const alertRulesTopic: HelpTopic = {
  id: 'alert-rules',
  group: 'alerting',
  title: 'Alert rules',
  summary:
    'Every rule that can raise an alert: what it detects, its default threshold and duration, what to check when it fires, and how an operator can change its threshold, duration, severity and enabled state.',
  page: {
    path: '/alerts/rules',
    label: 'Open Alert rules',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:alerts.rules.title}} page lists every rule that can raise an alert, with its tier, severity, threshold, duration and enabled state. A rule is the condition; an alert is the rule firing for one cluster or instance. There are two tiers. T0 rules are built into the server: they are always on and cannot be edited. T1 rules are seeded by the server with default values and can be edited by an operator. The rules are evaluated every 30 seconds by default.',
        },
        {
          kind: 'steps',
          items: [
            'Open {{ui:alerts.rules.title}} from the {{ui:shell.nav.alerting}} group of the sidebar, or from the page header of [{{ui:shell.nav.alerts}}](/help/alerts).',
            'Find the rule that fired in [{{ui:alerts.rules.configured}}](/help/alert-rules#rules-table) and read its {{ui:alerts.rules.tier}}.',
            'Read its description in [Built-in rules (T0)](/help/alert-rules#built-in-rules) or [Operator-managed rules (T1)](/help/alert-rules#operator-rules), then follow its check.',
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
                  term: '{{ui:alerts.column.severity}}',
                  meaning:
                    'Limits the list to the rules of one severity: critical, warning or info.',
                },
                {
                  term: '{{ui:alerts.rules.enabled}}',
                  meaning: 'Limits the list to rules that are Yes (enabled) or No (disabled).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'rules-table',
      title: '{{ui:alerts.rules.configured}}',
      blocks: [
        {
          kind: 'text',
          text: 'One row per rule. The columns are {{ui:alerts.column.rule}}, {{ui:alerts.rules.tier}}, {{ui:alerts.column.severity}}, {{ui:alerts.rules.threshold}}, {{ui:alerts.rules.duration}}, {{ui:alerts.rules.enabled}} and {{ui:alerts.column.action}}. The page header says the list shows built-in and operator-managed thresholds.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.column.rule}}',
              meaning:
                'The rule id, such as `agent_down` or `disk.free_low`. The id is fixed: it names the condition the server evaluates.',
            },
            {
              term: '{{ui:alerts.rules.tier}}',
              meaning:
                'T0 is a built-in rule, always on and not editable. T1 is an operator-managed rule that can be edited.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning: 'The severity the rule gives to its alerts: critical, warning or info.',
            },
            {
              term: '{{ui:alerts.rules.threshold}}',
              meaning:
                'The condition in words. Examples: "above 30 s" for a lag in seconds; "above 80% max_connections" for connections; "below 15% free" for disk space; "any deadlock" for a deadlock rate; "on event" for event rules, and "no report" for agent_down.',
            },
            {
              term: '{{ui:alerts.rules.duration}}',
              meaning:
                'How long the condition must hold before the alert fires, in seconds. Zero means the alert fires at once.',
            },
            {
              term: '{{ui:alerts.rules.enabled}}',
              meaning: 'Yes or No. A disabled T1 rule is not evaluated. A T0 rule is always on.',
            },
            {
              term: '{{ui:alerts.column.action}}',
              meaning:
                'For a T0 rule the cell reads {{ui:alerts.rules.alwaysOn}}. For a T1 rule it is the button Edit followed by the rule id, which opens the editor.',
            },
          ],
        },
      ],
    },
    {
      id: 'built-in-rules',
      title: 'Built-in rules (T0)',
      blocks: [
        {
          kind: 'text',
          text: 'Built-in rules are always on, and their {{ui:alerts.column.action}} column reads {{ui:alerts.rules.alwaysOn}}. Five rules read a metric and six read an event. An event rule comes in two kinds. A spell, such as `slot_inactive`, stays firing while its condition holds. A pulse, such as `failover_detected`, is one occurrence: it resolves at the first evaluation that finds no new occurrence, and a later occurrence fires it again. A metric rule closes only after 10 minutes without samples, so short gaps in its data do not resolve it.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'agent_down',
              meaning:
                'Default: critical, scope instance. The metric `up` stays below 1 for 90 seconds. No report has arrived from the agent of an instance.',
              action:
                'Check the agent logs, the container, network reachability and the server. The event kind is in [Event kinds](/help/events#event-kinds).',
            },
            {
              term: 'instance_unreachable',
              meaning:
                'Default: critical, scope instance, raised by the event of the same name. The server has had no report from an instance within the expected interval, and `agent_down` is raised with it.',
              action: 'Check the agent and the network path to the instance.',
            },
            {
              term: 'check_failing',
              meaning:
                'Default: warning, scope instance. The metric `pglens_check_error_rate` is above 0 for 5 minutes. A check has been failing for over five minutes.',
              action:
                'Open the instance to see which check fails, and fix the cause it reports, such as a missing privilege on the monitored database.',
            },
            {
              term: 'no_primary_in_cluster',
              meaning:
                'Default: critical, scope cluster, raised by the event of the same name. No instance of the cluster reports the primary role.',
              action: 'Investigate why no instance is primary, and promote a standby if needed.',
            },
            {
              term: 'agent_buffer_full',
              meaning:
                'Default: warning, scope instance. The metric `pglens_samples_dropped_rate` is above 0 for 1 minute. The agent is dropping samples because its buffer is full.',
              action:
                'Check that the agent can reach the server, and that the server accepts its reports.',
            },
            {
              term: 'clock_skew',
              meaning:
                'Default: warning, scope instance. The metric `pglens_agent_clock_skew_seconds` is above 30 for 1 minute. The clock of the agent differs from the server clock by more than 30 seconds.',
              action:
                'Synchronise the clock of the agent host with NTP, then check that the difference is gone.',
            },
            {
              term: 'cardinality_budget_exceeded',
              meaning:
                'Default: warning, scope instance. The metric `pglens_cardinality_truncated_rate` is above 0 for 5 minutes. An instance sent more distinct series than its limit, so some were dropped.',
              action:
                'Reduce the objects the check reports, or review its budget. The event names the check and the database.',
            },
            {
              term: 'failover_detected',
              meaning:
                'Default: critical, scope cluster, raised by the event of the same name. A failover was observed: another instance became the primary.',
              action: 'Check whether the promotion was planned or caused by a primary failure.',
            },
            {
              term: 'split_brain_detected',
              meaning:
                'Default: critical, scope cluster, raised by the event of the same name. More than one instance of the cluster reports the primary role.',
              action: 'Identify the authoritative primary and demote the other primary.',
            },
            {
              term: 'slot_inactive',
              meaning:
                'Default: warning, scope instance, raised by the event of the same name, as a spell. A replication slot is inactive and keeps WAL on the primary.',
              action:
                'Check whether the standby is down or disconnected and needs reconnecting. The slot check is in [Read-only checks](/help/alert-rules#checks).',
            },
            {
              term: 'slot_lost',
              meaning:
                'Default: critical, scope instance, raised by the event of the same name, as a spell. A replication slot lost WAL its consumer still needed.',
              action:
                'Rebuild a physical standby, or recreate a logical slot and resynchronise its subscriber, then size `max_slot_wal_keep_size` so the slot is not lost again.',
            },
          ],
        },
      ],
    },
    {
      id: 'operator-rules',
      title: 'Operator-managed rules (T1)',
      blocks: [
        {
          kind: 'text',
          text: 'T1 rules are seeded by the server with default values, and an operator can edit their threshold, duration, severity and enabled state. The rule id and what it measures stay fixed. The server seeds a rule only when it is missing, so an edit is kept across restarts and upgrades. Each row below gives the default, what the rule means and what to check.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'replica.lag_high',
              meaning:
                'Default: warning, scope instance. Lag above 30 seconds for 120 seconds. A standby is more than 30 seconds behind its primary.',
              action:
                'Measure the replay lag on the standby with the check in [Read-only checks](/help/alert-rules#checks). Then look at the network between the nodes and at long queries on the standby, which can hold back replay.',
            },
            {
              term: 'replica.all_standbys_lagging',
              meaning:
                'Default: critical, scope cluster. Lag above 30 seconds for 120 seconds. Every standby of the cluster is more than 30 seconds behind.',
              action:
                'Lag on every standby points to the primary or to the network, not to one standby. Check the write load on the primary and the network path to the standbys.',
            },
            {
              term: 'replica.no_sync_standby',
              meaning:
                'Default: critical, scope cluster. Fewer than 1 synchronous standby for 60 seconds. The cluster has no synchronous standby.',
              action:
                'Check `synchronous_standby_names` on the primary and the `sync_state` of the standbys with the checks in [Read-only checks](/help/alert-rules#checks).',
            },
            {
              term: 'conn.near_max',
              meaning:
                'Default: warning, scope instance. Connections above 80% of `max_connections` for 120 seconds. More than 80% of the connections allowed by `max_connections` are in use.',
              action:
                'Find which clients hold the connections with the check in [Read-only checks](/help/alert-rules#checks). Reduce the pool size of those clients first; raise `max_connections` only if the server memory allows it.',
            },
            {
              term: 'conn.idle_in_transaction',
              meaning:
                'Default: warning, scope instance. A session idle in a transaction for over 300 seconds, for 120 seconds. A session has been idle in a transaction for over 5 minutes.',
              action:
                'Find the sessions with the check in [Read-only checks](/help/alert-rules#checks), and ask the owner of the application to commit or roll back. Terminate a session only after checking it.',
            },
            {
              term: 'txn.long_running',
              meaning:
                'Default: warning, scope instance. A transaction open for over 900 seconds, for 120 seconds. A transaction has been open for a long time.',
              action:
                'Find the transaction with the check in [Read-only checks](/help/alert-rules#checks). A long transaction holds back vacuum, so ask its owner to finish it.',
            },
            {
              term: 'txn.wraparound_risk',
              meaning:
                'Default: critical, scope instance. The age of the oldest transaction ID above 1,000,000,000 for 300 seconds. The oldest transaction ID in a database is getting old: it needs a freezing vacuum.',
              action:
                'Find the database with the check in [Read-only checks](/help/alert-rules#checks), remove the blockers (long transactions, idle sessions), then run `VACUUM (FREEZE)` on that database in a maintenance window.',
            },
            {
              term: 'txn.multixact_wraparound_risk',
              meaning:
                'Default: critical, scope instance. The age of the oldest multixact ID above 1,000,000,000 for 300 seconds. The oldest multixact ID in a database is getting old: it needs a freezing vacuum.',
              action:
                'Find the database and the tables with the check in [Read-only checks](/help/alert-rules#checks), remove the blockers (long transactions, idle sessions), then run `VACUUM (FREEZE)` on those tables in a maintenance window.',
            },
            {
              term: 'db.deadlock_rate',
              meaning:
                'Default: warning, scope instance. Any deadlock, so the threshold is 0 and the duration is 0 seconds. Deadlocks are occurring.',
              action:
                'Read the deadlock report in the server log, which names the statements involved, and make the transactions take their locks in the same order.',
            },
            {
              term: 'archive.failing',
              meaning:
                'Default: critical, scope instance. The last archive attempt failed, for 300 seconds. WAL archiving is failing: finished WAL files are not being copied.',
              action:
                'Check `archive_command` and the archive destination with the archiver check in [Read-only checks](/help/alert-rules#checks). Fix the cause before the WAL directory grows.',
            },
            {
              term: 'disk.free_low',
              meaning:
                'Default: critical, scope instance. Free space on the data disk below 15% for 300 seconds. Less than 15% of the data disk is free.',
              action:
                'Find what grows, such as tables, WAL or logs, then free space or extend the disk. The check in [Read-only checks](/help/alert-rules#checks) shows where the data directory is.',
            },
            {
              term: 'replica.redundancy_reduced',
              meaning:
                'Default: warning, scope cluster. More than 0 missing standbys for 60 seconds. Fewer standbys stream than the cluster has members: a standby is down, disconnected or not yet rejoined; a member silent for over an hour is no longer counted.',
              action:
                'Find the standby that does not stream. Check whether it is down, disconnected, or has not rejoined after a failover.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Each check below only reads state, except that terminating a session in the idle-in-transaction row is a separate, deliberate action.',
        },
      ],
      subsections: [
        {
          id: 'checks',
          title: 'Read-only checks',
          blocks: [
            {
              kind: 'text',
              text: 'These read-only statements support the rules above. Run each one on the instance named in its label.',
            },
            {
              kind: 'sql',
              label: 'Replay lag on a standby (run on the standby)',
              code: 'SELECT now() - pg_last_xact_replay_timestamp() AS replay_lag;',
            },
            {
              kind: 'sql',
              label: 'Standbys and their state (run on the primary)',
              code: 'SELECT application_name, state, sync_state, replay_lag FROM pg_stat_replication;',
            },
            {
              kind: 'sql',
              label: 'Synchronous standby setting (run on the primary)',
              code: 'SHOW synchronous_standby_names;',
            },
            {
              kind: 'sql',
              label: 'Connections in use against the limit',
              code: "SELECT count(*) AS connections, current_setting('max_connections') AS max_connections FROM pg_stat_activity;",
            },
            {
              kind: 'sql',
              label: 'Sessions idle in a transaction',
              code: "SELECT pid, usename, datname, now() - state_change AS idle_for FROM pg_stat_activity WHERE state = 'idle in transaction' ORDER BY idle_for DESC;",
            },
            {
              kind: 'sql',
              label: 'Oldest open transactions',
              code: 'SELECT pid, usename, state, now() - xact_start AS xact_age FROM pg_stat_activity WHERE xact_start IS NOT NULL ORDER BY xact_start LIMIT 10;',
            },
            {
              kind: 'sql',
              label: 'Age of the oldest transaction ID per database',
              code: 'SELECT datname, age(datfrozenxid) AS xid_age FROM pg_database ORDER BY xid_age DESC;',
            },
            {
              kind: 'sql',
              label: 'Deadlocks per database',
              code: 'SELECT datname, deadlocks FROM pg_stat_database WHERE deadlocks > 0;',
            },
            {
              kind: 'sql',
              label: 'Archiver status',
              code: 'SELECT archived_count, last_archived_wal, failed_count, last_failed_wal, last_failed_time FROM pg_stat_archiver;',
            },
            {
              kind: 'sql',
              label: 'Data directory',
              code: 'SHOW data_directory;',
            },
          ],
        },
      ],
    },
    {
      id: 'editing',
      title: 'Editing a rule',
      blocks: [
        {
          kind: 'text',
          text: 'Only T1 rules can be edited. The {{ui:alerts.column.action}} cell of a T1 row reads Edit followed by the rule id. The editor opens above the list with the rule current values, and {{ui:alerts.ruleEditor.cancel}} closes it without saving. The editor changes four fields: {{ui:alerts.rules.threshold}}, {{ui:alerts.rules.duration}}, {{ui:alerts.column.severity}} and {{ui:alerts.rules.enabled}}.',
        },
        {
          kind: 'steps',
          items: [
            'Find the rule in [{{ui:alerts.rules.configured}}](/help/alert-rules#rules-table) and check that its {{ui:alerts.rules.tier}} is T1.',
            'Click Edit followed by the rule id in the {{ui:alerts.column.action}} column.',
            'Change {{ui:alerts.rules.threshold}}, {{ui:alerts.ruleEditor.duration}}, {{ui:alerts.column.severity}} or {{ui:alerts.rules.enabled}}.',
            'Click {{ui:alerts.ruleEditor.save}}. A confirmation names the rule and its new threshold, duration and severity.',
            'To leave without saving, click {{ui:alerts.ruleEditor.cancel}}.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.rules.threshold}}',
              meaning:
                'The value the metric is compared with. It must be a finite number, so enter a plain number.',
            },
            {
              term: '{{ui:alerts.ruleEditor.duration}}',
              meaning:
                'How long the condition must hold before the alert fires, in whole seconds from 0 to 2147483647.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning: 'The severity the rule gives to its alerts: critical, warning or info.',
            },
            {
              term: '{{ui:alerts.rules.enabled}}',
              meaning:
                'Yes or No. A disabled T1 rule is not evaluated, so it raises no new alerts.',
            },
            {
              term: '{{ui:alerts.ruleEditor.thresholdInvalid}}',
              meaning:
                'The {{ui:alerts.rules.threshold}} field holds a value that is not a number, or is infinite. Enter a finite number.',
            },
            {
              term: '{{ui:alerts.ruleEditor.durationInvalid}}',
              meaning:
                'The {{ui:alerts.rules.duration}} field holds a value that is not a whole number of seconds in that range. Enter a whole number.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'A rule applies to the whole fleet, not to one instance. A change to a threshold or to {{ui:alerts.rules.enabled}} alters what fires for every instance the rule covers.',
        },
      ],
    },
    {
      id: 'notification-channels',
      title: '{{ui:settings.notifications.listLabel}}',
      blocks: [
        {
          kind: 'text',
          text: 'A rule decides when an alert fires; it does not choose where the notification goes. Channels are set on the server for the whole installation: see [{{ui:alerts.section.delivery}}](/help/alerts#delivery). [{{ui:alerts.delivery.settingsLink}}](/settings?tab=notifications) shows which channels are configured and sends a test message.',
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
              term: '{{ui:alerts.rules.emptyDescription}}',
              meaning:
                'The server returned no rules. Reload the page; if the list stays empty, check the server log.',
            },
            {
              term: '{{ui:alerts.rules.alwaysOn}}',
              meaning: 'The rule is a T0 built-in rule. It is always on and cannot be edited.',
            },
            {
              term: 'An edit does not change the alert',
              meaning:
                'Rules are evaluated periodically, every 30 seconds by default. Wait for one evaluation, then reload the {{ui:shell.nav.alerts}} page. If nothing changes, check that the rule is {{ui:alerts.rules.enabled}}.',
            },
          ],
        },
      ],
    },
  ],
}
