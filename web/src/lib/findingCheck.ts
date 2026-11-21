import { isTruncatedQuery } from './format'
import type { FindingRecord } from './findings'
import { findingSubject, type FindingSubject } from './findingSubject'

/*
 * How to confirm a finding on the database itself: the steps and a read-only
 * SQL script, filled in with the finding's database, statement or object.
 * Every script runs inside BEGIN TRANSACTION READ ONLY … ROLLBACK and reads
 * only catalogs and statistics views; nothing here changes the database.
 */

export interface FindingCheck {
  /** The database to connect to; null when any database of the instance does. */
  database: string | null
  steps: string[]
  sql: string
}

type CheckSource = Pick<
  FindingRecord,
  'rule_id' | 'datname' | 'object_name' | 'instance_id' | 'cluster_id' | 'query_text' | 'scope'
>

interface Context {
  db: string | null
  subject: FindingSubject
  /** The statement's queryid, or null. */
  queryid: string | null
  /** The object as a quoted SQL literal of its qualified name, or null. */
  rel: string | null
}

/** A SQL string literal. */
export function sqlLiteral(value: string): string {
  return `'${value.replace(/'/g, "''")}'`
}

function quoteIdent(part: string): string {
  return /^[a-z_][a-z0-9_$]*$/.test(part) ? part : `"${part.replace(/"/g, '""')}"`
}

/** `schema.name` as the literal of its quoted form, for `::regclass`. */
function relationLiteral(name: string): string | null {
  const dot = name.indexOf('.')
  if (dot <= 0 || dot === name.length - 1 || /\s/.test(name)) return null
  return sqlLiteral(`${quoteIdent(name.slice(0, dot))}.${quoteIdent(name.slice(dot + 1))}`)
}

function script(...parts: string[]): string {
  return ['BEGIN TRANSACTION READ ONLY;', '', ...parts, 'ROLLBACK;'].join('\n')
}

function block(comment: string, sql: string): string {
  return `-- ${comment}\n${sql}\n`
}

const settings = (...names: string[]) =>
  `SELECT name, setting, unit, source, pending_restart\n  FROM pg_settings\n WHERE name IN (${names.map(sqlLiteral).join(', ')})\n ORDER BY name;`

// ---- statements -----------------------------------------------------------

function statementStats(ctx: Context): string {
  const db = ctx.db ? sqlLiteral(ctx.db) : 'current_database()'
  return block(
    'The statement in pg_stat_statements (since its last reset); run it in a database where the extension is installed.',
    `SELECT calls,
       round(mean_exec_time::numeric, 1)  AS mean_ms,
       round(total_exec_time::numeric, 1) AS total_ms,
       rows, shared_blks_hit, shared_blks_read, temp_blks_written,
       query
  FROM pg_stat_statements
 WHERE queryid = ${ctx.queryid}
   AND dbid = (SELECT oid FROM pg_database WHERE datname = ${db});`,
  )
}

function explainBlock(subject: FindingSubject): string {
  if (subject.kind !== 'statement' || subject.text === null) {
    return block(
      'The statement text is no longer kept: take it from your application or the server log, then',
      'EXPLAIN <statement>;',
    )
  }
  if (isTruncatedQuery(subject.text)) {
    return block(
      'The text kept is cut at track_activity_query_size: take the full statement from your application or the server log, then',
      'EXPLAIN <statement>;',
    )
  }
  const text = subject.text.trim().replace(/;\s*$/, '')
  const params = Math.max(0, ...[...text.matchAll(/\$(\d+)/g)].map((m) => Number(m[1])))
  if (params === 0) {
    return block(
      'The plan the planner picks now, without running the statement.',
      `EXPLAIN\n${text};`,
    )
  }
  return block(
    'The plan the planner picks now for any value of the $n parameters (a generic plan), without running the statement; PREPARE only plans it, DEALLOCATE drops it.',
    `SET LOCAL plan_cache_mode = force_generic_plan;
PREPARE pglens_check AS
${text};
EXPLAIN EXECUTE pglens_check(${Array.from({ length: params }, () => 'NULL').join(', ')});
DEALLOCATE pglens_check;`,
  )
}

const staleStats = block(
  'Statistics of the tables the plan reads: many changes since the last analyze make the planner guess.',
  `SELECT schemaname, relname, n_live_tup, n_mod_since_analyze,
       last_analyze, last_autoanalyze
  FROM pg_stat_user_tables
 ORDER BY n_mod_since_analyze DESC
 LIMIT 10;`,
)

