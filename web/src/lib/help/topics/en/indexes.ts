import type { HelpTopic } from '../../types'

export const indexesTopic: HelpTopic = {
  id: 'indexes',
  group: 'monitoring',
  title: 'Index analysis',
  summary:
    'Everything about the indexes of a server: what exists, what is missing, what can be dropped, what vacuum leaves behind and what the indexes cost on every write. Each value is explained with how to read it and what to do.',
  page: {
    path: '/indexes',
    label: 'Open Index analysis',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'An index speeds up the reads that filter, join or sort on its columns, and slows down every insert, update and delete of its table, which must update the index too. Managing indexes means running two loops: create the indexes the workload is missing, and drop the ones it does not use.',
        },
        {
          kind: 'steps',
          items: [
            '[{{ui:indexes.page.tabMissing}}](/help/indexes#missing): create what is marked {{ui:indexes.lib.missing.label.create}}, test what is marked {{ui:indexes.lib.missing.label.test}}.',
            '[{{ui:indexes.page.tabCleanup}}](/help/indexes#cleanup): drop invalid, duplicate and redundant indexes, then the unused ones the advisor confirms.',
            '[{{ui:indexes.page.tabDeadRows}}](/help/indexes#dead-rows): check that vacuum keeps up on the busy tables, otherwise tables and indexes bloat.',
            '[{{ui:indexes.page.tabTax}}](/help/indexes#tax): on write-heavy tables keep only the indexes that earn their write cost.',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'pglens never changes your database. Every CREATE and DROP shown on these pages is a statement for you to review and run yourself, in psql, off-peak, one at a time.',
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
                  term: '{{ui:indexes.search.box}}',
                  meaning:
                    'Finds indexes, tables, columns and statements whose name, definition or text contains the term, in the selected database. Results are grouped as {{ui:indexes.search.groupIndexes}}, {{ui:indexes.search.groupTables}}, {{ui:indexes.search.groupColumns}} and {{ui:indexes.search.groupQueries}}; each one links to the page that shows it.',
                },
                {
                  term: 'Tabs',
                  meaning:
                    '{{ui:indexes.page.tabIndexes}}, {{ui:indexes.page.tabMissing}}, {{ui:indexes.page.tabCleanup}}, {{ui:indexes.page.tabDeadRows}}, {{ui:indexes.page.tabTax}}: each is explained below.',
                },
                {
                  term: '{{ui:indexes.page.server}}',
                  meaning:
                    'The PostgreSQL instance analysed. Statistics are per server: a standby has its own scan counters.',
                },
                {
                  term: '{{ui:indexes.page.database}}',
                  meaning:
                    'Limits every tab to one database; {{ui:indexes.page.allDatabases}} shows them together.',
                },
                {
                  term: 'Updated … ago',
                  meaning: 'Age of the data on screen. The page refreshes by itself.',
                },
              ],
            },
          ],
        },
        {
          id: 'data-sources',
          title: 'Where the numbers come from',
          blocks: [
            {
              kind: 'text',
              text: 'The agent reads `pg_stat_user_indexes`, `pg_statio_user_indexes`, `pg_stat_user_tables`, `pg_stat_statements` and the catalog every few minutes. A rate (per second) is computed over the last collection interval. PostgreSQL counters restart from zero at a statistics reset or after a crash.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.cleanup.basisAdvisor}}',
                  meaning:
                    'A verdict of the pglens advisor over a time window (unused, duplicate, invalid, foreign key without index…). It is listed on the [{{ui:shell.nav.findings}}](/findings) page too.',
                  action: 'Act on it after reading its remediation.',
                },
                {
                  term: 'Raw counter',
                  meaning:
                    'What PostgreSQL counted since its last reset (for example never scanned). It is a hint, not a verdict.',
                  action: 'Watch it; do not drop on it alone.',
                },
                {
                  term: 'Reported indexes',
                  meaning:
                    'The agent reports only the busiest and largest indexes of each database (`checks.index_stats.top_n`, 50 by default) and the page shows at most 500. When the list is incomplete the {{ui:indexes.page.tabIndexes}} tab says so: an index you do not see may still exist. Raise top_n in the agent configuration to see more.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'inventory',
      title: 'Indexes tab',
      blocks: [
        {
          kind: 'text',
          text: 'The inventory: one row per index. Use it to see what already exists on a table before adding an index, and to find the flagged ones. Click a row to expand it: definition, charts, findings and "What to do".',
        },
      ],
      subsections: [
        {
          id: 'inventory-columns',
          title: 'Columns',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.panel.colIndex}}, {{ui:indexes.panel.colTable}}, {{ui:indexes.panel.colColumns}}',
                  meaning:
                    'Name, table (database · schema.table) and key columns of the index. INCLUDE columns and the WHERE predicate are in the row details.',
                },
                {
                  term: '{{ui:indexes.panel.colMethod}}',
                  meaning:
                    'btree: equality, ranges and sorting (the default). gist: spatial data and ranges. gin: arrays, jsonb, full-text search. brin: very large tables written in order (time series). hash: equality only.',
                },
                {
                  term: '{{ui:indexes.panel.colFlags}}',
                  meaning:
                    'Labels on the index. Grey and outlined ones describe it (primary, unique, partial…); orange and red ones are problems. Problems come first. Hover a flag for its meaning and fix; all of them are explained in [Flags](/help/indexes#flags).',
                },
                {
                  term: '{{ui:indexes.panel.colSize}}',
                  meaning:
                    'Disk size of the index (`pg_relation_size`). Every byte is also in WAL, backups and replicas.',
                },
                {
                  term: '{{ui:indexes.panel.colScans}}',
                  meaning:
                    'Index scans per second over the last interval. {{ui:indexes.panel.scansNoneSinceReset}} means the counter is still 0.',
                  action:
                    'A big index at 0 for days is a drop candidate: see [{{ui:indexes.page.tabCleanup}}](/help/indexes#cleanup).',
                },
                {
                  term: '{{ui:indexes.panel.colCacheHit}}',
                  meaning:
                    'Share of the index block reads served from PostgreSQL memory (shared_buffers). Below 90% on a busy index means it does not fit in memory.',
                  action:
                    'Raise shared_buffers, or make the index smaller (partial index, fewer columns).',
                },
                {
                  term: '{{ui:indexes.panel.colLastScan}}',
                  meaning:
                    'When a query last used the index. {{ui:indexes.panel.lastScanNever}}: no scan since the counters were reset. PostgreSQL 16 and later only; older servers show {{ui:indexes.panel.notAvailable}}.',
                },
              ],
            },
          ],
        },
        {
          id: 'inventory-views',
          title: 'Views of the Indexes tab',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.view.inventory}}',
                  meaning: 'Every index, one per row (the default).',
                },
                {
                  term: '{{ui:indexes.view.summary}}',
                  meaning:
                    'Counts by kind, access method, database and table. The kind counts overlap: a partial unique index is both unique and partial.',
                },
                {
                  term: '{{ui:indexes.view.findings}}',
                  meaning:
                    'Every open advisor finding about indexes, with the detail and the remediation the advisor wrote.',
                },
                {
                  term: '{{ui:indexes.view.foreignKeys}}',
                  meaning:
                    'Foreign keys whose columns no index leads with. The ready-to-run commands are on [{{ui:indexes.page.tabMissing}}](/help/indexes#missing-foreign-keys).',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'flags',
      title: 'Flags, one by one',
      blocks: [
        {
          kind: 'text',
          text: 'The flags of the Indexes tab. Problems are listed first: each says what it means and exactly what to do.',
        },
      ],
      subsections: [
        {
          id: 'flags-problems',
          title: 'Problem flags',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'invalid',
                  meaning:
                    'A CREATE INDEX CONCURRENTLY or REINDEX CONCURRENTLY failed and left the index half built: queries never use it, yet every write still updates it.',
                  action:
                    'Drop it and create it again (both statements are in the row details), or run REINDEX INDEX CONCURRENTLY on it.',
                },
                {
                  term: 'unused',
                  meaning:
                    'The advisor saw no scan of this index, on this server and on its standbys, for the whole unused-index window (7 days by default).',
                  action:
                    'Make sure no rare job (month-end report, yearly batch) needs it, then drop it from the Cleanup tab. Keep its CREATE statement to undo.',
                },
                {
                  term: 'duplicate',
                  meaning:
                    'Another index of the same table has exactly the same definition: both are updated on every write, but only one is needed.',
                  action:
                    'Keep one (the Cleanup tab names which) and drop the other. If both are unique, drop the one that does not back a constraint.',
                },
                {
                  term: 'redundant prefix',
                  meaning:
                    'Its columns are the leading columns of a wider index, which can serve the same lookups.',
                  action:
                    'Drop it unless it is unique, much smaller and very busy. Check with EXPLAIN that the wider index is chosen.',
                },
                {
                  term: 'diverges',
                  meaning:
                    'The same table has different indexes on the servers of the cluster (for example on a logical replica).',
                  action:
                    'Create or drop the index so that every server that serves this table has the same set.',
                },
                {
                  term: 'never scanned',
                  meaning:
                    'No scan since the statistics were last reset. It is the raw PostgreSQL counter, not a verdict: the reset may be recent, or the index too small for the advisor to judge.',
                  action:
                    'Do not drop it yet: wait for the advisor to flag it unused, or check how old the counters are (pg_stat_database.stats_reset).',
                },
              ],
            },
          ],
        },
        {
          id: 'flags-kinds',
          title: 'Kind flags',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'primary',
                  meaning: 'It backs the primary key of the table.',
                  action: 'Nothing to do: never drop it.',
                },
                {
                  term: 'unique',
                  meaning:
                    'It enforces that no two rows share the key (a UNIQUE constraint or index).',
                  action:
                    'Keep it: it protects the data, not only the speed. Drop it only as the duplicate of another unique index.',
                },
                {
                  term: 'partial',
                  meaning: 'It covers only the rows that match its WHERE clause, so it is smaller.',
                  action:
                    'A query uses it only when its own WHERE implies the same condition: the predicate is in the row details.',
                },
                {
                  term: 'expression',
                  meaning:
                    'It indexes an expression, such as lower(email), instead of a plain column.',
                  action: 'A query uses it only when it writes exactly the same expression.',
                },
                {
                  term: 'include',
                  meaning:
                    'It carries extra INCLUDE columns, so a query can be answered from the index alone (index-only scan).',
                  action:
                    'Nothing to do. Keep the included columns few: each one makes every write heavier.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'missing',
      title: 'Missing indexes tab',
      blocks: [
        {
          kind: 'text',
          text: 'Where an index is missing, why, and the exact statement to create it. One list, best first, fed by four sources. Expand a row for the four steps {{ui:indexes.lib.missing.step.title.check}}, {{ui:indexes.lib.missing.step.title.create}}, {{ui:indexes.lib.missing.step.title.verify}} and {{ui:indexes.lib.missing.step.title.undo}}, each with its SQL.',
        },
      ],
      subsections: [
        {
          id: 'missing-recommendation',
          title: 'Recommendation',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.lib.missing.label.create}}',
                  meaning:
                    'Strong evidence: the statements that need it read much more than the table is written, or the referenced rows are being deleted on a large table.',
                  action: 'Create it off-peak with the command given.',
                },
                {
                  term: '{{ui:indexes.lib.missing.label.test}}',
                  meaning:
                    'It will most likely help, but the estimate is rough (for example column statistics are missing).',
                  action:
                    'Compare the plan before and after (on a copy, or create it and check it is used); undo if not.',
                },
                {
                  term: '{{ui:indexes.lib.missing.label.check}}',
                  meaning:
                    'It helps only if the workload does what the reason says: parent rows deleted, spatial searches, or slow statements with a small total.',
                  action:
                    'Run the {{ui:indexes.lib.missing.step.title.check}} step first, then decide.',
                },
                {
                  term: '{{ui:indexes.lib.missing.label.skip}}',
                  meaning:
                    'The writes the index would slow down outweigh the reads it would speed up.',
                  action: 'Do not create it.',
                },
              ],
            },
          ],
        },
        {
          id: 'missing-statements',
          title: 'Source: statements',
          blocks: [
            {
              kind: 'text',
              text: "pglens parses the statements of `pg_stat_statements` and finds the columns they filter (=, <, >, BETWEEN, LIKE 'abc%') or sort on, that no index leads with. Equality columns come first in the suggested key, then the range or sort column.",
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.missing.readCost}}',
                  meaning:
                    'Milliseconds per second those statements spend today: the most an index could save (an upper bound).',
                },
                {
                  term: '{{ui:indexes.missing.weight}}',
                  meaning: 'The same time as a share of all the statement time of the database.',
                },
                {
                  term: '{{ui:indexes.missing.verdict}} and confidence',
                  meaning:
                    '{{ui:indexes.missing.verdict}} compares calls per second with index writes per second of the table: 10 or more strong, 1 or more moderate, below 1 write-heavy. A first column with 20 distinct values or fewer lowers it one step. {{ui:indexes.missing.confidence}} is low when column statistics or sizes are missing.',
                },
                {
                  term: '{{ui:indexes.missing.analysisWindow}}',
                  meaning:
                    'The period of statement statistics used: 1 hour, 6 hours, 24 hours (default) or 7 days.',
                },
                {
                  term: 'Coverage line',
                  meaning:
                    'How many statements were analysed and why the others were skipped (not a SELECT, parse error, catalog tables, small tables…). A statement that cannot be parsed can still show up as a full table scan.',
                },
              ],
            },
          ],
        },
        {
          id: 'missing-validate',
          title: 'Validating a suggestion with hypopg',
          blocks: [
            {
              kind: 'text',
              text: '{{ui:indexes.missing.validate.button}} asks the agent to cost the heaviest statement of the suggestion twice: as it is, and with a hypothetical index on the suggested key created by the `hypopg` extension. The hypothetical index lives only in the agent session, inside a read-only transaction that is rolled back; nothing is created on the database and no lock is taken on the table.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.lib.validation.wouldUse}}',
                  meaning:
                    'The plan with the hypothetical index scans it. {{ui:indexes.lib.validation.wouldNotUse}} means the planner still prefers another path: the index would cost writes without helping this statement.',
                },
                {
                  term: '{{ui:indexes.missing.validate.cost}}',
                  meaning:
                    'Planner total cost before and after, in planner units (not milliseconds), with their ratio. A generic plan is used on PostgreSQL 16 and later, a recent execution sample before.',
                },
                {
                  term: '{{ui:indexes.missing.validate.size}}',
                  meaning: 'The size hypopg estimates for the index.',
                },
              ],
            },
            {
              kind: 'text',
              text: 'It needs tier T1 and the extension in the database: `CREATE EXTENSION hypopg;` (pglens never installs it). A key with a descending column is costed in ascending order, so the result is marked approximate. The last result is kept and shown again on the suggestion.',
            },
          ],
        },
        {
          id: 'missing-foreign-keys',
          title: 'Source: foreign keys',
          blocks: [
            {
              kind: 'text',
              text: 'PostgreSQL does not index the referencing columns of a foreign key by itself. Without an index, every DELETE of a referenced (parent) row, and every change of its key, scans the whole child table looking for rows that point to it, holding locks meanwhile. On a big child table this makes deletes slow and blocks other sessions.',
            },
            {
              kind: 'text',
              text: '{{ui:indexes.missing.recommendation}}: {{ui:indexes.lib.missing.label.create}} when parent rows are being deleted now and the child table is 10 MiB or larger; {{ui:indexes.lib.missing.label.test}} when they are deleted and the table is smaller; {{ui:indexes.lib.missing.label.check}} when no delete is seen. The index also speeds up joins on those columns.',
            },
          ],
        },
        {
          id: 'missing-spatial',
          title: 'Source: spatial columns',
          blocks: [
            {
              kind: 'text',
              text: 'A PostGIS geometry or geography column with no GiST index. Every spatial search on it (`ST_Intersects`, `ST_DWithin`, `ST_Contains`, `&&`) reads the whole table. The index is useless if nobody searches the column spatially, and it can be large, so the recommendation is {{ui:indexes.lib.missing.label.check}}: the {{ui:indexes.lib.missing.step.title.check}} step lists the statements that use the column.',
            },
          ],
        },
        {
          id: 'missing-scans',
          title: 'Source: full table scans',
          blocks: [
            {
              kind: 'text',
              text: 'Tables of 8 MiB or more that sequential scans read at 100 rows per second or more, and that no other source already covers. Some statement filters them on a column no index serves, but pglens could not parse it. The row gives no command: its steps show how to find the statements and read their plan to choose the columns.',
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'A sequential scan is the right plan for a small table, or for a query that reads most of a table. Index only what queries filter down to a few rows.',
            },
          ],
        },
        {
          id: 'missing-act',
          title: 'How to act on a row',
          blocks: [
            {
              kind: 'steps',
              items: [
                '{{ui:indexes.lib.missing.step.title.check}}: run the check query and keep its output.',
                '{{ui:indexes.lib.missing.step.title.create}}: run the CREATE INDEX CONCURRENTLY statement in psql, off-peak, alone (not inside BEGIN … COMMIT). It does not block reads or writes but takes longer, and needs free disk about the size of the index.',
                '{{ui:indexes.lib.missing.step.title.verify}}: run the verify query. The plan must show an Index Scan or Bitmap Index Scan on the new index; after a day the index must have scans.',
                '{{ui:indexes.lib.missing.step.title.undo}}: if the plan does not change, or writes slow down, drop the index with the Undo statement.',
              ],
            },
            {
              kind: 'note',
              tone: 'warning',
              text: 'If CREATE INDEX CONCURRENTLY fails (deadlock, unique violation, cancel) it leaves an INVALID index that is still written on every change. Drop it with DROP INDEX CONCURRENTLY and run the statement again.',
            },
            {
              kind: 'sql',
              label: 'Follow a running index build (PostgreSQL 12+)',
              code: 'SELECT pid, phase, blocks_done, blocks_total, tuples_done, tuples_total\nFROM pg_stat_progress_create_index;',
            },
            {
              kind: 'sql',
              label: 'Find invalid indexes left by a failed build',
              code: 'SELECT indexrelid::regclass AS index, indrelid::regclass AS table\nFROM pg_index WHERE NOT indisvalid;',
            },
          ],
        },
        {
          id: 'missing-script',
          title: 'Copy script',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:indexes.missing.script}} copy button in the tab header copies a psql script of every row of the list (for the database picked on the page), grouped by database with `\\connect`. {{ui:indexes.lib.missing.label.create}} and {{ui:indexes.lib.missing.label.test}} rows are active; {{ui:indexes.lib.missing.label.check}} rows are commented out, to enable one by one after their check; {{ui:indexes.lib.missing.label.skip}} rows and {{ui:indexes.lib.missing.source.scans}} rows (no command) are left out.',
            },
          ],
        },
      ],
    },
    {
      id: 'cleanup',
      title: 'Cleanup tab',
      blocks: [
        {
          kind: 'text',
          text: 'Indexes that look removable, with the space a drop gives back and the writes it saves. Primary keys are never listed; a unique index is listed only as the duplicate of another unique or primary index, which keeps the column unique.',
        },
      ],
      subsections: [
        {
          id: 'cleanup-reasons',
          title: 'Reasons',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'invalid',
                  meaning:
                    'A CREATE INDEX CONCURRENTLY or REINDEX CONCURRENTLY failed and left the index half built: queries never use it, yet every write still updates it.',
                  action:
                    'Drop it and create it again (both statements are in the row details), or run REINDEX INDEX CONCURRENTLY on it.',
                },
                {
                  term: 'duplicate',
                  meaning: 'Same definition as an index that stays (named in the row).',
                  action:
                    'Drop it: queries keep using the other one. If DROP INDEX fails with "constraint requires it", drop the constraint instead (ALTER TABLE … DROP CONSTRAINT).',
                },
                {
                  term: 'redundant prefix',
                  meaning:
                    'Its columns are the leading columns of a wider index, which can serve the same lookups.',
                  action:
                    'Drop it unless it is unique, much smaller and very busy. Check with EXPLAIN that the wider index is chosen.',
                },
                {
                  term: 'unused',
                  meaning:
                    'The advisor saw no scan of this index, on this server and on its standbys, for the whole unused-index window (7 days by default).',
                  action:
                    'Make sure no rare job (month-end report, yearly batch) needs it, then drop it from the Cleanup tab. Keep its CREATE statement to undo.',
                },
                {
                  term: 'never scanned',
                  meaning:
                    'No scan since the statistics were last reset. It is the raw PostgreSQL counter, not a verdict: the reset may be recent, or the index too small for the advisor to judge.',
                  action:
                    'Do not drop it yet: wait for the advisor to flag it unused, or check how old the counters are (pg_stat_database.stats_reset).',
                },
              ],
            },
          ],
        },
        {
          id: 'cleanup-figures',
          title: 'Figures and columns',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:indexes.cleanup.spaceBack}}',
                  meaning:
                    'Total size of the candidates the advisor stands behind (invalid, duplicate, redundant, unused). Never-scanned candidates rest on raw counters only and are counted apart, in their own figure.',
                },
                {
                  term: '{{ui:indexes.cleanup.indexWrites}}',
                  meaning:
                    'Entries per second that inserts and non-HOT updates add to the index: what a drop saves on every write.',
                },
                {
                  term: '{{ui:indexes.cleanup.basis}}',
                  meaning:
                    '{{ui:indexes.cleanup.basisAdvisor}}: a verdict over the window. {{ui:indexes.cleanup.basisStatistics}}: only PostgreSQL counters since their reset.',
                },
                {
                  term: 'Script',
                  meaning:
                    'All drops in one script: advisor-backed ones active, raw-counter ones commented out, and the rebuild statements at the end to undo.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'How old are the statistics counters?',
              code: 'SELECT datname, stats_reset FROM pg_stat_database WHERE datname = current_database();',
            },
            {
              kind: 'sql',
              label: 'Drop without blocking writes (not inside a transaction)',
              code: 'DROP INDEX CONCURRENTLY IF EXISTS schema_name.index_name;',
            },
          ],
        },
      ],
    },
    {
      id: 'dead-rows',
      title: 'Dead rows tab',
      blocks: [
        {
          kind: 'text',
          text: 'Every UPDATE and DELETE leaves the old row version (a dead row) in the table until vacuum removes it. Dead rows bloat the table and every index on it and slow down scans. This tab ranks tables by dead rows so you can see where vacuum is behind.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:indexes.deadRows.deadRows}}',
              meaning:
                'Old row versions not removed yet, with the estimated space (heap size × dead share).',
            },
            {
              term: '{{ui:indexes.deadRows.deadLive}}',
              meaning:
                'Dead rows divided by live rows. Above 20% vacuum is behind; above 100% the table holds more garbage than data.',
              action: 'Run VACUUM (VERBOSE) on the table and read what it could not remove.',
            },
            {
              term: '{{ui:indexes.deadRows.vacuumDue}}',
              meaning:
                'Dead rows divided by the count at which autovacuum starts (50 + 20% of the rows by default). 1.0× or more with no recent vacuum means autovacuum cannot keep up or is blocked.',
              action:
                'Look for long transactions or replication slots holding back the cleanup (see the SQL below), then lower the table scale factor.',
            },
            {
              term: '{{ui:indexes.deadRows.lastVacuum}}',
              meaning:
                'The latest manual or automatic vacuum; {{ui:indexes.deadRows.never}} if none is recorded.',
            },
            {
              term: 'HOT',
              meaning:
                'Share of updates that wrote no index entry (heap-only tuples). Low on an update-heavy table means the updated columns are indexed or the pages are full.',
              action: 'Drop indexes on often-updated columns, or set fillfactor = 90 on the table.',
            },
            {
              term: '{{ui:indexes.deadRows.writes}}, {{ui:indexes.deadRows.trend}}',
              meaning:
                'Inserts + updates + deletes per second, and the dead rows over the selected time range (10 worst tables only).',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'The agent reports the tables with the most dead rows first, up to `checks.table_stats.top_n` per database (50 by default). When the list is incomplete the tab says so: the worst tables are always there, the long tail is not.',
        },
        {
          kind: 'sql',
          label: 'What holds vacuum back: oldest open transactions',
          code: 'SELECT pid, now() - xact_start AS age, state, left(query, 80) AS query\nFROM pg_stat_activity\nWHERE xact_start IS NOT NULL\nORDER BY xact_start LIMIT 10;',
        },
        {
          kind: 'sql',
          label: 'Make autovacuum start sooner on one big table',
          code: 'ALTER TABLE schema_name.table_name SET (autovacuum_vacuum_scale_factor = 0.02);',
        },
      ],
    },
    {
      id: 'tax',
      title: 'Index tax tab',
      blocks: [
        {
          kind: 'text',
          text: 'What the indexes of each table cost: how many there are, their size against the table, the space that looks unused, and the index entries that every write adds (write amplification). Every figure is an estimate.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:indexes.tax.indexes}}',
              meaning:
                'Number of indexes on the table. More than 6–8 on a write-heavy table deserves a review.',
            },
            {
              term: '{{ui:indexes.tax.indexSize}}, {{ui:indexes.tax.tableSize}}, {{ui:indexes.tax.ratio}}',
              meaning:
                'Combined index size, heap size (without TOAST) and their ratio. Above 100% the indexes are bigger than the data.',
              action:
                'Open {{ui:indexes.page.tabCleanup}} filtered on the table: look for duplicates, redundant prefixes and unused indexes.',
            },
            {
              term: '{{ui:indexes.tax.unusedSize}}',
              meaning:
                'Space of indexes with no scan: advisor verdicts and raw counters, labelled apart. Unique and primary indexes are never counted.',
            },
            {
              term: '{{ui:indexes.tax.amplification}}, {{ui:indexes.tax.writes}}',
              meaning:
                'Inserts, deletes and non-HOT updates per second, times the number of indexes: the index entries the writes add.',
              action:
                'On a table with high amplification and few reads, every index must earn its place.',
            },
          ],
        },
      ],
    },
  ],
}
