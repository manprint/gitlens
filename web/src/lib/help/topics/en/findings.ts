import type { HelpTopic } from '../../types'

export const findingsTopic: HelpTopic = {
  id: 'findings',
  group: 'monitoring',
  title: 'Findings',
  summary:
    'The advisor findings for the instances and clusters: each problem a rule finds, with its severity, state and remediation to review. The {{ui:findings.ruleCatalogue.title}} lists every rule.',
  page: {
    path: '/findings',
    label: 'Open {{ui:findings.advisorFindings}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'The advisor checks the monitored instances and clusters against a set of rules. Each problem it finds is a finding: a rule, an object, a severity and a state.',
        },
        {
          kind: 'text',
          text: 'The page lists the findings of all instances and clusters. The default view is {{ui:findings.page.stateActive}}, which hides muted and resolved findings.',
        },
        {
          kind: 'steps',
          items: [
            'Read the summary buttons at the top: {{ui:findings.page.severityCritical}}, {{ui:findings.page.severityWarning}}, {{ui:findings.page.severityInfo}} and {{ui:findings.page.notEvaluated}}. Click one to filter the table.',
            'Narrow the list with {{ui:findings.page.state}}, {{ui:findings.table.severity}}, {{ui:findings.page.scope}}, {{ui:findings.page.cluster}}, {{ui:findings.page.instance}}, or the {{ui:findings.page.searchLabel}} box.',
            'Open the details of a row to read its finding card. See [Finding card](/help/findings#finding-card).',
            'To silence a finding for a set time, use {{ui:findings.mute.mute}}. See [Mute dialog](/help/findings#mute-dialog).',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'The remediation is text for the operator to review. pglens shows it and does not run it.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'A rule that could not be evaluated is not a healthy “no findings” result. Its problem is unknown until the rule can run.',
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
                  term: 'Findings and {{ui:findings.ruleCatalogue.title}}',
                  meaning:
                    'Two tabs: Findings lists the findings; {{ui:findings.ruleCatalogue.title}} lists every advisor rule. See [{{ui:findings.ruleCatalogue.title}}](/help/findings#rule-catalogue).',
                },
                {
                  term: 'Summary buttons',
                  meaning:
                    '{{ui:findings.page.severityCritical}}, {{ui:findings.page.severityWarning}} and {{ui:findings.page.severityInfo}} filter the table by severity. {{ui:findings.page.notEvaluated}} sets {{ui:findings.page.state}} to {{ui:findings.page.stateDegraded}} and clears {{ui:findings.table.severity}}.',
                },
                {
                  term: '{{ui:findings.page.state}}',
                  meaning:
                    'Filters the findings by state: {{ui:findings.page.stateActive}} (the default), {{ui:findings.page.allStates}}, Open, Degraded, Muted or Resolved. See [States](/help/findings#states).',
                },
                {
                  term: '{{ui:findings.table.severity}}',
                  meaning:
                    'Filters by severity: {{ui:findings.ruleCatalogue.allSeverities}}, Critical, Warning or Info. See [Severities](/help/findings#severities).',
                },
                {
                  term: '{{ui:findings.page.scope}}',
                  meaning:
                    'Filters by scope: {{ui:findings.ruleCatalogue.allScopes}}, Cluster or Instance.',
                },
                {
                  term: '{{ui:findings.page.cluster}}',
                  meaning: '{{ui:findings.page.allClusters}}, or one cluster.',
                },
                {
                  term: '{{ui:findings.page.instance}}',
                  meaning: '{{ui:findings.page.allInstances}}, or one instance.',
                },
                {
                  term: '{{ui:findings.page.searchLabel}}',
                  meaning:
                    'Free-text search. The placeholder reads {{ui:findings.page.searchPlaceholder}}. The empty state lists the searched fields: rule, title, detail, remediation, object, database, mute reason and evidence.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'Only the first 1000 findings are loaded. When the limit is reached, the page says: “Only the first 1000 findings were returned. {{ui:findings.page.stateActive}} findings come first, so muted and resolved findings may be missing from the counts and the list.”',
            },
            {
              kind: 'text',
              text: 'The results line above the table can end with “N muted and M resolved hidden.”, counting the findings the current view hides.',
            },
          ],
        },
      ],
    },
    {
      id: 'severities',
      title: 'Severities',
      blocks: [
        {
          kind: 'text',
          text: 'Every rule has a severity. The {{ui:findings.table.severity}} column and the {{ui:findings.table.severity}} filter use {{ui:findings.page.severityCritical}}, {{ui:findings.page.severityWarning}} and {{ui:findings.page.severityInfo}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.page.severityCritical}}',
              meaning:
                'The most serious findings, such as `config.fsync_off`, `archive.failing` and `txn.wraparound_risk`.',
            },
            {
              term: '{{ui:findings.page.severityWarning}}',
              meaning:
                'A problem that needs review, such as `table.dead_tuples_high` and `index.unused`.',
            },
            {
              term: '{{ui:findings.page.severityInfo}}',
              meaning:
                'A finding to know about, with a lower priority, such as `archive.disabled` and `table.seq_scan_heavy`.',
            },
            {
              term: '{{ui:findings.page.notEvaluated}}',
              meaning:
                'The summary button. Counts degraded findings, not rules: a rule that could not be evaluated for an instance. Muted and resolved findings are not counted.',
            },
          ],
        },
        {
          kind: 'text',
          text: 'Some rules set the severity by size, rows or age: `index.fk_unindexed` is Info below 10 MiB of table size and Warning from 10 MiB; `postgis.spatial_unindexed` is Info up to 10,000 rows and Warning above; `partition.future_missing` is Warning, or Critical when inserts already fail; `function.secdef_no_search_path` is Warning, or Critical when the owner is a superuser and `PUBLIC` has `EXECUTE`; `sequence.exhaustion_risk` is Warning from 75% of the range and Critical from 90%.',
        },
      ],
    },
    {
      id: 'states',
      title: 'States',
      blocks: [
        {
          kind: 'text',
          text: 'Each finding has one state. The State filter and the State column use these values.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.page.stateOpen}}',
              meaning: 'The problem is present now.',
            },
            {
              term: '{{ui:findings.page.stateDegraded}}',
              meaning:
                'The rule could not be evaluated for an instance. Degraded findings are counted under {{ui:findings.page.notEvaluated}}.',
              action:
                'Read the rule’s row in the {{ui:findings.ruleCatalogue.title}}: {{ui:findings.ruleCatalogue.cannotEvaluate}} lists the instances where it cannot run.',
            },
            {
              term: '{{ui:findings.page.stateMuted}}',
              meaning:
                'You silenced the finding for a time. See [Mute dialog](/help/findings#mute-dialog).',
              action: 'Mute it again with a reason if it is still wanted, or fix the problem.',
            },
            {
              term: '{{ui:findings.page.stateResolved}}',
              meaning: 'The problem is gone.',
            },
          ],
        },
      ],
    },
    {
      id: 'findings-table',
      title: '{{ui:findings.page.tableHelp}}',
      blocks: [
        {
          kind: 'text',
          text: 'One row per finding, with four columns. {{ui:findings.table.severity}}, {{ui:findings.table.finding}} and {{ui:findings.table.lastSeen}} can sort the table. Nothing is cut: a long title wraps, and the statement text shows on two lines, in full in the card.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.table.severity}}',
              meaning:
                'Critical, Warning or Info, set by the rule. See [Severities](/help/findings#severities). A state other than open shows under it: muted, degraded or resolved. See [States](/help/findings#states).',
            },
            {
              term: '{{ui:findings.table.finding}}',
              meaning: 'The title of the finding, then in grey what the rule checks.',
            },
            {
              term: '{{ui:findings.table.object}}',
              meaning:
                'What the finding is about: the text of the statement, or the table, index, setting, subscription or replication slot by name; never a bare id. The grey line under it names the database, instance and cluster. A statement whose text the server no longer keeps reads “text no longer kept”.',
            },
            {
              term: '{{ui:findings.table.lastSeen}}',
              meaning: 'The last time the advisor saw the finding, in the console time zone.',
            },
          ],
        },
      ],
    },
    {
      id: 'finding-card',
      title: 'Finding card',
      blocks: [
        {
          kind: 'text',
          text: 'Open the details of a row to show its finding card: the severity, state and scope, the full title, what the rule checks and its id, then the detail, the statement, where it was found, what to do, how to verify it on the database and the evidence. A muted finding shows a Reason line.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.card.statement}}',
              meaning:
                'For a finding on a query: its full text with SQL highlighting, a copy button, the exact query id and {{ui:findings.card.openQuery}}, which opens the query in Query performance. A text that ends with … was cut by PostgreSQL at `track_activity_query_size` bytes.',
            },
            {
              term: '{{ui:findings.card.affectedObject}}',
              meaning:
                'For any other finding, the object it is about: a table, an index, a setting, a subscription or a replication slot. {{ui:findings.card.wholeInstance}} when the rule covers all of it.',
            },
            {
              term: '{{ui:findings.card.where}}',
              meaning: 'The database, instance and cluster, by name.',
            },
            {
              term: '{{ui:findings.card.firstSeen}}',
              meaning: 'When the advisor first saw the finding.',
            },
            {
              term: '{{ui:findings.table.lastSeen}}',
              meaning: 'When the advisor last saw the finding.',
            },
            {
              term: '{{ui:findings.card.whatToDo}}',
              meaning:
                'The suggested action, when the finding has one. Each rule’s remediation is listed under Advisor rules below.',
            },
            {
              term: '{{ui:findings.card.verifyTitle}}',
              meaning:
                'The steps and the SQL to check the finding yourself: the database to connect to, what to look at and a script with a copy button. The script opens a read-only transaction and ends with `ROLLBACK`: it reads catalogs and statistics views and changes nothing. It reads the statistics of now, not of the moment of the finding.',
            },
            {
              term: '{{ui:findings.mute.reason}}',
              meaning:
                'For a muted finding, the reason given when it was muted. When none is stored, the card reads “{{ui:findings.card.noReason}}”',
            },
            {
              term: '{{ui:findings.card.catalogue}}',
              meaning:
                '{{ui:findings.card.metadataUnavailable}} appears when the finding has no matching entry in the {{ui:findings.ruleCatalogue.title}}.',
            },
            {
              term: '{{ui:findings.card.evidence}}',
              meaning:
                'The values the rule read, with their units: times, sizes, percentages and dates in the console time zone. Hover a label to see the key the advisor wrote. {{ui:findings.card.noEvidence}} appears when the rule reports none.',
            },
          ],
        },
      ],
    },
    {
      id: 'mute-dialog',
      title: 'Mute dialog',
      blocks: [
        {
          kind: 'text',
          text: 'Muting hides a finding from the default view until the expiry you choose. When the mute expires and the condition still holds, the finding returns to open.',
        },
        {
          kind: 'steps',
          items: [
            'Choose {{ui:findings.mute.mute}} on the row. The {{ui:findings.mute.mute}} dialog opens.',
            'Enter the reason in {{ui:findings.mute.reason}}. {{ui:findings.mute.reasonRequired}}',
            'Choose the expiry: {{ui:findings.mute.preset1h}}, {{ui:findings.mute.preset8h}}, {{ui:findings.mute.preset24h}} (the default), {{ui:findings.mute.preset7d}}, or {{ui:findings.mute.presetCustom}}.',
            'For {{ui:findings.mute.presetCustom}}, set the date and time in {{ui:findings.mute.until}}. A mute can last up to 90 days.',
            'Confirm with {{ui:findings.mute.mute}}. Use {{ui:findings.mute.unmute}} to lift a mute.',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'A mute hides a problem that is still present. Give a clear reason, and the shortest expiry that fits.',
        },
      ],
    },
    {
      id: 'rule-catalogue',
      title: '{{ui:findings.ruleCatalogue.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:findings.ruleCatalogue.title}} tab lists every advisor rule with what it checks and what it needs. Read it before you mute the findings of a rule, or when a rule shows instances in {{ui:findings.ruleCatalogue.cannotEvaluate}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.ruleCatalogue.rule}}',
              meaning: 'The rule, with its id and its description.',
            },
            {
              term: '{{ui:findings.table.severity}}',
              meaning: 'The severity of the rule’s findings.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.scope}}',
              meaning: 'What the rule’s findings cover.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.needs}}',
              meaning: 'The inputs the rule depends on.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.minTier}}',
              meaning:
                'Rule minimum tier: T0 pg_monitor and pg_control_system(), T1 adds pg_read_all_data, T2 adds pg_signal_backend.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.firingInstances}}',
              meaning: 'The instances where the rule currently fires.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.cannotEvaluate}}',
              meaning: 'The instances where the rule cannot be evaluated.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.severityFilter}}',
              meaning: 'Filters the rules by their level.',
            },
            {
              term: '{{ui:findings.ruleCatalogue.scopeFilter}}',
              meaning: 'Filters the rules by their scope.',
            },
          ],
        },
      ],
    },
    {
      id: 'advisor-rules',
      title: 'Advisor rules',
      blocks: [
        {
          kind: 'text',
          text: 'The rules are grouped by area. Each entry gives the rule id, its meaning, the threshold where the code sets one, its severity, and the remediation the finding suggests.',
        },
      ],
      subsections: [
        {
          id: 'rules-config',
          title: 'Configuration and memory',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`config.checkpoints_too_frequent`',
                  meaning:
                    'More than 20% of the checkpoints are requested before the scheduled time, over the last 24 hours. Severity: Warning.',
                  action: 'Increase `max_wal_size` after sizing storage.',
                },
                {
                  term: '`config.drift`',
                  meaning:
                    'The same setting has different values on instances of this cluster. Severity: Warning.',
                  action:
                    'Reconcile shared settings while retaining intentional per-instance values.',
                },
                {
                  term: '`config.effective_cache_size_mismatch`',
                  meaning:
                    '`effective_cache_size` is far from the memory the server has. Threshold: below 25% or above 150% of the usable memory. Severity: Info.',
                  action: 'Align the estimate with usable memory and workload.',
                },
                {
                  term: '`config.fsync_off`',
                  meaning: '`fsync` is off: a crash can corrupt data. Severity: Critical.',
                  action: 'Enable `fsync` in production to preserve durability.',
                },
                {
                  term: '`config.full_page_writes_off`',
                  meaning:
                    '`full_page_writes` is off: a crash can leave torn pages and corrupt data. Severity: Critical.',
                  action: 'Enable `full_page_writes` unless storage guarantees replacement.',
                },
                {
                  term: '`config.maintenance_work_mem_low`',
                  meaning:
                    '`maintenance_work_mem` is below 256 MiB on a host with a lot of memory. Threshold: more than 8 GiB of memory and less than 256 MiB. Severity: Info.',
                  action: 'Review maintenance memory for vacuum and index builds.',
                },
                {
                  term: '`config.max_connections_high`',
                  meaning:
                    '`max_connections` is much higher than the connections actually in use. Threshold: above 200, with less than 30% of the connections in use. Severity: Warning.',
                  action: 'Use a pooler, or reduce `max_connections` after workload review.',
                },
                {
                  term: '`config.shared_buffers_high`',
                  meaning:
                    '`shared_buffers` is more than 40% of the usable memory. Severity: Warning.',
                  action: 'Leave memory for connections and the operating system.',
                },
                {
                  term: '`config.shared_buffers_low`',
                  meaning:
                    '`shared_buffers` is small for the memory the server has. Threshold: below 15% of the usable memory. Severity: Warning.',
                  action: 'Review `shared_buffers` for the workload.',
                },
                {
                  term: '`config.track_io_timing_off`',
                  meaning:
                    '`track_io_timing` is off, so read and write times are not measured. Severity: Info.',
                  action: 'Enable it when I/O diagnosis is required.',
                },
                {
                  term: '`config.work_mem_low`',
                  meaning:
                    '`work_mem` is small and statements write temporary files. Threshold: `work_mem` below 4 MiB, while the `query.temp_bytes_high` rule has findings. Severity: Info.',
                  action: 'Review `work_mem` with concurrent memory usage.',
                },
                {
                  term: '`config.work_mem_oversized`',
                  meaning:
                    '`work_mem` times `max_connections` is a large share of the memory. Threshold: above 25% of the usable memory. Severity: Warning.',
                  action: 'Reduce `work_mem` or cap connections to protect memory.',
                },
                {
                  term: '`conn.idle_share_high`',
                  meaning:
                    'A large share of the connections is idle. Threshold: more than 70% idle, with more than 100 client connections. Severity: Info.',
                  action: 'Use pooling and review client connection lifetimes.',
                },
                {
                  term: '`conn.saturation`',
                  meaning:
                    'Most of the connections allowed by `max_connections` are in use. Threshold: above 80%. Severity: Warning.',
                  action: 'Add capacity or use a pooler.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-backup',
          title: 'Backup and WAL archiving',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`archive.disabled`',
                  meaning:
                    'WAL archiving is turned off, so backups cannot be restored to a point in time. Severity: Info.',
                  action: 'Confirm an alternate backup strategy.',
                },
                {
                  term: '`archive.failing`',
                  meaning:
                    'WAL archiving is failing: finished WAL files are not being copied. Severity: Critical.',
                  action: 'Repair `archive_command` and verify WAL retention.',
                },
                {
                  term: '`archive.stalled`',
                  meaning:
                    'No WAL file has been archived for a long time although the server writes WAL. Threshold: the last archived WAL is older than one hour, or ten times `archive_timeout` when that is longer, unless the archiver reports no segment waiting. Severity: Critical.',
                  action: 'Repair archiving before WAL retention is exhausted.',
                },
                {
                  term: '`backup.no_basebackup_seen`',
                  meaning:
                    'No base backup has been seen for this cluster. Threshold: the last base backup seen is at least 7 days old, or none was seen during monitoring. Severity: Warning.',
                  action:
                    'Establish and verify a regular base backup schedule. Backups copied outside the replication protocol (file-level tools, storage snapshots) are not visible here.',
                },
                {
                  term: '`backup.no_strategy`',
                  meaning:
                    'No backup strategy is visible: WAL is not archived and no base backup was seen. Threshold: no base backup seen in the last 30 days, and no member of the cluster archives WAL. Severity: Critical.',
                  action: 'Configure archiving or establish a verified base backup strategy.',
                },
                {
                  term: '`config.wal_keep_size_low`',
                  meaning:
                    '`wal_keep_size` may be too small to keep WAL for a standby that falls behind. Threshold: a standby lags more than `wal_keep_size` and more than one WAL segment. Severity: Warning.',
                  action: 'Increase `wal_keep_size` or configure a replication slot.',
                },
                {
                  term: '`wal.directory_large`',
                  meaning:
                    'The WAL directory is larger than the WAL settings explain. Threshold: `pg_wal` above twice `max_wal_size` plus `wal_keep_size`. Severity: Warning.',
                  action:
                    'Look at the WAL each replication slot retains (`pg_replication_slots`), and drop with `pg_drop_replication_slot()` the slots no consumer uses. Fix `archive_command` if segments wait for it, which `pg_stat_archiver.last_failed_wal` shows.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-tables',
          title: 'Tables, vacuum and bloat',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`bloat.estimate_blind`',
                  meaning:
                    'The wasted-space estimate cannot be computed for some tables or indexes.',
                  action:
                    'Grant the monitoring role `SELECT` on the columns of these tables and row access that row security does not hide, or accept that they are not estimated.',
                },
                {
                  term: '`table.autoanalyze_stale`',
                  meaning:
                    'More than 20% of the rows of a table changed since it was last analyzed. Severity: Info.',
                  action: 'Analyze the table and review the autoanalyze settings.',
                },
                {
                  term: '`table.bloat_high`',
                  meaning:
                    'A table holds a lot of space that a rebuild would give back. Threshold: bloat ratio above 30% and size above 100 MiB. Severity: Warning.',
                  action: 'Schedule maintenance after confirming the workload impact.',
                },
                {
                  term: '`table.dead_tuples_high`',
                  meaning:
                    'A table has many dead rows that vacuum has not yet cleared. Threshold: dead rows above 20% of live rows, and at least 10,000 dead rows. Severity: Warning.',
                  action: 'Investigate autovacuum, and vacuum this table.',
                },
                {
                  term: '`table.never_autovacuumed`',
                  meaning:
                    'A large table has never been vacuumed. Threshold: more than 100,000 live rows, and no vacuum or autovacuum timestamp. The rule is not evaluated on a standby. Severity: Warning.',
                  action: 'Check the autovacuum coverage for this table.',
                },
                {
                  term: '`table.seq_scan_heavy`',
                  meaning:
                    'A large table is mostly read in full, not through an index. Threshold: more than 100,000 live rows, and an index scan rate at most 10% of the sequential scan rate. A missing-index candidate, not a certainty. Severity: Info.',
                  action: 'Review the query predicates and the execution plans.',
                },
                {
                  term: '`table.wraparound_risk`',
                  meaning:
                    'A table holds old transaction IDs, so it needs a freezing vacuum. Threshold: `relfrozenxid` age above 1,000,000,000. Severity: Critical.',
                  action: 'Vacuum this relation immediately.',
                },
                {
                  term: '`vacuum.disabled`',
                  meaning: 'Automatic vacuum (autovacuum) is turned off. Severity: Critical.',
                  action: 'Enable autovacuum to prevent table and transaction ID bloat.',
                },
                {
                  term: '`vacuum.starvation`',
                  meaning:
                    'Many tables have dead rows while every vacuum worker is busy. Threshold: more than 10 tables with dead rows above 20% of their live rows, and all autovacuum workers busy. Severity: Critical.',
                  action: 'Investigate blocked or undersized autovacuum workers.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-indexes',
          title: 'Indexes',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`index.bloat_high`',
                  meaning:
                    'An index holds a lot of space that a rebuild would give back. Threshold: bloat ratio above 40% and size above 50 MiB. Severity: Warning.',
                  action: 'Schedule maintenance after confirming the workload impact.',
                },
                {
                  term: '`index.divergence`',
                  meaning:
                    'The same table has different indexes on instances of this cluster. Severity: Warning.',
                  action: 'Reconcile index definitions after confirming the replication state.',
                },
                {
                  term: '`index.duplicate`',
                  meaning: 'Two or more indexes have the same definition. Severity: Warning.',
                  action: 'Drop the non-primary, non-unique duplicate after validation.',
                },
                {
                  term: '`index.fk_unindexed`',
                  meaning:
                    'A foreign key has no index on its columns. Severity: Info below 10 MiB of table size, Warning from 10 MiB.',
                  action:
                    'If rows of the referenced table are deleted, or their keys updated, create the index with `CREATE INDEX CONCURRENTLY`. If they never are, the index is not needed.',
                },
                {
                  term: '`index.invalid`',
                  meaning:
                    'An index is marked invalid, so queries do not use it. Severity: Critical.',
                  action: 'Rebuild or drop the invalid index.',
                },
                {
                  term: '`index.redundant_prefix`',
                  meaning: 'An index repeats the leading columns of another index. Severity: Info.',
                  action: 'Review whether the shorter non-unique index can be removed.',
                },
                {
                  term: '`index.unused`',
                  meaning:
                    'An index has had no scans in the observed history. Threshold: no scan within the 7-day default window. Only the primary evaluates it, and only indexes above 10 MiB that are neither primary keys nor unique. Severity: Warning.',
                  action:
                    'Confirm the workload intent, including queries served by standbys, before dropping this candidate. See [Index analysis](/help/indexes).',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-statements',
          title: 'Statements, transactions and locks',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`lock.long_wait`',
                  meaning:
                    'A session has waited a long time for a lock. Threshold: 300 seconds (5 minutes). Severity: Warning.',
                  action:
                    'Open the Locks page and look at the session named in the finding: its state and transaction age show whether it is idle in transaction or still working. If its transaction can end, ask its owner to end it. If the work can be lost, cancel or terminate it there, and the waiting session proceeds.',
                },
                {
                  term: '`query.cache_miss_high`',
                  meaning:
                    'A frequent statement reads most of its pages from disk, not from memory. Threshold: more than 1,000 calls, and a miss ratio above 30%. Severity: Info.',
                  action:
                    'Review the working-set size and the indexing. See [Query performance](/help/query-performance).',
                },
                {
                  term: '`query.regression`',
                  meaning:
                    'A statement is slower than its own baseline. Threshold: mean time at least three times the baseline, and at least 100 ms. Severity: Critical.',
                  action:
                    'Investigate the query plan regression. See [Query performance](/help/query-performance).',
                },
                {
                  term: '`query.slow_mean`',
                  meaning:
                    'A statement has a high mean execution time. Threshold: mean above 500 ms, with at least 10 calls. Severity: Warning.',
                  action: 'Tune or index the query. See [Slowest queries](/help/slowest-queries).',
                },
                {
                  term: '`query.temp_bytes_high`',
                  meaning:
                    'A statement writes a lot of temporary files. Threshold: average temporary bytes per call above `work_mem`. Severity: Warning.',
                  action: 'Review the query and the current `work_mem`.',
                },
                {
                  term: '`query.total_time_share`',
                  meaning:
                    'One statement takes a large share of the total execution time. Threshold: more than 30% of the total. Severity: Warning.',
                  action:
                    'Optimize the dominant query. See [Slowest queries](/help/slowest-queries).',
                },
                {
                  term: '`txn.high_rollback_ratio`',
                  meaning:
                    'A large share of transactions end in a rollback. Threshold: more than 10% of transactions rolled back. Severity: Info.',
                  action: 'Investigate transaction failures and the retry behavior.',
                },
                {
                  term: '`txn.idle_in_transaction`',
                  meaning:
                    'A session has been idle inside an open transaction for a long time. Threshold: 300 seconds (5 minutes). Severity: Warning.',
                  action: 'Close or commit the idle transaction.',
                },
                {
                  term: '`txn.long_running`',
                  meaning:
                    'A transaction has been open for a long time. Threshold: 900 seconds (15 minutes). Severity: Warning.',
                  action: 'Commit, roll back, or investigate the transaction.',
                },
                {
                  term: '`txn.prepared_orphan`',
                  meaning:
                    'A prepared transaction has been left open for a long time. Threshold: 3,600 seconds (1 hour). Severity: Critical.',
                  action: 'Resolve the prepared transaction so that vacuum can proceed.',
                },
                {
                  term: '`txn.wraparound_risk`',
                  meaning:
                    'The oldest transaction ID in a database is getting old. Threshold: age of 1,000,000,000 transactions. Severity: Critical.',
                  action: 'Run maintenance immediately, and resolve the vacuum starvation.',
                },
                {
                  term: '`txn.multixact_wraparound_risk`',
                  meaning:
                    'The oldest multixact ID in a database is getting old. Threshold: age of 1,000,000,000 multixacts. Severity: Critical.',
                  action: 'Run a freezing vacuum on the tables with the oldest multixact IDs.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-replication',
          title: 'Replication and standby',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`standby.recovery_conflicts`',
                  meaning: 'Queries on a standby are cancelled by WAL replay. Severity: Warning.',
                  action:
                    'Raise `max_standby_streaming_delay` (and `max_standby_archive_delay`) on the standby to let queries finish before replay cancels them, at the cost of the standby falling behind while they run. Or remove the cause on the primary.',
                },
                {
                  term: '`standby.replay_paused`',
                  meaning: 'WAL replay is paused on a standby. Severity: Warning.',
                  action:
                    'Once the reason for the pause is over, resume replay as a superuser: `SELECT pg_wal_replay_resume();`.',
                },
                {
                  term: '`subscription.disabled`',
                  meaning: 'A logical replication subscription is disabled. Severity: Warning.',
                  action:
                    'Find why it was disabled (with `disable_on_error`, the subscriber’s log names the failed change), fix it, then `ALTER SUBSCRIPTION … ENABLE`. If it is no longer needed, `DROP SUBSCRIPTION` also drops its slot on the publisher.',
                },
                {
                  term: '`subscription.errors`',
                  meaning: 'A logical replication subscription reports errors. Severity: Warning.',
                  action:
                    'Read the error in the subscriber’s log. Most errors are a row that conflicts with one already on the subscriber, a missing table or column, or a permission the subscription owner lacks.',
                },
                {
                  term: '`subscription.not_receiving`',
                  meaning:
                    'An enabled subscription is not receiving changes. Threshold: silent for 5 minutes. Severity: Warning.',
                  action:
                    'The finding gives one of two steps. If no apply worker is running, read its error in the subscriber’s log: for a conflicting row, fix the row, or skip the transaction with `ALTER SUBSCRIPTION … SKIP` once its changes are known to be unwanted. If the publisher has been silent, check the walsender for the slot in `pg_stat_replication`, and the network between the two. `ALTER SUBSCRIPTION … DISABLE` and then `ENABLE` restarts the apply worker.',
                },
                {
                  term: '`logical_slot.spill_high`',
                  meaning:
                    'A logical slot writes a lot of decoded changes to disk. Threshold: above 1 MiB per second. Severity: Info.',
                  action:
                    'Raise `logical_decoding_work_mem` (64MB by default), or have the subscription stream large transactions with `ALTER SUBSCRIPTION … SET (streaming = parallel)`.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-partitions',
          title: 'Partitions and sequences',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`partition.default_has_rows`',
                  meaning:
                    'The default partition holds rows that match no other partition. Severity: Warning.',
                  action:
                    'Create the partitions those rows belong to: in one transaction, `ALTER TABLE … DETACH PARTITION` the default partition, create the missing partitions, move the rows with `INSERT INTO … SELECT` and `DELETE`, then `ATTACH PARTITION … DEFAULT`.',
                },
                {
                  term: '`partition.future_missing`',
                  meaning:
                    'A partitioned table has no partition ready for upcoming data. Severity: Warning, or Critical when inserts already fail.',
                  action:
                    'Create the next partitions ahead of time (`CREATE TABLE … PARTITION OF … FOR VALUES FROM (…) TO (…)`), and schedule their creation, with pg_partman or a cron job, so the table never runs out.',
                },
                {
                  term: '`partition.index_invalid`',
                  meaning: 'An index on a partitioned table is invalid. Severity: Warning.',
                  action:
                    'For each partition without the index, `CREATE INDEX CONCURRENTLY` on the partition, then `ALTER INDEX … ATTACH PARTITION` the new index to the partitioned index. The partitioned index becomes valid once every partition has one attached.',
                },
                {
                  term: '`partition.parent_not_analyzed`',
                  meaning:
                    'A partitioned table has never been analyzed. Threshold: at least 10,000 estimated rows. Severity: Info.',
                  action:
                    'Run `ANALYZE` on the table, and again after large changes in its data, for example from a scheduled job.',
                },
                {
                  term: '`sequence.column_narrower`',
                  meaning:
                    'A sequence can count higher than the column that stores its values. Severity: Warning.',
                  action:
                    'Widen the column with `ALTER TABLE … ALTER COLUMN … TYPE bigint`, which rewrites the table under an ACCESS EXCLUSIVE lock. If the column stays as it is, give the sequence the column’s bound with `ALTER SEQUENCE … AS`.',
                },
                {
                  term: '`sequence.exhaustion_risk`',
                  meaning:
                    'A sequence is close to its maximum value. Severity: Warning from 75% of its range, Critical from 90%.',
                  action:
                    'The remediation on the finding names the statements for this sequence: see the finding card.',
                },
                {
                  term: '`sequence.value_hidden`',
                  meaning: 'pglens cannot read the current value of a sequence. Severity: Info.',
                  action:
                    'Run the agent at tier 1 (`pg_read_all_data`), or `GRANT SELECT ON SEQUENCE` to the monitoring role for the sequences to watch. Never grant `USAGE`: it allows `nextval()`, which advances the sequence.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-security',
          title: 'Security, functions and triggers',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`function.secdef_no_search_path`',
                  meaning:
                    'A `SECURITY DEFINER` function has no fixed `search_path`. Severity: Warning, or Critical when the owner is a superuser and `PUBLIC` has `EXECUTE`.',
                  action:
                    'Set a fixed `search_path` with `ALTER FUNCTION`, put `pg_temp` last, and schema-qualify the objects its body uses. If `PUBLIC` has `EXECUTE`, `REVOKE EXECUTE … FROM PUBLIC`, then grant `EXECUTE` to the roles that need it.',
                },
                {
                  term: '`function.untracked`',
                  meaning:
                    'Function calls are not tracked, so function statistics are empty. Severity: Info.',
                  action:
                    "Set `track_functions` to `pl` with `ALTER SYSTEM SET track_functions = 'pl'; SELECT pg_reload_conf();`, or for this database only with `ALTER DATABASE … SET track_functions = 'pl';`. `pl` costs little; `all` also tracks SQL and C functions at a higher cost, and an SQL function the planner inlines is never tracked. pglens never changes the setting.",
                },
                {
                  term: '`security.monitoring_role_can_write`',
                  meaning: 'The monitoring role can write data. Severity: Critical.',
                  action:
                    'Run `deploy/sql/monitoring_user.sql` again as an administrator: it refuses the role, and lists the statement that removes each problem.',
                },
                {
                  term: '`security.monitoring_role_read_only_off`',
                  meaning:
                    'The monitoring role does not default to read-only transactions. Severity: Warning.',
                  action:
                    'Run `deploy/sql/monitoring_user.sql` again: it sets the role default and removes database-specific overrides.',
                },
                {
                  term: '`trigger.constraint_disabled`',
                  meaning:
                    'The triggers that enforce a constraint are disabled. Severity: Critical.',
                  action:
                    'Run `ALTER TABLE … ENABLE TRIGGER ALL`, then look for rows written meanwhile that violate the constraint: enabling the triggers does not check existing rows.',
                },
                {
                  term: '`trigger.user_disabled`',
                  meaning: 'A user trigger is disabled. Severity: Warning.',
                  action:
                    'If the trigger should run, `ALTER TABLE … ENABLE TRIGGER …`. If it is retired, drop it, so its state is not mistaken for an accident.',
                },
              ],
            },
          ],
        },
        {
          id: 'rules-extensions',
          title: 'Extensions and PostGIS',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`extension.upgrade_available`',
                  meaning: 'An installed extension has a newer version available. Severity: Info.',
                  action:
                    'Read the extension’s release notes, then, connected as its owner, run `ALTER EXTENSION … UPDATE`.',
                },
                {
                  term: '`postgis.spatial_unindexed`',
                  meaning:
                    'A PostGIS geometry column has no spatial index. Severity: Info up to 10,000 rows, Warning above 10,000 rows.',
                  action:
                    'If the column is searched spatially, create a GiST index with `CREATE INDEX CONCURRENTLY … USING gist`.',
                },
                {
                  term: '`postgis.srid_mixed`',
                  meaning: 'Geometry columns use more than one coordinate system. Severity: Info.',
                  action:
                    'If the columns are compared with each other, store them in one SRID, or keep a transformed, indexed copy of the ones that are joined.',
                },
                {
                  term: '`postgis.srid_unknown`',
                  meaning:
                    'A geometry column has no declared coordinate system. Severity: Warning.',
                  action:
                    'Find the coordinate system the data was captured in, and declare it with `ALTER TABLE … ALTER COLUMN … TYPE … USING ST_SetSRID(column, <srid>)`, or add the missing definition to `spatial_ref_sys`.',
                },
                {
                  term: '`postgis.upgrade_available`',
                  meaning:
                    'The PostGIS extension is older than its installed library. Severity: Warning.',
                  action:
                    'Read the PostGIS upgrade notes, then, connected as a superuser, run `SELECT postgis_extensions_upgrade();`.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Empty or unexpected views',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.page.noActive}}',
              meaning:
                'Shown when the advisor has no findings at all. The description says “No findings are currently firing” and how many advisor rules were evaluated successfully.',
              action:
                'If a problem is expected, check the {{ui:findings.ruleCatalogue.title}} for rules with instances in {{ui:findings.ruleCatalogue.cannotEvaluate}}.',
            },
            {
              term: '{{ui:findings.page.noMatchFilters}}',
              meaning:
                'Shown when findings exist, but the current filters show none of them. The description says: “Try clearing one or more filters or choose a different state.”',
              action: 'Choose a different State, or clear the filters.',
            },
            {
              term: 'No finding matches “…”',
              meaning:
                'Shown when the search matches no finding in any state. The description lists the searched fields. The button {{ui:findings.page.showAll}} clears the search.',
            },
            {
              term: 'No finding matches “…”',
              meaning:
                'Shown when the search matches findings only in other states, and the current State filter hides them. The description says how many findings match in other states. The button {{ui:findings.page.searchAllStates}} sets State to {{ui:findings.page.allStates}}.',
            },
            {
              term: 'No finding matches “…”',
              meaning:
                'Shown when the search matches findings, but the {{ui:findings.table.severity}}, {{ui:findings.page.scope}}, {{ui:findings.page.cluster}} or {{ui:findings.page.instance}} filter hides them. The button {{ui:findings.page.clearFilters}} resets {{ui:findings.page.state}}, {{ui:findings.table.severity}}, {{ui:findings.page.scope}}, {{ui:findings.page.cluster}} and {{ui:findings.page.instance}}.',
              action: 'Choose {{ui:findings.page.clearFilters}} to widen the view.',
            },
            {
              term: 'Muted finding returns to open',
              meaning:
                'A mute ends at its expiry. When the condition still holds, the finding returns to open.',
              action: 'Mute it again with a reason, or fix the problem.',
            },
          ],
        },
      ],
    },
  ],
}