function statementCheck(ruleId: string, ctx: Context): FindingCheck | null {
  if (ctx.queryid === null) return null
  const stats = statementStats(ctx)
  const explain = explainBlock(ctx.subject)
  switch (ruleId) {
    case 'query.regression':
      return {
        database: ctx.db,
        steps: [
          'Read the statement in pg_stat_statements: mean_ms is its mean since the last reset; the finding compares the recent mean with the mean of the previous seven days.',
          'Compare the current plan (EXPLAIN below) with the plans kept in Query performance › Plan history: a different join order, a sequential scan instead of an index scan or a lost index usually explain a regression.',
          'Check whether the tables it reads changed a lot since their last analyze: stale statistics lead the planner to a worse plan.',
          'EXPLAIN ANALYZE runs the statement: use it only for a SELECT, ideally on a standby, and only inside the read-only transaction.',
        ],
        sql: script(stats, explain, staleStats),
      }
    case 'query.slow_mean':
      return {
        database: ctx.db,
        steps: [
          'Read the statement in pg_stat_statements: mean_ms and calls tell how slow and how frequent it is.',
          'Read its plan: look for sequential scans of large tables, sorts or hashes spilling to disk and nested loops over many rows.',
          'temp_blks_written above zero means it spills to disk; shared_blks_read much higher than shared_blks_hit means it reads from disk.',
        ],
        sql: script(stats, explain),
      }
    case 'query.total_time_share':
      return {
        database: ctx.db,
        steps: [
          'The top statements by total execution time show the share this one takes.',
          'A high share with a low mean time comes from the number of calls: check whether the application runs it more often than needed.',
          'A high share with a high mean time comes from the statement itself: read its plan.',
        ],
        sql: script(
          stats,
          block(
            'The ten statements that take the most execution time, with their share.',
            `SELECT queryid, calls,
       round(total_exec_time::numeric) AS total_ms,
       round((100 * total_exec_time / sum(total_exec_time) OVER ())::numeric, 1) AS share_pct,
       left(query, 80) AS query
  FROM pg_stat_statements
 ORDER BY total_exec_time DESC
 LIMIT 10;`,
          ),
          explain,
        ),
      }
    case 'query.temp_bytes_high':
      return {
        database: ctx.db,
        steps: [
          'temp_per_call is what one call writes to temporary files on average; compare it with work_mem.',
          'In the plan, a Sort or Hash node that spills shows "Sort Method: external merge" or "Batches" above 1 in EXPLAIN ANALYZE.',
          'Raising work_mem for this statement alone (SET LOCAL in its transaction) is safer than raising it for the whole instance.',
        ],
        sql: script(
          block(
            'Temporary bytes this statement writes per call, and the work_mem in force.',
            `SELECT calls, temp_blks_written,
       pg_size_pretty(temp_blks_written * current_setting('block_size')::bigint / NULLIF(calls, 0)) AS temp_per_call,
       current_setting('work_mem') AS work_mem,
       query
  FROM pg_stat_statements
 WHERE queryid = ${ctx.queryid};`,
          ),
          explain,
        ),
      }
    case 'query.cache_miss_high':
      return {
        database: ctx.db,
        steps: [
          'hit_pct is the share of blocks found in shared buffers; a low value means the statement reads from disk.',
          'Compare the size of the tables it reads with shared_buffers: a scan of a table larger than the cache always reads from disk.',
          'An index that narrows the rows read usually helps more than a larger cache.',
        ],
        sql: script(
          block(
            'Blocks found in shared buffers and read from disk by this statement.',
            `SELECT calls, shared_blks_hit, shared_blks_read,
       round(100.0 * shared_blks_hit / NULLIF(shared_blks_hit + shared_blks_read, 0), 1) AS hit_pct,
       current_setting('shared_buffers') AS shared_buffers,
       query
  FROM pg_stat_statements
 WHERE queryid = ${ctx.queryid};`,
          ),
          explain,
        ),
      }
  }
  return {
    database: ctx.db,
    steps: ['Read the statement in pg_stat_statements and its plan.'],
    sql: script(stats, explain),
  }
}

// ---- tables and indexes ---------------------------------------------------

const xminHolders = block(
  'Sessions whose snapshot keeps dead rows from being removed (oldest first).',
  `SELECT pid, usename, application_name, state, xact_start,
       age(backend_xmin) AS xmin_age, left(query, 80) AS query
  FROM pg_stat_activity
 WHERE backend_xmin IS NOT NULL
 ORDER BY age(backend_xmin) DESC
 LIMIT 5;`,
)

function tableStats(rel: string): string {
  return block(
    'Row counts, vacuum and analyze history of the table.',
    `SELECT schemaname, relname, n_live_tup, n_dead_tup,
       round(100.0 * n_dead_tup / NULLIF(n_live_tup + n_dead_tup, 0), 1) AS dead_pct,
       n_mod_since_analyze, last_vacuum, last_autovacuum, last_analyze, last_autoanalyze,
       autovacuum_count, autoanalyze_count,
       pg_size_pretty(pg_total_relation_size(relid)) AS total_size
  FROM pg_stat_user_tables
 WHERE relid = ${rel}::regclass;`,
  )
}

function relOptions(rel: string): string {
  return block(
    'Per-table settings (an autovacuum_enabled=false here turns autovacuum off for this table).',
    `SELECT oid::regclass AS relation, reloptions FROM pg_class WHERE oid = ${rel}::regclass;`,
  )
}

function indexDefinition(rel: string): string {
  return block(
    'The index, its definition, size, scans and whether it enforces a constraint.',
    `SELECT s.relname AS table_name, s.indexrelname AS index_name,
       pg_get_indexdef(s.indexrelid) AS definition,
       pg_size_pretty(pg_relation_size(s.indexrelid)) AS size,
       s.idx_scan, i.indisunique, i.indisprimary, i.indisvalid
  FROM pg_stat_user_indexes s
  JOIN pg_index i USING (indexrelid)
 WHERE s.indexrelid = ${rel}::regclass;`,
  )
}

