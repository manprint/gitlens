import type { HelpTopic } from '../../types'

export const instanceOverviewTopic: HelpTopic = {
  id: 'instance-overview',
  group: 'instance',
  title: 'Instance overview',
  summary:
    'What the {{ui:instance.overview.section}} page shows for one PostgreSQL server: its header and state, the summary tiles and charts of the selected time range, the host it runs on, its settings and durability, its internals, its tables and indexes, its schema objects, its logical replication and its readiness gaps. Each value is explained with how to read it and what to do when it is worrying.',
  page: {
    path: '/instances/:instanceId',
    label: 'Open {{ui:instance.overview.section}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'The Instance overview page shows one PostgreSQL server as the agent reports it: the header and state, the summary tiles and charts of the selected time range, the host it runs on, its settings and durability, its internals, its tables and indexes, its schema objects, its logical replication and its readiness gaps. The values come from the agent reports stored on the server. The other views of the same server, {{ui:shell.nav.queryPerformance}}, {{ui:shell.nav.waitEvents}} and {{ui:shell.nav.locks}}, are in the Instance group of the sidebar.',
        },
      ],
      subsections: [
        {
          id: 'time-range',
          title: 'Time range',
          blocks: [
            {
              kind: 'text',
              text: 'The time range picker sets the window of the charts and of the tiles that cover a range. The default is Last hour. The presets are Last 15 minutes, Last hour, Last 2 hours, Last 4 hours, Last 6 hours, Last 8 hours, Last 24 hours, Last 3 days, Last 7 days, Last 14 days and Last 30 days. Custom time range sets an absolute range in the console time zone, and no range can exceed 30 days.',
            },
          ],
        },
        {
          id: 'database-scope',
          title: 'Database scope',
          blocks: [
            {
              kind: 'text',
              text: 'The Database select sets the database of the per-database figures. When activity data is available, it starts on the most active monitored database. The {{ui:instance.overviewCharts.group.workload.title}} and {{ui:instance.overviewCharts.group.memoryIo.title}} charts and the {{ui:instance.overview.tile.transactions}} tile use the selected database. The {{ui:instance.overviewCharts.card.connections.title}} and {{ui:instance.overviewCharts.group.walCheckpoints.title}} charts cover the whole instance, and the Queries and {{ui:instance.overviewCharts.group.tablesIndexes.title}} charts cover every database of the instance.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Database',
                  meaning: 'The database whose per-database figures the page shows.',
                },
                {
                  term: 'No monitored databases',
                  meaning:
                    'The empty state shown in place of the select. Its text reads: This instance has no database available for per-database metrics.',
                  action:
                    'Check the `databases.max` budget and the `include` filter in the agent configuration.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'header',
      title: 'Instance header',
      blocks: [
        {
          kind: 'text',
          text: 'The header starts with the eyebrow Server, then the title, the subtitle and the freshness badge. The title is the display name of the instance when one is set; the address then moves to the subtitle. The subtitle shows the state badge, the address when a display name is set, and an ID button that copies the instance id, shown in its tooltip. Below the title, five cells show Role, PostgreSQL, {{ui:instance.header.label.tier}}, Last seen and {{ui:instance.header.label.cluster}}. The {{ui:instance.header.slowest}} button opens that page filtered to this instance.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'up',
              meaning: 'The instance is reported as up.',
            },
            {
              term: 'down',
              meaning:
                'The instance is reported as down. The warning below the header reads: {{ui:instance.header.downNotice}}',
              action:
                'Check that the agent runs on the host and reaches the server. The values stop updating until the agent reports again.',
            },
            {
              term: 'Role',
              meaning:
                'The role reported by the agent, as a badge. The value unknown appears when no role is reported.',
            },
            {
              term: 'standby (read-only)',
              meaning:
                'The callout shown when the role is standby. Its Replay lag reads loading while the lag is read, unavailable when it cannot be read, a duration when it is measured, and Unknown with the reason {{ui:instance.header.replayNotMeasured}}',
              action:
                'When the lag grows, check the network between the primary and this standby, and whether replay is paused, as described in [{{ui:instance.internals.walReplay}}](/help/instance-overview#wal-replay).',
            },
            {
              term: 'PostgreSQL',
              meaning: 'The PostgreSQL version of the server, as the agent reports it.',
            },
            {
              term: '{{ui:instance.header.label.tier}}',
              meaning:
                'The permission tier the agent has on this instance. Cancelling or ending sessions needs permission tier T2, granted with `monitoring_user.sql -v tier1=1 -v tier2=1`, see [{{ui:shell.nav.locks}}](/help/locks).',
            },
            {
              term: 'Last seen',
              meaning:
                'The time of the last report the server received from the agent for this instance, in the console time zone.',
            },
            {
              term: '{{ui:instance.header.label.cluster}}',
              meaning:
                'The cluster the instance belongs to, shown by name once the cluster list has loaded, otherwise by id. The link opens the cluster detail page.',
            },
            {
              term: '{{ui:instance.header.slowest}}',
              meaning:
                'The button in the header. It opens the query analysis filtered to this instance, described in [{{ui:instance.header.slowest}}](/help/slowest-queries).',
            },
          ],
        },
      ],
    },
    {
      id: 'views',
      title: 'Instance views',
      blocks: [
        {
          kind: 'text',
          text: 'The Instance group of the sidebar switches between four views of the same instance; a database chosen on one view stays the scope of the others.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:instance.overview.section}}',
              meaning: 'This page, with the section tabs described below.',
            },
            {
              term: '{{ui:shell.nav.queryPerformance}}',
              meaning:
                'Opens the query analysis of this instance, described in [{{ui:shell.nav.queryPerformance}}](/help/query-performance).',
            },
            {
              term: '{{ui:shell.nav.waitEvents}}',
              meaning:
                'Opens the wait event analysis of this instance, described in [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:shell.nav.locks}}',
              meaning:
                'Opens the sessions, the locks and the blocking chains of this instance, described in [{{ui:shell.nav.locks}}](/help/locks).',
            },
          ],
        },
      ],
    },
    {
      id: 'section-tabs',
      title: 'Section tabs',
      blocks: [
        {
          kind: 'text',
          text: 'Below the header, the section tabs switch between eight sections: {{ui:instance.section.overview}}, {{ui:instance.section.host}}, {{ui:instance.section.settings}}, {{ui:instance.relations.section}}, {{ui:instance.section.catalog}}, {{ui:instance.section.logical}}, {{ui:instance.section.internals}} and {{ui:instance.section.readiness}}.',
        },
      ],
    },
    {
      id: 'summary-tiles',
      title: 'Summary tiles',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:instance.section.overview}} tab opens with a row of summary tiles. The tiles whose label ends in in range cover the selected time range.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:instance.overview.tile.connectionsUsed}}',
              meaning:
                'Connections in use, against the configured `max_connections`. The advisor rule `conn.saturation` warns above 80 % of the connections that `max_connections` allows.',
              action:
                'Look for idle sessions in [{{ui:shell.nav.locks}}](/help/locks). If many are idle, put a connection pooler in front of the server. Raise `max_connections` only when the server memory allows it.',
            },
            {
              term: '{{ui:instance.overview.tile.transactions}}',
              meaning:
                'Transactions per second in the selected database, counting both commits and rollbacks. The advisor rule `txn.high_rollback_ratio` reports a large share of transactions that end in a rollback, with a threshold of 10 %.',
              action:
                'Read the errors in the server log and in the application that aborts the transactions.',
            },
            {
              term: '{{ui:instance.overview.tile.cacheHitRatio}}',
              meaning:
                'Share of page reads found in shared buffers, the memory cache of PostgreSQL, rather than read from disk or the operating system cache. The advisor rule `query.cache_miss_high` flags a statement with more than 1000 calls whose share of shared block reads is above 30 %.',
              action:
                'When the ratio falls while reads grow, open [{{ui:instance.header.slowest}}](/help/slowest-queries) to find the statements that read the most, then check their indexes in [Index analysis](/help/indexes).',
            },
            {
              term: '{{ui:instance.overview.tile.walInRange}}',
              meaning:
                'WAL bytes generated by the whole instance during the range. The WAL is the write-ahead log that PostgreSQL writes before it changes the data files.',
            },
            {
              term: '{{ui:instance.overview.tile.checkpointsInRange}}',
              meaning:
                'Timed and Requested checkpoints during the range. The chart {{ui:instance.overviewCharts.card.checkpoints.title}} in {{ui:instance.overviewCharts.group.walCheckpoints.title}} shows them over time.',
              action:
                'When Requested checkpoints dominate, WAL fills faster than `max_wal_size` allows. Check the {{ui:instance.overviewCharts.group.walCheckpoints.title}} charts before you raise `max_wal_size`.',
            },
            {
              term: '{{ui:instance.overview.tile.tempBytesInRange}}',
              meaning: 'Bytes written to temporary files during the range.',
              action:
                'Find the statements that write to disk in [{{ui:instance.header.slowest}}](/help/slowest-queries), then raise `work_mem` for them in their session, or fix their plans.',
            },
            {
              term: '{{ui:instance.overview.tile.tempFilesInRange}}',
              meaning: 'Temporary files created during the range.',
            },
            {
              term: '{{ui:instance.overview.tile.deadlocksInRange}}',
              meaning:
                'Deadlocks during the range. The advisor rule `db.deadlock_rate` reports that deadlocks are occurring.',
              action:
                'Read the deadlock reports in the server log: each names the statements and the locks involved. Make the transactions take their locks in the same order.',
            },
          ],
        },
      ],
    },
    {
      id: 'charts',
      title: 'Charts',
      blocks: [
        {
          kind: 'text',
          text: 'Below the tiles, the charts are in five groups: {{ui:instance.overviewCharts.group.workload.title}}, {{ui:instance.overviewCharts.group.memoryIo.title}}, {{ui:instance.overviewCharts.group.walCheckpoints.title}}, Queries and {{ui:instance.overviewCharts.group.tablesIndexes.title}}. Each chart names the sampling interval at which the agent samples the data that feeds it.',
        },
      ],
      subsections: [
        {
          id: 'chart-workload',
          title: '{{ui:instance.overviewCharts.group.workload.title}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.overviewCharts.group.workload.title}} group shows transactions, connections, deadlocks and recovery conflicts. The transaction and deadlock figures are for the selected database; the connections are for the whole instance.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overviewCharts.card.transactions.title}}',
                  meaning:
                    'Transactions per second in the selected database, counting both commits and rollbacks. Sampled every 30 s. A large share of rollbacks is what the advisor rule `txn.high_rollback_ratio` reports, with a threshold of 10 %.',
                  action:
                    'Read the errors in the server log and in the application that aborts the transactions.',
                },
                {
                  term: 'Connections',
                  meaning:
                    'Connections of the whole instance, with the {{ui:instance.overviewCharts.line.configuredMaximum}} line. Sampled every 10 s. The advisor rule `conn.saturation` warns above 80 % of the connections that `max_connections` allows.',
                  action:
                    'Follow the action of the tile [{{ui:instance.overview.tile.connectionsUsed}}](/help/instance-overview#summary-tiles).',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.deadlocksConflicts.title}}',
                  meaning:
                    '{{ui:instance.overviewCharts.line.deadlocks}} and {{ui:instance.internals.recoveryConflicts}} of the selected database. Sampled every 30 s. The advisor rule `db.deadlock_rate` reports that deadlocks are occurring.',
                  action:
                    'For deadlocks, read the reports in the server log. For {{ui:instance.internals.recoveryConflicts}}, read [{{ui:instance.internals.recoveryConflicts}}](/help/instance-overview#recovery-conflicts) in {{ui:instance.section.internals}}.',
                },
              ],
            },
          ],
        },
        {
          id: 'chart-memory-io',
          title: '{{ui:instance.overviewCharts.group.memoryIo.title}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.overviewCharts.group.memoryIo.title}} group shows how often reads are served from shared buffers, and the temporary files written by queries, for the selected database.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overview.tile.cacheHitRatio}}',
                  meaning:
                    'Share of page reads found in shared buffers. Sampled every 30 s. A read that misses shared buffers may still be served by the operating system cache, so a lower ratio is not always disk I/O.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.blocks.title}}',
                  meaning:
                    'Blocks read from outside shared buffers, and blocks found in shared buffers, for the selected database. Sampled every 30 s.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.temp.title}}',
                  meaning:
                    'Temporary files and temporary bytes written by queries of the selected database. Sampled every 30 s.',
                  action:
                    'Find the statements that write temporary files in [{{ui:instance.header.slowest}}](/help/slowest-queries), then raise `work_mem` for them, or fix their plans.',
                },
              ],
            },
          ],
        },
        {
          id: 'chart-wal-checkpoints',
          title: 'WAL & checkpoints',
          blocks: [
            {
              kind: 'text',
              text: 'The WAL & checkpoints group shows the write-ahead log generated by the whole instance, and the checkpoints that flush it to disk.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overviewCharts.card.wal.title}}',
                  meaning:
                    'WAL bytes, WAL records and {{ui:instance.overviewCharts.line.fullPageImages}} written by the whole instance. Sampled every 15 s. A full-page image is a whole page written to the WAL the first time it changes after a checkpoint.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.checkpoints.title}}',
                  meaning: 'Timed and Requested checkpoints. Sampled every 30 s.',
                  action:
                    'When Requested checkpoints dominate, WAL fills faster than `max_wal_size` allows. Check the {{ui:instance.overviewCharts.group.walCheckpoints.title}} charts before you raise `max_wal_size`.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.checkpointTime.title}}',
                  meaning:
                    'Time spent writing and syncing files during checkpoints, from the Write and Sync series. Sampled every 30 s.',
                  action:
                    'When these times grow together with Requested checkpoints, check the storage latency with the tools of the operating system.',
                },
              ],
            },
          ],
        },
        {
          id: 'chart-queries',
          title: 'Queries',
          blocks: [
            {
              kind: 'text',
              text: 'The Queries group shows the statements tracked by `pg_stat_statements` in every database of the instance. Sampled every 60 s.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overviewCharts.card.calls.title}}',
                  meaning: 'Statement executions per second, over the tracked statements.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.execTime.title}}',
                  meaning: 'Execution time of the tracked statements per second.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.rows.title}}',
                  meaning: 'Rows returned or affected per second by the tracked statements.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.statementReads.title}}',
                  meaning:
                    'Shared blocks read per second by the tracked statements, that is blocks not found in shared buffers.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.statementTemp.title}}',
                  meaning: 'Temporary blocks written per second by the tracked statements.',
                  action:
                    'To find the statement behind a line, open [{{ui:instance.header.slowest}}](/help/slowest-queries) from the header button.',
                },
              ],
            },
          ],
        },
        {
          id: 'chart-tables-indexes',
          title: '{{ui:instance.overviewCharts.group.tablesIndexes.title}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.overviewCharts.group.tablesIndexes.title}} group covers the tables and indexes tracked by the agent in every database of the instance. These charts are sampled every 5 minutes.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.overviewCharts.card.scans.title}}',
                  meaning:
                    'Sequential scans and {{ui:instance.overviewCharts.card.indexScans.title}} on the tracked tables.',
                  action:
                    'When sequential scans grow on a large table, check its indexes in [Index analysis](/help/indexes) and the statements that scan it in [{{ui:instance.header.slowest}}](/help/slowest-queries).',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.tupleChurn.title}}',
                  meaning:
                    'Rows {{ui:instance.overviewCharts.line.inserted}}, Updated, {{ui:instance.overviewCharts.line.hotUpdated}} and {{ui:instance.overviewCharts.line.deleted}} on the tracked tables. A HOT update keeps the new row version on the same page, so the indexes need no new entry for it.',
                },
                {
                  term: '{{ui:instance.overviewCharts.card.deadTuples.title}}',
                  meaning:
                    'Dead rows of the tracked tables, the old row versions that vacuum has not yet removed.',
                  action:
                    'A line that keeps rising means vacuum does not keep up. Check the {{ui:instance.relations.col.deadRatio}} of the tables in [{{ui:instance.relations.section}}](/help/instance-overview#relations).',
                },
                {
                  term: 'Index size',
                  meaning: 'Size of the tracked indexes, summed over every database.',
                  action:
                    'The advisor rule `index.unused` reports an index that had no scans in the observed history. Verify it in [Index analysis](/help/indexes) before you drop it.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'host',
      title: 'Host metrics',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:instance.section.host}} tab reads the machine the agent runs on: CPU, memory, swap, load average and the filesystem that holds the PostgreSQL data directory. The panel {{ui:instance.host.section}} shows the host tiles and the Host charts.',
        },
      ],
      subsections: [
        {
          id: 'host-tiles',
          title: 'Host tiles',
          blocks: [
            {
              kind: 'text',
              text: 'The host tiles show the latest reading of each value. The {{ui:instance.host.chart.filesystemTitle}} tiles need the PostgreSQL data directory mounted in the agent container, as [{{ui:instance.host.chart.group}}](/help/instance-overview#host-charts) describes.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.host.metric.cpuUsed}}',
                  meaning:
                    'Share of CPU in use. The label names its source: {{ui:instance.host.cpuHost}} for the whole host, or {{ui:instance.host.cpuCgroup}} when the agent reads a container CPU limit.',
                  action:
                    'When the CPU stays high, check the statements that use the most time in [{{ui:instance.header.slowest}}](/help/slowest-queries). If the load comes from other processes, look at them on the machine itself.',
                },
                {
                  term: '{{ui:instance.host.metric.cpuCount}}',
                  meaning: 'The number of CPUs.',
                },
                {
                  term: '{{ui:instance.host.metric.memTotal}}',
                  meaning: 'Total memory of the host.',
                },
                {
                  term: '{{ui:instance.host.metric.memAvailable}}',
                  meaning: 'Memory the operating system reports as available.',
                },
                {
                  term: '{{ui:instance.host.metric.swapTotal}}',
                  meaning: '{{ui:instance.host.chart.swapTitle}} configured on the host.',
                },
                {
                  term: '{{ui:instance.host.metric.swapUsed}}',
                  meaning: '{{ui:instance.host.chart.swapTitle}} in use on the host.',
                  action:
                    '{{ui:instance.host.chart.swapTitle}} in use together with a low {{ui:instance.host.metric.memAvailable}} means the machine is short of memory. Find the processes that use it; if PostgreSQL is the cause, reduce the connections or `shared_buffers`.',
                },
                {
                  term: '{{ui:instance.host.metric.load1}}',
                  meaning: 'The load average over the last minute.',
                },
                {
                  term: '{{ui:instance.host.metric.load5}}',
                  meaning: 'The load average over the last 5 minutes.',
                },
                {
                  term: '{{ui:instance.host.metric.load15}}',
                  meaning: 'The load average over the last 15 minutes.',
                },
                {
                  term: '{{ui:instance.host.metric.diskTotal}}',
                  meaning: 'Total size of the filesystem that holds the PostgreSQL data directory.',
                },
                {
                  term: '{{ui:instance.host.metric.diskFree}}',
                  meaning: 'Free space on the filesystem that holds the PostgreSQL data directory.',
                },
                {
                  term: '{{ui:instance.host.metric.diskFreeRatio}}',
                  meaning:
                    'Free space as a share of the filesystem. The advisor rule `disk.free_low` warns when less than 15 % of the data disk is free.',
                  action:
                    'Check the [{{ui:instance.internals.walDirectory}}](/help/instance-overview#wal-directory) and the [{{ui:instance.internals.databaseSizes}}](/help/instance-overview#database-sizes), then the largest tables. Free space or add capacity before the disk fills.',
                },
              ],
            },
          ],
        },
        {
          id: 'host-charts',
          title: 'Host charts',
          blocks: [
            {
              kind: 'text',
              text: 'The Host charts show the host readings over the selected range. The {{ui:instance.host.chart.swapTitle}} and {{ui:instance.host.chart.filesystemTitle}} charts are replaced by a note when their data is missing.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.host.chart.cpuUsedTitle}}',
                  meaning: 'CPU in use over the range, in percent.',
                },
                {
                  term: '{{ui:instance.host.chart.loadTitle}}',
                  meaning: 'The load average over 1, 5 and 15 minutes.',
                },
                {
                  term: 'Memory',
                  meaning: 'Memory of the host over the range.',
                },
                {
                  term: '{{ui:instance.host.chart.swapTitle}}',
                  meaning:
                    '{{ui:instance.host.chart.swapTitle}} of the host over the range. When no swap is configured, the chart is replaced by the note No swap is configured.',
                },
                {
                  term: '{{ui:instance.host.chart.filesystemTitle}}',
                  meaning:
                    'Free space of the filesystem that holds the data directory. When the disk is not measured, the chart is replaced by the note {{ui:instance.host.chart.filesystemTitle}} metrics need the PostgreSQL data directory mounted read-only at the same path in the agent container.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'settings',
      title: '{{ui:instance.section.settings}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:instance.section.settings}} tab shows the panel {{ui:instance.settings.section}}: first the {{ui:instance.settings.durabilitySummary}}, then the table {{ui:instance.settings.observed}}.',
        },
      ],
      subsections: [
        {
          id: 'durability',
          title: '{{ui:instance.settings.durabilitySummary}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.settings.durabilitySummary}} lists five settings that decide crash safety, with their values. A value that weakens crash safety gets a severity: critical for `fsync` or `full_page_writes` set to `off`; warning for `archive_mode` set to `off`, `wal_level` set to `minimal` or `synchronous_commit` set to `off`.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'fsync',
                  meaning:
                    'Off: PostgreSQL does not force its writes to disk, so a crash or a power loss can corrupt the data. Severity critical when off.',
                  action:
                    'Set `fsync` to `on` with `ALTER SYSTEM SET fsync = on;` and then `SELECT pg_reload_conf();`, or set it in the configuration file.',
                },
                {
                  term: 'full_page_writes',
                  meaning:
                    'Off: changed pages are not written whole to the WAL after a checkpoint, so a crash can leave a page that recovery cannot repair. Severity critical when off.',
                  action: 'Set `full_page_writes` to `on`, in the same way as `fsync`.',
                },
                {
                  term: 'archive_mode',
                  meaning:
                    'Off: finished WAL files are not archived, so a backup cannot be restored to a point in time. Severity warning when off.',
                  action:
                    'Set `archive_mode` to `on` with a working `archive_command`. The setting takes effect after a restart of the server.',
                },
                {
                  term: 'wal_level',
                  meaning:
                    'Minimal: the WAL keeps only what crash recovery needs, so there is no replication and no point-in-time recovery. Severity warning when minimal.',
                  action:
                    'Set `wal_level` to `replica`, or to `logical` when you use logical replication. The setting takes effect after a restart of the server.',
                },
                {
                  term: 'synchronous_commit',
                  meaning:
                    'Off: a commit is confirmed before its WAL is flushed to disk, so a crash can lose the latest commits. Severity warning when off.',
                  action:
                    'Set `synchronous_commit` back to `on` where losing commits is not acceptable. It can also be changed per session or per transaction.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Read the durability settings on the server',
              code: "SELECT name, setting, source, context, pending_restart FROM pg_settings WHERE name IN ('fsync', 'full_page_writes', 'archive_mode', 'wal_level', 'synchronous_commit');",
            },
          ],
        },
        {
          id: 'settings-table',
          title: '{{ui:instance.settings.observed}}',
          blocks: [
            {
              kind: 'text',
              text: 'The table {{ui:instance.settings.observed}} lists the settings whose value changed since the date in the {{ui:instance.settings.changedSince}} control. The search field {{ui:instance.settings.search}} and the Source and State filters change only what the table shows. A cell reads {{ui:instance.settings.unitNotReported}}, Source was not reported. or {{ui:instance.settings.contextNotReported}} when the agent sent no value for it.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Name',
                  meaning: 'The setting name.',
                },
                {
                  term: 'Value',
                  meaning: 'The value of the setting.',
                },
                {
                  term: 'Unit',
                  meaning: 'The unit of the value.',
                },
                {
                  term: 'Source',
                  meaning:
                    'Where the value was set. The Source filter narrows the table to one source.',
                },
                {
                  term: 'Context',
                  meaning:
                    'When a change takes effect: some settings need a restart of the server, some a reload of the configuration, others apply to new sessions.',
                },
                {
                  term: '{{ui:instance.settings.col.changedAt}}',
                  meaning: 'The time the value last changed.',
                },
                {
                  term: '{{ui:instance.settings.state.pending}}',
                  meaning:
                    'A State value: the value is set but needs a restart of the server before it takes effect.',
                  action:
                    'Restart the server in a maintenance window to put a pending value in force.',
                },
                {
                  term: '{{ui:instance.settings.state.inForce}}',
                  meaning: 'A State value: the value is in effect.',
                },
                {
                  term: '{{ui:instance.settings.changedSince}}',
                  meaning:
                    'A date control above the table. The button {{ui:instance.settings.useRange}} fills it with the selected time range. The table lists the settings changed since this date.',
                },
                {
                  term: 'No setting changed since the date in {{ui:instance.settings.changedSince}}.',
                  meaning:
                    'The empty state shown when no setting changed since the date in the {{ui:instance.settings.changedSince}} control.',
                  action:
                    'Choose an earlier date in {{ui:instance.settings.changedSince}}, or use {{ui:instance.settings.useRange}}.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'internals',
      title: '{{ui:instance.section.internals}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:instance.section.internals}} tab shows the panel {{ui:instance.internals.section}}: the {{ui:instance.internals.walDirectory}}, the {{ui:instance.internals.walReplay}}, the {{ui:instance.internals.databaseSizes}}, the {{ui:instance.internals.slru}}, the {{ui:instance.internals.tempFiles}} and the {{ui:instance.internals.recoveryConflicts}}.',
        },
      ],
      subsections: [
        {
          id: 'wal-directory',
          title: '{{ui:instance.internals.walDirectory}}',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.internals.walDirectory}}',
                  meaning:
                    'Total size and file count of the `pg_wal` directory, read with `pg_ls_waldir()`.',
                  action:
                    'The advisor rule `wal.directory_large` reports a `pg_wal` larger than twice `max_wal_size` plus `wal_keep_size`. Check the replication slots, the archiving with `archive_mode` and `archive_command`, and `wal_keep_size`.',
                },
                {
                  term: 'pg_wal is not listed',
                  meaning:
                    'The note shown in place of the value when the agent cannot list `pg_wal`: pg_wal is not listed: the monitoring role needs EXECUTE on pg_ls_waldir(), which pg_monitor grants.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Count and size the WAL directory',
              code: 'SELECT count(*) AS files, pg_size_pretty(sum(size)) AS total FROM pg_ls_waldir();',
            },
            {
              kind: 'sql',
              label: 'Replication slots',
              code: 'SELECT slot_name, slot_type, active, restart_lsn FROM pg_replication_slots;',
            },
          ],
        },
        {
          id: 'wal-replay',
          title: 'WAL replay',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.internals.walReplay}} view shows the replay state of a standby, with the badges paused and running.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'paused',
                  meaning:
                    'The badge paused means WAL replay is paused on this standby: received WAL is not applied until replay resumes.',
                  action:
                    'Resume replay with `SELECT pg_wal_replay_resume();` on the standby, once you know why it was paused.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Check recovery and replay state on a standby',
              code: 'SELECT pg_is_in_recovery(), pg_is_wal_replay_paused();',
            },
          ],
        },
        {
          id: 'database-sizes',
          title: '{{ui:instance.internals.databaseSizes}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.internals.databaseSizes}} view lists the size of each database, with its change over the last 24 hours and over the last 7 days.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Size',
                  meaning: 'Size on disk of the database.',
                },
                {
                  term: 'Last 24 hours',
                  meaning: 'Size change over the last 24 hours.',
                },
                {
                  term: 'Last 7 days',
                  meaning: 'Size change over the last 7 days.',
                  action:
                    'For a database that grows fast, find its largest tables in [{{ui:instance.relations.section}}](/help/instance-overview#relations) and check their dead rows.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Database sizes, largest first',
              code: 'SELECT datname, pg_size_pretty(pg_database_size(datname)) AS size FROM pg_database ORDER BY pg_database_size(datname) DESC;',
            },
          ],
        },
        {
          id: 'slru-caches',
          title: '{{ui:instance.internals.slru}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.internals.slru}} view lists the small caches that PostgreSQL uses for transaction status, subtransactions and multixacts. A page missing from one of these caches is read from disk, so a cache that misses often slows the sessions that need it.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Cache',
                  meaning: 'The name of the SLRU cache, as PostgreSQL reports it.',
                },
                {
                  term: '{{ui:instance.internals.col.hitRatio}}',
                  meaning: 'Hits divided by hits plus reads, in percent of the cache accesses.',
                  action:
                    'A low ratio on the subtransaction or multixact cache: find the sessions that use savepoints or many row locks in [{{ui:shell.nav.locks}}](/help/locks), and reduce them.',
                },
                {
                  term: '{{ui:instance.internals.col.readFromDisk}}',
                  meaning: 'Pages read from disk, per second.',
                },
                {
                  term: 'Written',
                  meaning: 'Pages written, per second.',
                },
                {
                  term: 'Zeroed',
                  meaning: 'Pages zeroed, per second.',
                },
                {
                  term: 'Flushes',
                  meaning: 'Flushes, per second.',
                },
                {
                  term: 'Truncations',
                  meaning: 'Truncations, per second.',
                },
              ],
            },
          ],
        },
        {
          id: 'temporary-files',
          title: '{{ui:instance.internals.tempFiles}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.internals.tempFiles}} view lists the databases whose queries wrote temporary files, with the files created and the bytes written. A query writes temporary files when its sort or hash does not fit in `work_mem`.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Database',
                  meaning: 'The database the row refers to.',
                },
                {
                  term: 'Files',
                  meaning: 'Temporary files created in this database.',
                },
                {
                  term: 'Written',
                  meaning: 'Bytes written to temporary files in this database.',
                  action:
                    'Find the statements that write to disk in [{{ui:instance.header.slowest}}](/help/slowest-queries). Set a higher `work_mem` for them in their session, or fix their plans.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Temporary files since the last statistics reset',
              code: 'SELECT datname, temp_files, pg_size_pretty(temp_bytes) AS temp_size FROM pg_stat_database ORDER BY temp_bytes DESC;',
            },
          ],
        },
        {
          id: 'recovery-conflicts',
          title: '{{ui:instance.internals.recoveryConflicts}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.internals.recoveryConflicts}} view lists the queries that WAL replay cancelled on a standby, by database and reason. An empty list means replay cancelled no query in the period shown.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Database',
                  meaning: 'The database where the query was cancelled.',
                },
                {
                  term: 'Reason',
                  meaning: 'The conflict reason, as the agent reports it.',
                },
                {
                  term: '{{ui:instance.internals.col.queriesCancelled}}',
                  meaning: 'Queries cancelled for that reason.',
                  action:
                    'When cancellations are frequent, consider `hot_standby_feedback` (it can bloat the primary) or a higher `max_standby_streaming_delay`, and check the long queries on the standby.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Recovery conflicts by database',
              code: 'SELECT datname, confl_snapshot, confl_lock, confl_bufferpin, confl_deadlock FROM pg_stat_database_conflicts;',
            },
          ],
        },
      ],
    },
    {
      id: 'other-tabs',
      title: 'Other tabs',
      blocks: [
        {
          kind: 'text',
          text: 'The section tabs {{ui:instance.relations.section}}, {{ui:instance.section.catalog}}, {{ui:instance.section.logical}} and {{ui:instance.section.readiness}} list the objects of the instance and what needs attention on it.',
        },
      ],
    },
    {
      id: 'relations',
      title: '{{ui:instance.relations.section}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:instance.relations.section}} tab lists the tables, the indexes and the bloat estimates of each monitored database, as the agent last reported them. Its views are Tables, Indexes, {{ui:instance.relations.view.tableBloat}} and {{ui:instance.relations.view.indexBloat}}. Tables and indexes come from the last 15 minutes, and bloat estimates from the last 13 hours. Each list is limited by the top_n setting of its check, such as `checks.table_stats.top_n`, `checks.index_stats.top_n` or `checks.bloat_estimate.top_n`.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Relation',
              meaning: 'The schema and the name of the table.',
            },
            {
              term: '{{ui:instance.relations.col.liveTuples}}',
              meaning: 'Live rows of the table.',
            },
            {
              term: '{{ui:instance.overviewCharts.card.deadTuples.title}}',
              meaning:
                'Dead rows of the table, the old row versions that vacuum has not yet removed.',
            },
            {
              term: '{{ui:instance.relations.col.deadRatio}}',
              meaning: 'The ratio of dead rows to the rows of the table, as the agent reports it.',
              action:
                'Check the last vacuum and analyze times, and whether autovacuum keeps up. Run `VACUUM (ANALYZE)` on the table when its dead rows keep growing.',
            },
            {
              term: 'Size',
              meaning: 'Size of the table.',
            },
            {
              term: 'Sequential scans',
              meaning: 'Sequential scans of the table.',
            },
            {
              term: 'Index scans',
              meaning: 'Index scans of the table.',
            },
            {
              term: '{{ui:instance.relations.col.lastVacuum}}',
              meaning:
                'When the table was last vacuumed. The text Never vacuumed since the statistics were reset. appears when it has not been vacuumed since then.',
            },
            {
              term: '{{ui:instance.relations.col.lastAnalyze}}',
              meaning:
                'When the table was last analyzed. The text Never analysed since the statistics were reset. appears when it has not been analyzed since then.',
            },
            {
              term: 'Index',
              meaning: 'The name of the index, in the Indexes view.',
            },
            {
              term: 'Last scan',
              meaning:
                'When the index was last scanned. The cell reads never since reset when the index had no scan since the statistics were reset, and not available on PostgreSQL 15, which does not report it.',
              action:
                'An index with no scan since the statistics were reset is a candidate to drop. Verify it in [Index analysis](/help/indexes) first.',
            },
            {
              term: '{{ui:instance.relations.col.realSize}}',
              meaning: 'The actual size of the table or index.',
            },
            {
              term: '{{ui:instance.relations.col.expectedSize}}',
              meaning: 'The size the table or index should have without bloat, as estimated.',
            },
            {
              term: 'Bloat',
              meaning:
                'Estimated wasted space in a table or a B-tree index. It is an estimate, not an exact measurement.',
            },
            {
              term: '{{ui:instance.relations.col.bloatRatio}}',
              meaning: 'The bloat as a share of the real size.',
              action:
                'Use the estimate to choose a rebuild candidate, then measure with {{ui:instance.relations.exact.button}} before you rebuild.',
            },
            {
              term: 'Method',
              meaning: 'How the estimate was computed. pgstattuple measures tables only.',
            },
            {
              term: '{{ui:instance.relations.col.exactMeasurement}}',
              meaning:
                'Runs pgstattuple on the agent in a read-only transaction. It reads the whole table, bounded at 50 seconds, and needs the extension to be installed already in the database that holds the table: pglens never installs extensions.',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Tables with the most dead rows',
          code: 'SELECT relname, n_live_tup, n_dead_tup, last_autovacuum, last_autoanalyze FROM pg_stat_user_tables ORDER BY n_dead_tup DESC LIMIT 20;',
        },
      ],
    },
    {
      id: 'catalog',
      title: '{{ui:instance.section.catalog}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:instance.section.catalog}} tab checks objects of each monitored database. Its tabs are {{ui:instance.catalog.tabs.triggers}}, Sequences, {{ui:instance.catalog.tabs.partitions}}, Functions, {{ui:instance.catalog.tabs.securityDefiner}}, Unindexed foreign keys, {{ui:instance.catalog.tabs.extensions}} and {{ui:instance.catalog.tabs.postgis}}.',
        },
      ],
      subsections: [
        {
          id: 'triggers',
          title: '{{ui:instance.catalog.tabs.triggers}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.catalog.tabs.triggers}} tab lists the triggers of each monitored database, with Trigger, Table, State, {{ui:instance.catalog.triggers.enforces}}, Fires, Function and {{ui:instance.catalog.triggers.stateSince}}. The filter {{ui:instance.catalog.triggers.disabledOnly}} lists the disabled triggers. The empty state No triggers match these filters appears when the filters hide every trigger, and it suggests trying another database or showing enabled triggers too.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'State',
                  meaning: 'Whether the trigger is enabled or disabled.',
                  action:
                    'When the trigger should run, enable it with `ALTER TABLE table_name ENABLE TRIGGER trigger_name;`. The advisor rule `trigger.user_disabled` reports a disabled user trigger.',
                },
              ],
            },
          ],
        },
        {
          id: 'sequences',
          title: 'Sequences',
          blocks: [
            {
              kind: 'text',
              text: 'The Sequences tab compares the last value of each sequence with the integer type of the column it feeds, in the columns Sequence, Feeds, {{ui:instance.catalog.seq.lastValue}}, Limit, Used and Remaining.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Remaining',
                  meaning: 'How many values the sequence can still produce before its limit.',
                  action: 'When Remaining runs low, plan to change the column to `bigint`.',
                },
                {
                  term: 'Not readable.',
                  meaning:
                    'The sequence cannot be read. The reason is either The monitoring role has no SELECT on this sequence: grant it, or run the agent at tier 1., or Reading the sequence waited too long for a lock.',
                  action:
                    'Grant SELECT on the sequence to the monitoring role, or check whether the sequence was locked at that moment.',
                },
              ],
            },
          ],
        },
        {
          id: 'partitions',
          title: '{{ui:instance.catalog.tabs.partitions}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.catalog.tabs.partitions}} tab lists the partitioned tables with their Key, {{ui:instance.catalog.tabs.partitions}}, {{ui:instance.catalog.part.sizeEstimate}}, {{ui:instance.catalog.part.defaultPartition}}, Invalid indexes and Analyzed.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.catalog.part.defaultPartition}}',
                  meaning:
                    'The default partition of the table, which takes the rows that match no other partition.',
                },
                {
                  term: 'Invalid indexes',
                  meaning: 'The number of invalid indexes on the partitioned table.',
                },
              ],
            },
          ],
        },
        {
          id: 'functions',
          title: 'Functions',
          blocks: [
            {
              kind: 'text',
              text: 'The Functions tab lists the tracked functions with their Language, Calls, {{ui:instance.catalog.fn.ownTime}}, {{ui:instance.catalog.fn.totalTime}} and {{ui:instance.catalog.fn.meanPerCall}}. {{ui:instance.catalog.fn.ownTime}} is the time in the function body, leaving out the functions it calls. {{ui:instance.catalog.fn.totalTime}} includes them.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Calls',
                  meaning: 'Calls of the function.',
                },
                {
                  term: 'Language',
                  meaning: 'The language the function is written in.',
                },
                {
                  term: '{{ui:instance.catalog.fn.ownTime}}',
                  meaning:
                    'Time spent in the body of the function, leaving out the functions it calls.',
                },
                {
                  term: '{{ui:instance.catalog.fn.totalTime}}',
                  meaning: 'Time spent in the function, including the functions it calls.',
                },
                {
                  term: '{{ui:instance.catalog.fn.meanPerCall}}',
                  meaning:
                    'The mean time of one call. The cell reads not called when the function had no call in the window.',
                },
                {
                  term: 'No tracked function was called in the window, or track_functions is none.',
                  meaning:
                    'The empty state shown when no tracked function was called in the window, or when the setting `track_functions` is none.',
                  action:
                    'Set `track_functions` to `pl` to track functions written in a procedural language.',
                },
              ],
            },
          ],
        },
        {
          id: 'security-definer',
          title: '{{ui:instance.catalog.tabs.securityDefiner}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.catalog.tabs.securityDefiner}} tab lists the functions declared SECURITY DEFINER, which run with the rights of their owner, with {{ui:instance.catalog.sd.runsAs}} and {{ui:instance.catalog.sd.executableBy}}. {{ui:instance.catalog.sd.executableBy}} reads every role (PUBLIC) when PUBLIC can run the function. The advisor rule `function.secdef_no_search_path` reports a function with no fixed search_path: its severity is critical when the owner is a superuser and PUBLIC can execute the function, otherwise warning.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.catalog.sd.runsAs}}',
                  meaning: 'The owner whose rights the function runs with.',
                },
                {
                  term: '{{ui:instance.catalog.sd.executableBy}}',
                  meaning:
                    'Who can run the function: every role (PUBLIC) when PUBLIC has the execute right.',
                  action:
                    'Revoke the execute right from PUBLIC, grant EXECUTE to the roles that need it, and fix the search path.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'SECURITY DEFINER functions and their pinned settings',
              code: 'SELECT n.nspname AS schema, p.proname AS function, pg_get_userbyid(p.proowner) AS owner, p.proconfig AS settings FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE p.prosecdef;',
            },
            {
              kind: 'sql',
              label: 'Revoke public execution of a function',
              code: 'REVOKE EXECUTE ON FUNCTION function_name FROM PUBLIC;',
            },
          ],
        },
        {
          id: 'foreign-keys',
          title: 'Unindexed foreign keys',
          blocks: [
            {
              kind: 'text',
              text: 'The Unindexed foreign keys tab lists the foreign keys without an index leading with their columns, with Constraint, Columns, References and Table size. The advisor rule `index.fk_unindexed` reports them.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Constraint',
                  meaning: 'The name of the foreign key constraint.',
                  action:
                    'Create an index on the referencing columns with `CREATE INDEX CONCURRENTLY`, so that writes are not blocked.',
                },
                {
                  term: 'Every foreign key has an index',
                  meaning:
                    'The empty state, with the description: No foreign key without an index leading with its columns was reported.',
                },
              ],
            },
          ],
        },
        {
          id: 'extensions',
          title: '{{ui:instance.catalog.tabs.extensions}}',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.catalog.tabs.extensions}} tab lists the extensions installed in each monitored database, with Extension, Schema, {{ui:instance.catalog.ext.installed}}, {{ui:instance.catalog.ext.packages}} and Update. The advisor rule `extension.upgrade_available` reports an extension with a newer version available.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.catalog.ext.installed}}',
                  meaning: 'The version of the extension installed in the database.',
                },
                {
                  term: '{{ui:instance.catalog.ext.packages}}',
                  meaning: 'The version available in the installed packages.',
                  action:
                    'Check the release notes of the extension before you upgrade it with `ALTER EXTENSION extension_name UPDATE;`.',
                },
                {
                  term: 'Update',
                  meaning:
                    'The update state: available when a newer version exists. The text The packages no longer carry it. means the packages no longer provide the installed version, and The versions cannot be compared. means the two versions cannot be compared.',
                },
              ],
            },
          ],
        },
        {
          id: 'postgis',
          title: 'Spatial columns',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:instance.catalog.tabs.postgis}} tab lists the spatial columns of each monitored database, with Column, Type, SRID, {{ui:instance.catalog.common.rowsEstimate}} and {{ui:instance.catalog.common.spatialIndex}}. Declared without typmod: each value carries its own SRID.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:instance.catalog.common.spatialIndex}}',
                  meaning:
                    'The index method of the spatial index on the column, or the value missing when there is none.',
                  action:
                    'Create a spatial index on the column with `CREATE INDEX CONCURRENTLY` when the table is large and the column is searched.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'logical',
      title: '{{ui:instance.section.logical}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:instance.section.logical}} tab shows the subscriptions of this instance, with {{ui:instance.logical.col.subscription}}, State, Database, {{ui:instance.logical.col.publications}}, Slot, {{ui:instance.logical.col.lastMessage}}, {{ui:instance.logical.col.errors}} and Conflicts, and the logical slots, with {{ui:instance.logical.col.decoded}}, {{ui:instance.overview.tile.transactions}}, {{ui:instance.logical.col.spilled}} and {{ui:instance.logical.col.streamed}}. The advisor rules `subscription.disabled`, `subscription.errors` and `subscription.not_receiving` report a disabled subscription, a subscription with errors, and an enabled subscription that is not receiving changes. When the instance has no logical replication, the empty state {{ui:instance.logical.noReplication}} appears.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'State',
              meaning:
                'The state of a subscription: running, not running, silent or disabled. A subscription that is not enabled shows disabled.',
              action:
                'For a subscription that is disabled, enable it with `ALTER SUBSCRIPTION subscription_name ENABLE;`. For one that is not receiving changes, check the publisher and the network between the two servers.',
            },
            {
              term: '{{ui:instance.logical.col.spilled}}',
              meaning:
                'The decoded changes that a logical slot writes to disk. The advisor rule `logical_slot.spill_high` flags a slot that writes about 1 MiB per second to disk.',
              action:
                'Raise `logical_decoding_work_mem` so that the decoding fits in memory, as the advisor suggests.',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Subscriptions and their enabled state',
          code: 'SELECT subname, subenabled FROM pg_subscription;',
        },
      ],
    },
    {
      id: 'readiness',
      title: '{{ui:instance.section.readiness}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:instance.section.readiness}} tab lists what is not set up on this server, so some pglens features show less data than they could, and how to close each gap. A warning means a pglens feature is lost or degraded on this server. A note means something is off by design or optional. Each gap shows What you lose and Evidence:, and a fix that requires a restart is marked Requires restart. When nothing is missing, the tab reads Everything pglens can use is set up on this server.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'What you lose',
              meaning:
                'The data or the feature that the gap takes away from pglens on this server.',
            },
            {
              term: 'Requires restart',
              meaning:
                'The fix changes a setting that is read only at start, so the agent or the server must restart afterwards. Without a restart the change has no effect.',
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
              term: 'Unknown',
              meaning:
                'A value is missing: the agent sent no value for it. The reason written beside Unknown says why.',
              action:
                'Check Last seen in the header: a Last seen that stops moving means the agent is not reporting. When the reason names a missing grant, grant it and wait for the next check.',
            },
            {
              term: '{{ui:instance.section.host}} collection is local-only.',
              meaning:
                'The note in the {{ui:instance.host.section}} section when the host is collected only locally. It says: {{ui:instance.section.host}} collection is local-only. Configure `host_local` in the agent configuration documentation.',
              action:
                'Set `host_local` in the agent configuration, as the documentation describes.',
            },
          ],
        },
      ],
    },
  ],
}