function tableIndexes(tableExpr: string): string {
  return block(
    'Every index of the table with its definition and size.',
    `SELECT indexrelid::regclass AS index_name,
       pg_get_indexdef(indexrelid) AS definition,
       pg_size_pretty(pg_relation_size(indexrelid)) AS size,
       indisunique, indisprimary
  FROM pg_index
 WHERE indrelid = ${tableExpr}
 ORDER BY 2;`,
  )
}

const statsReset = block(
  'Since when the statistics count (a recent reset makes every counter small).',
  'SELECT stats_reset FROM pg_stat_database WHERE datname = current_database();',
)

function relationCheck(ruleId: string, ctx: Context): FindingCheck | null {
  const rel = ctx.rel
  const db = ctx.db
  switch (ruleId) {
    case 'table.dead_tuples_high':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'dead_pct is the share of dead rows; last_autovacuum tells when autovacuum last cleaned the table.',
          'If autovacuum ran but the dead rows stay, a long transaction or an idle-in-transaction session holds an old snapshot: see the sessions below.',
          'If autovacuum has not run for long, check its per-table settings and Autovacuum workers busy on other tables.',
        ],
        sql: script(tableStats(rel), xminHolders, relOptions(rel)),
      }
    case 'table.never_autovacuumed':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'last_autovacuum empty and autovacuum_count 0 confirm it; n_dead_tup tells whether the table needs it.',
          'A table changed rarely can legitimately never reach the autovacuum threshold; a busy one points to autovacuum disabled for it (reloptions) or for the instance.',
        ],
        sql: script(
          tableStats(rel),
          relOptions(rel),
          settings(
            'autovacuum',
            'track_counts',
            'autovacuum_vacuum_threshold',
            'autovacuum_vacuum_scale_factor',
          ),
        ),
      }
    case 'table.autoanalyze_stale':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'n_mod_since_analyze is the rows changed since the last analyze; compared with n_live_tup it tells how stale the statistics are.',
          'Stale statistics lead the planner to wrong row estimates: compare estimated and actual rows in EXPLAIN ANALYZE of a query on this table.',
        ],
        sql: script(
          tableStats(rel),
          settings('autovacuum_analyze_threshold', 'autovacuum_analyze_scale_factor'),
          relOptions(rel),
        ),
      }
    case 'table.wraparound_risk':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'xid_age is how far the table is from its last freeze; at autovacuum_freeze_max_age autovacuum forces an aggressive vacuum, at about 2 billion the instance stops accepting writes.',
          'A vacuum already working on it appears in pg_stat_progress_vacuum; if none runs, look for what blocks it (long transactions, prepared transactions, replication slots).',
        ],
        sql: script(
          block(
            'Transaction id and multixact age of the table.',
            `SELECT oid::regclass AS table_name,
       age(relfrozenxid) AS xid_age, mxid_age(relminmxid) AS mxid_age,
       current_setting('autovacuum_freeze_max_age') AS freeze_max_age
  FROM pg_class
 WHERE oid = ${rel}::regclass;`,
          ),
          block(
            'A vacuum running on the table now.',
            `SELECT pid, phase, heap_blks_scanned, heap_blks_total\n  FROM pg_stat_progress_vacuum\n WHERE relid = ${rel}::regclass;`,
          ),
          xminHolders,
        ),
      }
    case 'table.seq_scan_heavy':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'seq_scan and seq_tup_read against idx_scan show how the table is read; on a large table many sequential scans are expensive.',
          'Find the statements that read it (second query) and read their plan: a missing index on the filtered columns is the usual cause.',
        ],
        sql: script(
          block(
            'How the table is read.',
            `SELECT seq_scan, seq_tup_read, idx_scan, n_live_tup,
       pg_size_pretty(pg_relation_size(relid)) AS size
  FROM pg_stat_user_tables
 WHERE relid = ${rel}::regclass;`,
          ),
          block(
            'Statements that name the table, by total time (needs pg_stat_statements).',
            `SELECT queryid, calls, round(mean_exec_time::numeric, 1) AS mean_ms, left(query, 100) AS query
  FROM pg_stat_statements
 WHERE query ILIKE ${sqlLiteral(`%${(ctx.subject.kind === 'object' ? ctx.subject.name : '').split('.').pop() ?? ''}%`)}
 ORDER BY total_exec_time DESC
 LIMIT 10;`,
          ),
        ),
      }
    case 'table.bloat_high':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'The advisor estimates bloat from statistics; compare the table size with its live rows.',
          'For a measured value use pgstattuple_approx (extension pgstattuple): it reads the table, so run it off-peak.',
          'Bloat comes back unless dead rows are vacuumed in time: check dead rows and autovacuum history too.',
        ],
        sql: script(
          tableStats(rel),
          block(
            'Measured free and dead space (needs the pgstattuple extension; reads the table).',
            `-- SELECT * FROM pgstattuple_approx(${rel}::regclass);`,
          ),
        ),
      }
    case 'index.bloat_high':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'The advisor estimates bloat from statistics; pgstatindex (extension pgstattuple) measures it: avg_leaf_density far below 90 means a bloated B-tree.',
          'REINDEX CONCURRENTLY rebuilds it without blocking writes.',
        ],
        sql: script(
          indexDefinition(rel),
          block(
            'Measured density (needs the pgstattuple extension; reads the index).',
            `-- SELECT * FROM pgstatindex(${rel}::regclass);`,
          ),
        ),
      }
    case 'index.unused':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'idx_scan is the scans since the statistics reset: check stats_reset, a recent reset makes every index look unused.',
          'Scans on a standby are counted only there: run the same query on every standby before dropping it.',
          'An index that enforces a unique or primary key constraint (indisunique, indisprimary) is needed even without scans.',
        ],
        sql: script(indexDefinition(rel), statsReset),
      }
    case 'index.invalid':
    case 'partition.index_invalid':
      return {
        database: db,
        steps: [
          'indisvalid false: a CREATE INDEX CONCURRENTLY or REINDEX CONCURRENTLY failed or is still running; the planner ignores the index but writes still maintain it.',
          'A build in progress shows in pg_stat_progress_create_index; if none runs, drop the index and create it again.',
        ],
        sql: script(
          block(
            'Invalid indexes of this database.',
            `SELECT indexrelid::regclass AS index_name, indrelid::regclass AS table_name,
       pg_get_indexdef(indexrelid) AS definition
  FROM pg_index
 WHERE NOT indisvalid;`,
          ),
          block(
            'Index builds running now.',
            'SELECT pid, relid::regclass, index_relid::regclass, phase FROM pg_stat_progress_create_index;',
          ),
        ),
      }
    case 'index.duplicate':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'Indexes with the same definition (same columns, method and predicate) do the same work twice: every write maintains both.',
          'Keep the one that backs a constraint, if any, and drop the other.',
        ],
        sql: script(tableIndexes(`${rel}::regclass`)),
      }
    case 'index.redundant_prefix':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'An index whose columns are the leading columns of another index is covered by it for most queries.',
          'Check that it is not unique and that its smaller size does not matter to a hot query before dropping it.',
        ],
        sql: script(
          tableIndexes(`(SELECT indrelid FROM pg_index WHERE indexrelid = ${rel}::regclass)`),
        ),
      }
    case 'index.divergence':
      if (!rel) return null
      return {
        database: db,
        steps: [
          'Run the query on this instance and on the instance named in the finding: the definitions differ or the index exists on one only.',
          'A failover to the other instance changes the plans of the queries that use it.',
        ],
        sql: script(
          block(
            'The index definition on this instance.',
            `SELECT pg_get_indexdef(${rel}::regclass);`,
          ),
        ),
      }
    case 'index.fk_unindexed':
      return {
        database: db,
        steps: [
          'A foreign key without an index on its columns makes every delete or key update of the referenced table scan the referencing table.',
          'Create an index on the foreign key columns (CREATE INDEX CONCURRENTLY).',
        ],
        sql: script(
          block(
            'Foreign keys whose columns no index starts with.',
            `SELECT c.conrelid::regclass AS table_name, c.conname,
       pg_get_constraintdef(c.oid) AS definition
  FROM pg_constraint c
 WHERE c.contype = 'f'
   AND NOT EXISTS (
         SELECT 1 FROM pg_index i
          WHERE i.indrelid = c.conrelid
            AND (i.indkey::int2[])[0:cardinality(c.conkey) - 1] @> c.conkey)
 ORDER BY 1, 2;`,
          ),
        ),
      }
  }
  return null
}

// ---- catalog --------------------------------------------------------------

function catalogCheck(ruleId: string, ctx: Context): FindingCheck | null {
  const db = ctx.db
  const one = (steps: string[], ...parts: string[]): FindingCheck => ({
    database: db,
    steps,
    sql: script(...parts),
  })
  switch (ruleId) {
    case 'partition.default_has_rows':
      return one(
        [
          'Rows in a default partition belong to no declared range: attaching a partition for them later must scan and move them.',
        ],
        block(
          'Default partitions and their estimated rows.',
          `SELECT p.partrelid::regclass AS parent, p.partdefid::regclass AS default_partition,
       c.reltuples::bigint AS estimated_rows
  FROM pg_partitioned_table p
  JOIN pg_class c ON c.oid = p.partdefid;`,
        ),
      )
    case 'partition.future_missing':
      return one(
        [
          'The last partition bounds show how far ahead partitions exist; rows past the last bound go to the default partition or fail.',
        ],
        block(
          'Partitions of each partitioned table with their bounds.',
          `SELECT i.inhparent::regclass AS parent, c.oid::regclass AS partition_name,
       pg_get_expr(c.relpartbound, c.oid) AS bounds
  FROM pg_inherits i
  JOIN pg_class c ON c.oid = i.inhrelid
  JOIN pg_partitioned_table p ON p.partrelid = i.inhparent
 ORDER BY 1, 3;`,
        ),
      )
    case 'partition.parent_not_analyzed':
      return one(
        [
          'Autovacuum never analyzes a partitioned parent: without ANALYZE on the parent, queries across partitions plan on default estimates.',
        ],
        block(
          'Partitioned tables and their last analyze.',
          `SELECT c.oid::regclass AS parent, s.last_analyze, s.last_autoanalyze
  FROM pg_class c
  LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
 WHERE c.relkind = 'p';`,
        ),
      )
    case 'sequence.exhaustion_risk':
      return one(
        [
          'used_pct is how much of its range the sequence has used; at 100% nextval fails and inserts stop.',
        ],
        block(
          'Sequences by used share of their range.',
          `SELECT schemaname, sequencename, data_type, last_value, max_value,
       round(100.0 * last_value / max_value, 1) AS used_pct
  FROM pg_sequences
 ORDER BY used_pct DESC NULLS LAST
 LIMIT 10;`,
        ),
      )
    case 'sequence.column_narrower':
      return one(
        [
          'A sequence wider than the column it fills fails on insert when its value outgrows the column type.',
        ],
        block(
          'Columns fed by a sequence, with both types.',
          `SELECT d.refobjid::regclass AS table_name, a.attname AS column_name,
       format_type(a.atttypid, a.atttypmod) AS column_type,
       s.seqrelid::regclass AS sequence_name, s.seqtypid::regtype AS sequence_type
  FROM pg_depend d
  JOIN pg_sequence s ON s.seqrelid = d.objid
  JOIN pg_attribute a ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
 WHERE d.classid = 'pg_class'::regclass
   AND d.refclassid = 'pg_class'::regclass
   AND d.deptype IN ('a', 'i');`,
        ),
      )
    case 'sequence.value_hidden':
      return one(
        ['last_value is empty for a sequence the monitoring role may not read, or one never used.'],
        block(
          'Sequences whose value the current role cannot see.',
          `SELECT schemaname, sequencename,
       has_sequence_privilege(format('%I.%I', schemaname, sequencename), 'SELECT') AS can_read
  FROM pg_sequences
 WHERE last_value IS NULL;`,
        ),
      )
    case 'trigger.user_disabled':
    case 'trigger.constraint_disabled':
      return one(
        [
          'tgenabled D means disabled; a disabled constraint trigger stops enforcing its foreign key.',
        ],
        block(
          'Disabled triggers.',
          `SELECT tgrelid::regclass AS table_name, tgname, tgenabled, tgisinternal,
       tgconstraint <> 0 AS constraint_trigger
  FROM pg_trigger
 WHERE tgenabled = 'D'
 ORDER BY 1, 2;`,
        ),
      )
    case 'function.secdef_no_search_path':
      return one(
        [
          'A SECURITY DEFINER function without its own search_path runs with the caller’s, who can then put objects of their own in its way.',
        ],
        block(
          'SECURITY DEFINER functions without a search_path setting.',
          `SELECT p.oid::regprocedure AS function_name, pg_get_userbyid(p.proowner) AS owner, p.proconfig
  FROM pg_proc p
  JOIN pg_namespace n ON n.oid = p.pronamespace
 WHERE p.prosecdef
   AND n.nspname NOT IN ('pg_catalog', 'information_schema')
   AND NOT EXISTS (SELECT 1 FROM unnest(coalesce(p.proconfig, '{}')) c WHERE c LIKE 'search_path=%');`,
        ),
      )
    case 'function.untracked':
      return one(
        [
          'With track_functions none, pg_stat_user_functions stays empty and slow functions cannot be found.',
        ],
        settings('track_functions'),
        block(
          'User functions by language.',
          `SELECT l.lanname, count(*) AS functions
  FROM pg_proc p
  JOIN pg_language l ON l.oid = p.prolang
  JOIN pg_namespace n ON n.oid = p.pronamespace
 WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
 GROUP BY 1;`,
        ),
      )
    case 'extension.upgrade_available':
    case 'postgis.upgrade_available':
      return one(
        [
          'installed_version older than default_version: the packages were upgraded but ALTER EXTENSION … UPDATE was not run in this database.',
        ],
        block(
          'Installed extensions behind their packaged version.',
          `SELECT name, installed_version, default_version
  FROM pg_available_extensions
 WHERE installed_version IS NOT NULL
   AND installed_version <> default_version;`,
        ),
      )
    case 'postgis.srid_mixed':
    case 'postgis.srid_unknown':
      return one(
        [
          'srid 0 is an unknown reference system; different srids in one table make spatial comparisons wrong or failing.',
        ],
        block(
          'Geometry columns and their reference systems.',
          `SELECT f_table_schema, f_table_name, f_geometry_column, srid, type
  FROM geometry_columns
 ORDER BY srid, 1, 2;`,
        ),
      )
    case 'postgis.spatial_unindexed':
      return one(
        [
          'A geometry column without a GiST, SP-GiST or BRIN index makes every spatial filter scan the whole table.',
        ],
        block(
          'Geometry columns no spatial index covers.',
          `SELECT g.f_table_schema, g.f_table_name, g.f_geometry_column
  FROM geometry_columns g
 WHERE NOT EXISTS (
         SELECT 1
           FROM pg_index i
           JOIN pg_class ic ON ic.oid = i.indexrelid
           JOIN pg_am am ON am.oid = ic.relam
           JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
          WHERE i.indrelid = format('%I.%I', g.f_table_schema, g.f_table_name)::regclass
            AND a.attname = g.f_geometry_column
            AND am.amname IN ('gist', 'spgist', 'brin'));`,
        ),
      )
    case 'bloat.estimate_blind':
      return one(
        [
          'Bloat is estimated from table statistics; tables never analyzed have none, so their bloat is unknown.',
        ],
        block(
          'Tables never analyzed.',
          `SELECT schemaname, relname, n_live_tup
  FROM pg_stat_user_tables
 WHERE last_analyze IS NULL AND last_autoanalyze IS NULL
 ORDER BY n_live_tup DESC;`,
        ),
      )
  }
  return null
}

// ---- instance -------------------------------------------------------------

const CONFIG_SETTINGS: Record<string, string[]> = {
  'config.checkpoints_too_frequent': [
    'max_wal_size',
    'checkpoint_timeout',
    'checkpoint_completion_target',
  ],
  'config.effective_cache_size_mismatch': ['effective_cache_size', 'shared_buffers'],
  'config.fsync_off': ['fsync'],
  'config.full_page_writes_off': ['full_page_writes'],
  'config.maintenance_work_mem_low': ['maintenance_work_mem', 'autovacuum_work_mem'],
  'config.max_connections_high': ['max_connections', 'work_mem'],
  'config.shared_buffers_high': ['shared_buffers', 'effective_cache_size'],
  'config.shared_buffers_low': ['shared_buffers', 'effective_cache_size'],
  'config.track_io_timing_off': ['track_io_timing'],
  'config.wal_keep_size_low': ['wal_keep_size', 'max_slot_wal_keep_size'],
  'config.work_mem_low': ['work_mem', 'max_connections'],
  'config.work_mem_oversized': ['work_mem', 'max_connections', 'hash_mem_multiplier'],
}

const activityByState = block(
  'Client connections by state, with max_connections.',
  `SELECT state, count(*) AS connections,
       current_setting('max_connections') AS max_connections
  FROM pg_stat_activity
 WHERE backend_type = 'client backend'
 GROUP BY state
 ORDER BY 2 DESC;`,
)

function instanceCheck(ruleId: string, ctx: Context): FindingCheck | null {
  const one = (steps: string[], ...parts: string[]): FindingCheck => ({
    database: null,
    steps,
    sql: script(...parts),
  })
  const configNames = CONFIG_SETTINGS[ruleId]
  if (configNames) {
    const extra: string[] = []
    if (ruleId === 'config.checkpoints_too_frequent') {
      extra.push(
        block(
          'Checkpoints started by time and requested by WAL volume: pg_stat_checkpointer from PostgreSQL 17, pg_stat_bgwriter before.',
          `SELECT (xpath('//num_timed/text()', x))[1]::text::bigint     AS num_timed,
       (xpath('//num_requested/text()', x))[1]::text::bigint AS num_requested
  FROM query_to_xml(
         CASE WHEN current_setting('server_version_num')::int >= 170000
              THEN 'SELECT num_timed, num_requested FROM pg_stat_checkpointer'
              ELSE 'SELECT checkpoints_timed AS num_timed, checkpoints_req AS num_requested FROM pg_stat_bgwriter'
         END, false, true, '') AS q(x);`,
        ),
      )
    }
    if (ruleId === 'config.max_connections_high') extra.push(activityByState)
    return one(
      [
        'The settings in force, with their source (configuration file, ALTER SYSTEM, default) and whether a restart is pending.',
        'The finding evidence holds the value the advisor compared and its threshold.',
      ],
      settings(...configNames),
      ...extra,
    )
  }
  switch (ruleId) {
    case 'config.drift': {
      const name = ctx.subject.kind === 'setting' ? ctx.subject.name : null
      return one(
        [
          'Run the query on every instance of the cluster: the values differ, so a failover changes how the database behaves.',
        ],
        name
          ? settings(name)
          : block(
              'Settings changed from their defaults.',
              "SELECT name, setting, source FROM pg_settings WHERE source <> 'default' ORDER BY name;",
            ),
      )
    }
    case 'conn.saturation':
      return one(
        [
          'Compare the connections in use with max_connections minus the reserved ones; at the limit new connections fail.',
          'Many idle connections point to a pool that is too large or not shared.',
        ],
        activityByState,
        settings('max_connections', 'superuser_reserved_connections', 'reserved_connections'),
      )
    case 'conn.idle_share_high':
      return one(
        [
          'Idle connections hold memory and slots; group them by application and client to find who opens them.',
        ],
        block(
          'Idle connections by application and client.',
          `SELECT application_name, client_addr, usename, count(*) AS idle
  FROM pg_stat_activity
 WHERE state = 'idle' AND backend_type = 'client backend'
 GROUP BY 1, 2, 3
 ORDER BY 4 DESC
 LIMIT 10;`,
        ),
      )
    case 'txn.idle_in_transaction':
      return one(
        [
          'A session idle in a transaction holds its locks and its snapshot: dead rows cannot be removed and other sessions may wait.',
          'The application named in application_name left the transaction open; idle_in_transaction_session_timeout ends such sessions.',
        ],
        block(
          'Sessions idle in a transaction, oldest first.',
          `SELECT pid, usename, application_name, client_addr,
       now() - xact_start   AS transaction_age,
       now() - state_change AS idle_for,
       left(query, 100)     AS last_statement
  FROM pg_stat_activity
 WHERE state LIKE 'idle in transaction%'
 ORDER BY xact_start;`,
        ),
      )
    case 'txn.long_running':
      return one(
        [
          'A long transaction holds its snapshot: vacuum cannot remove the rows deleted after it began.',
        ],
        block(
          'Open transactions, oldest first.',
          `SELECT pid, usename, application_name, state,
       now() - xact_start AS transaction_age,
       wait_event_type, wait_event, left(query, 100) AS query
  FROM pg_stat_activity
 WHERE xact_start IS NOT NULL
 ORDER BY xact_start
 LIMIT 10;`,
        ),
      )
    case 'txn.prepared_orphan':
      return one(
        [
          'A prepared transaction nobody commits or rolls back holds its locks and its snapshot indefinitely.',
          'Only the transaction manager that prepared it should decide: COMMIT PREPARED or ROLLBACK PREPARED with its gid.',
        ],
        block(
          'Prepared transactions, oldest first.',
          'SELECT gid, prepared, owner, database, age(transaction) AS xid_age FROM pg_prepared_xacts ORDER BY prepared;',
        ),
      )
    case 'txn.wraparound_risk':
      return one(
        [
          'xid_age per database is how far it is from its last freeze; vacuum cannot freeze past the oldest snapshot, prepared transaction or replication slot below.',
        ],
        block(
          'Transaction id age per database.',
          `SELECT datname, age(datfrozenxid) AS xid_age, mxid_age(datminmxid) AS mxid_age,
       current_setting('autovacuum_freeze_max_age') AS freeze_max_age
  FROM pg_database
 ORDER BY 2 DESC;`,
        ),
        xminHolders,
        block(
          'Prepared transactions and replication slots holding an old xmin.',
          `SELECT gid, age(transaction) FROM pg_prepared_xacts;
SELECT slot_name, active, age(xmin) AS xmin_age, age(catalog_xmin) AS catalog_xmin_age FROM pg_replication_slots;`,
        ),
      )
    case 'txn.high_rollback_ratio':
      return one(
        [
          'rollback_pct is the share of transactions rolled back; application errors, retries or drivers that roll back by default raise it.',
        ],
        block(
          'Commits and rollbacks per database.',
          `SELECT datname, xact_commit, xact_rollback,
       round(100.0 * xact_rollback / NULLIF(xact_commit + xact_rollback, 0), 1) AS rollback_pct,
       stats_reset
  FROM pg_stat_database
 WHERE datname IS NOT NULL
 ORDER BY 4 DESC NULLS LAST;`,
        ),
      )
    case 'lock.long_wait':
      return one(
        [
          'blocked_by lists the sessions holding the lock; the first of the chain is the one to look at.',
          'Locks › Captured incidents keeps the sessions as they were when the agent saw the wait.',
        ],
        block(
          'Sessions waiting for a lock and who blocks them.',
          `SELECT pid, usename, application_name, wait_event_type, wait_event,
       now() - query_start AS waiting_for,
       pg_blocking_pids(pid) AS blocked_by,
       left(query, 100) AS query
  FROM pg_stat_activity
 WHERE cardinality(pg_blocking_pids(pid)) > 0
 ORDER BY query_start;`,
        ),
      )
    case 'vacuum.disabled':
      return one(
        [
          'autovacuum and track_counts must both be on; tables with autovacuum_enabled=false are left out one by one.',
        ],
        settings('autovacuum', 'track_counts'),
        block(
          'Tables with autovacuum turned off.',
          "SELECT oid::regclass AS table_name, reloptions FROM pg_class WHERE reloptions::text LIKE '%autovacuum_enabled=false%' OR reloptions::text LIKE '%autovacuum_enabled=off%';",
        ),
      )
    case 'vacuum.starvation':
      return one(
        [
          'All autovacuum workers busy for long means tables wait their turn; the tables with most dead rows are the ones waiting.',
        ],
        block(
          'Vacuums running now.',
          'SELECT pid, datname, relid::regclass AS table_name, phase, heap_blks_scanned, heap_blks_total FROM pg_stat_progress_vacuum;',
        ),
        settings('autovacuum_max_workers', 'autovacuum_naptime', 'autovacuum_vacuum_cost_limit'),
        block(
          'Tables with the most dead rows (in the database you are connected to).',
          'SELECT schemaname, relname, n_dead_tup, last_autovacuum FROM pg_stat_user_tables ORDER BY n_dead_tup DESC LIMIT 10;',
        ),
      )
    case 'archive.disabled':
      return one(
        ['archive_mode off: no WAL archive, so no point-in-time recovery.'],
        settings('archive_mode', 'archive_command', 'archive_library', 'wal_level'),
      )
    case 'archive.failing':
    case 'archive.stalled':
      return one(
        [
          'failed_count rising and last_failed_wal show the archiver failing; the server log holds the archive_command error.',
          'WAL files waiting to be archived stay in pg_wal until archiving succeeds.',
        ],
        block(
          'Archiver status.',
          `SELECT archived_count, last_archived_wal, last_archived_time,
       failed_count, last_failed_wal, last_failed_time, stats_reset
  FROM pg_stat_archiver;`,
        ),
        settings('archive_mode', 'archive_timeout'),
      )
    case 'backup.no_basebackup_seen':
    case 'backup.no_strategy':
      return one(
        [
          'The agent sees a base backup only while it streams (pg_basebackup) or while a backup is in progress; a tool that copies files elsewhere is not visible here.',
          'Check your backup tool’s log for the last successful backup.',
        ],
        block(
          'Base backups streaming now.',
          'SELECT pid, phase, backup_total, backup_streamed FROM pg_stat_progress_basebackup;',
        ),
        block(
          'Archiver status: a backup strategy usually archives WAL.',
          'SELECT archived_count, last_archived_time, failed_count FROM pg_stat_archiver;',
        ),
      )
    case 'wal.directory_large':
      return one(
        [
          'pg_wal grows when WAL cannot be removed: a replication slot not consumed, failing archiving, or wal_keep_size and max_wal_size set high.',
        ],
        block(
          'Size of pg_wal (needs pg_monitor).',
          'SELECT count(*) AS files, pg_size_pretty(sum(size)) AS size FROM pg_ls_waldir();',
        ),
        block(
          'WAL retained by each replication slot (on a primary).',
          `SELECT slot_name, slot_type, active,
       pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)) AS retained
  FROM pg_replication_slots
 ORDER BY pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn) DESC NULLS LAST;`,
        ),
        block(
          'Archiver failures.',
          'SELECT failed_count, last_failed_wal, last_failed_time FROM pg_stat_archiver;',
        ),
      )
    case 'standby.recovery_conflicts':
      return one(
        [
          'Run it on the standby: each column counts queries cancelled because replay needed what they were reading.',
          'hot_standby_feedback on, or a higher max_standby_streaming_delay, trade bloat on the primary or replay delay for fewer cancellations.',
        ],
        block(
          'Recovery conflicts per database (on the standby).',
          'SELECT datname, confl_tablespace, confl_lock, confl_snapshot, confl_bufferpin, confl_deadlock FROM pg_stat_database_conflicts;',
        ),
        settings(
          'hot_standby_feedback',
          'max_standby_streaming_delay',
          'max_standby_archive_delay',
        ),
      )
    case 'standby.replay_paused':
      return one(
        [
          'Run it on the standby: paused means WAL is received but not applied, so the standby falls behind.',
          'Someone ran pg_wal_replay_pause() or a recovery target was reached; pg_wal_replay_resume() resumes it (not run here: it changes state).',
        ],
        block(
          'Replay state of the standby (PostgreSQL 14 and later).',
          `SELECT pg_is_in_recovery() AS in_recovery,
       CASE WHEN pg_is_in_recovery() THEN pg_get_wal_replay_pause_state() END AS pause_state,
       pg_last_wal_receive_lsn() AS received, pg_last_wal_replay_lsn() AS replayed,
       now() - pg_last_xact_replay_timestamp() AS replay_delay;`,
        ),
      )
    case 'subscription.disabled':
    case 'subscription.errors':
    case 'subscription.not_receiving':
      return one(
        [
          'Run it on the subscriber, in the subscription’s database: a disabled subscription or no worker pid means nothing is applied.',
          'The error counters rise on every failed apply or table sync; the server log holds the error.',
        ],
        block(
          'Subscriptions, their worker and last message.',
          `SELECT s.subname, s.subenabled, st.pid, st.received_lsn,
       st.last_msg_receipt_time, st.latest_end_time
  FROM pg_subscription s
  LEFT JOIN pg_stat_subscription st ON st.subid = s.oid;`,
        ),
        block(
          'Apply and sync errors (PostgreSQL 15 and later).',
          'SELECT subname, apply_error_count, sync_error_count, stats_reset FROM pg_stat_subscription_stats;',
        ),
      )
    case 'logical_slot.spill_high':
      return one(
        [
          'spill_bytes is decoded data written to disk because a transaction did not fit logical_decoding_work_mem.',
        ],
        block(
          'Spill per logical slot (PostgreSQL 14 and later).',
          'SELECT slot_name, spill_txns, spill_count, pg_size_pretty(spill_bytes) AS spilled, stats_reset FROM pg_stat_replication_slots;',
        ),
        settings('logical_decoding_work_mem'),
      )
    case 'security.monitoring_role_can_write':
    case 'security.monitoring_role_read_only_off':
      return one(
        [
          'Connect as the monitoring role the agent uses: it should be able to read statistics and nothing else.',
          'rolsuper or write privileges on tables mean a leaked agent password can change data.',
        ],
        block(
          'Attributes of the current role.',
          `SELECT rolname, rolsuper, rolcreaterole, rolcreatedb, rolreplication, rolbypassrls, rolconfig
  FROM pg_roles
 WHERE rolname = current_user;`,
        ),
        block(
          'Write privileges the current role holds on tables.',
          `SELECT table_schema, table_name, privilege_type
  FROM information_schema.role_table_grants
 WHERE grantee = current_user AND privilege_type <> 'SELECT'
 LIMIT 20;`,
        ),
        settings('default_transaction_read_only'),
      )
  }
  return null
}

/** The steps and read-only SQL that confirm `finding` on the database, or null for a rule with none. */
export function findingCheck(finding: CheckSource): FindingCheck | null {
  const subject = findingSubject(finding)
  const ctx: Context = {
    db: finding.datname === '' ? null : (finding.datname ?? null),
    subject,
    queryid: subject.kind === 'statement' ? subject.queryid : null,
    rel: subject.kind === 'object' ? relationLiteral(subject.name) : null,
  }
  const rule = finding.rule_id
  if (rule.startsWith('query.')) return statementCheck(rule, ctx)
  return relationCheck(rule, ctx) ?? catalogCheck(rule, ctx) ?? instanceCheck(rule, ctx)
}
