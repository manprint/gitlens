import type { CaptionEntry } from '../captions'

/** Captions of the indexes route group (D30); rules in the plan, phase 17. */
export const indexesCaptions = {
  'indexes.page': {
    what: "Every index the agent reports for a server, how big and how used, with the advisor's findings on unused, duplicate, redundant, invalid and missing indexes.",
    source:
      'getInstanceIndexes rows and the index.* advisor findings of getFindings, for the server and database picked on the page.',
    unit: 'indexes, findings',
    window: 'Latest collection and latest advisor evaluation.',
    howToRead:
      'The tabs split the indexes into the inventory, the removable ones, tables with dead rows, suggested new indexes and the cost of the indexes of each table.',
  },
  'indexes.inventory': {
    what: 'Every index the agent reported for the server, one row per index, with its columns, access method, size, scan rate and last scan time.',
    source:
      'getInstanceIndexes: the newest metrics_indexes sample of each index (pg_stat_user_indexes, pg_stat_statio_user_indexes, pg_relation_size) joined with its index_def fact (pg_get_indexdef).',
    unit: 'bytes, scans per second, percent',
    window:
      'Latest collection (a sample from the last 15 minutes); scan figures are rates over the last collection interval.',
    howToRead:
      'Need attention counts indexes with an open advisor finding or marked invalid. The agent reports only its busiest and largest indexes per database, so a missing index may still exist.',
  },
  'indexes.summary': {
    what: 'How the indexes in scope break down by kind, access method, database and table, with the counts of duplicate, redundant, invalid and unindexed findings.',
    source:
      'Counted in the browser from the getInstanceIndexes rows (summariseIndexes) and the open index.* findings of getFindings.',
    unit: 'indexes',
    window: 'Latest collection and latest advisor evaluation, for the database picked on the page.',
    howToRead:
      'Counts cover only the indexes the agent reported. The kind counts overlap: a partial unique index is both unique and partial.',
  },
  'indexes.findings': {
    what: 'Open advisor findings on indexes: unused, duplicate, redundant prefix, invalid, diverging and foreign keys without an index, one row per finding.',
    source:
      'getFindings rows of the advisor rules index.unused, index.duplicate, index.redundant_prefix, index.invalid, index.divergence and index.fk_unindexed (rules_relation.go, rules_catalog.go).',
    unit: 'findings',
    window: 'Latest advisor evaluation; only open findings are listed.',
    howToRead:
      'Expand a row for the detail and the remediation the advisor wrote. A finding is a candidate to review, not an instruction.',
  },
  'indexes.foreign-keys': {
    what: 'Foreign keys whose referencing columns no index leads with, so deleting or re-keying a referenced row scans the referencing table.',
    source:
      'Advisor rule index.fk_unindexed over the fk_unindexed facts of the fk_indexes check, which reads the catalogs only (internal/check/fk_indexes.go).',
    unit: 'foreign keys',
    window: 'Latest advisor evaluation of the open findings.',
    howToRead:
      'These are indexes that are missing, so they do not appear in the inventory. If rows of the referenced table are never deleted or re-keyed, the index is not needed. Up to 200 foreign keys per database are reported, the largest referencing tables first.',
    thresholds: 'Severity is warning when the referencing table is 10 MiB or larger, info below.',
  },
  'indexes.summary-primary': {
    what: 'Indexes that back a primary key constraint.',
    source: 'getInstanceIndexes is_primary, from pg_index.indisprimary.',
    unit: 'indexes',
    window: 'Latest collection.',
  },
  'indexes.summary-unique': {
    what: 'Unique indexes that are not a primary key.',
    source:
      'getInstanceIndexes is_unique without is_primary, from pg_index.indisunique and indisprimary.',
    unit: 'indexes',
    window: 'Latest collection.',
  },
  'indexes.summary-partial': {
    what: 'Indexes with a predicate, which cover only the rows the predicate selects.',
    source: 'The predicate parsed in the browser from the pg_get_indexdef text of the index.',
    unit: 'indexes',
    window: 'Latest collection.',
  },
  'indexes.summary-expression': {
    what: 'Indexes with a key built from an expression, such as lower(email), instead of a plain column.',
    source: 'The key columns parsed in the browser from the pg_get_indexdef text of the index.',
    unit: 'indexes',
    window: 'Latest collection.',
  },
  'indexes.summary-composite': {
    what: 'Indexes with more than one key column; included columns are not counted.',
    source: 'The key columns parsed in the browser from the pg_get_indexdef text of the index.',
    unit: 'indexes',
    window: 'Latest collection.',
  },
  'indexes.summary-invalid': {
    what: 'Indexes PostgreSQL marks invalid: queries cannot use them, yet writes still maintain them.',
    source: 'getInstanceIndexes is_valid, from pg_index.indisvalid.',
    unit: 'indexes',
    window: 'Latest collection.',
    howToRead: 'Rebuild or drop an invalid index, as the advisor rule index.invalid advises.',
  },
  'indexes.breakdown-method': {
    what: 'Number and combined size of the indexes in scope for each access method, such as btree or gin.',
    source:
      'Counted in the browser (summariseIndexes) from the access method in the pg_get_indexdef text and the pg_relation_size of each index.',
    unit: 'indexes, bytes',
    window: 'Latest collection.',
  },
  'indexes.breakdown-database': {
    what: 'Number and combined size of the indexes in scope for each database.',
    source:
      'Counted in the browser (summariseIndexes) from getInstanceIndexes: datname and the pg_relation_size of each index.',
    unit: 'indexes, bytes',
    window: 'Latest collection.',
  },
  'indexes.breakdown-tables': {
    what: 'The eight tables that carry the most indexes in scope, with the number and combined size of their indexes.',
    source:
      'Counted in the browser (groupByTable) from getInstanceIndexes: the table and the pg_relation_size of each index.',
    unit: 'indexes, bytes',
    window: 'Latest collection.',
    howToRead:
      'Every index adds work to each insert and non-HOT update of its table: a table high on this list is worth a look at the Index tax tab.',
  },
  'indexes.flags': {
    what: 'Labels on an index: its kind, such as primary, unique or partial, and the advisor findings on it, such as unused, duplicate or invalid.',
    source:
      'rowFlags (IndexesPanel): the kind from the index definition and pg_index flags, the findings of the advisor rules index.* for this index.',
    window: 'Latest collection and latest advisor evaluation.',
    howToRead:
      'Diverges means the index is absent or defined differently on a sibling server of the cluster. Never scanned is the raw counter since the last statistics reset, not the advisor verdict.',
  },
  'indexes.finding-rule': {
    what: 'The advisor rule that raised the finding; critical findings are red, others amber.',
    source: 'rule_id and severity of each getFindings row; the rules are in internal/advisor.',
    window: 'Latest advisor evaluation.',
  },
  'indexes.fk-severity': {
    what: 'How costly the missing index is: warning when the referencing table is 10 MiB or larger, info below that.',
    source:
      'Severity set by the advisor rule index.fk_unindexed from the table size the fk_indexes check reports (fkUnindexedWarnBytes).',
    window:
      'Latest advisor evaluation; the table size is an estimate from the last vacuum or analyze.',
  },
  'indexes.cleanup': {
    what: 'Indexes that look unused, repeat another index or are invalid, with the disk space and index writes that dropping them would save.',
    source:
      'buildCleanup (indexCleanup.ts) over the getInstanceIndexes rows and the index.* advisor findings; index writes from the insert, update and HOT update rates of getInstanceTables.',
    unit: 'indexes, bytes, index entries per second',
    window: 'Latest collection and latest advisor evaluation.',
    howToRead:
      'Confirmed candidates rest on an advisor finding. Raw-counter candidates only had no scan since the last statistics reset: not a verdict, and left out of the confirmed totals. Primary keys and unique indexes are never listed. pglens drops nothing: copy the script, review it and run it yourself.',
  },
  'indexes.cleanup-reason': {
    what: 'Why an index is listed: invalid, duplicate, redundant prefix or unused (advisor findings), or never scanned (raw counters only).',
    source:
      'classify (indexCleanup.ts): the advisor findings on the index, else the last scan status of its statistics.',
    window: 'Latest collection and latest advisor evaluation.',
    howToRead:
      'A duplicate row is a copy to drop; the copy to keep is the constraint, then the most scanned, then the largest.',
  },
  'indexes.dead-rows': {
    what: 'Tables ranked by dead rows, the old row versions vacuum has not cleared yet, with their share of live rows, vacuum due ratio and last vacuum.',
    source:
      'getInstanceTables: the newest metrics_tables sample of each table (pg_stat_user_tables n_dead_tup, n_live_tup); trend from the table history; autovacuum settings from the instance settings.',
    unit: 'rows, percent, bytes (estimated)',
    window:
      'Latest collection; the trend follows the 10 worst tables over the selected time range.',
    howToRead:
      'Dead space is estimated as the heap size times the dead share of its tuples. Vacuum due is the dead-row count at which autovacuum is expected to start; it is approximate when the server settings are not reported.',
  },
  'indexes.inventory-table': {
    what: 'One row per index reported: its table, key columns, kind and flags, size, scan rate, cache hit ratio and last scan; expand a row for its charts.',
    source:
      'getInstanceIndexes rows (pg_stat_user_indexes, pg_statio_user_indexes, pg_relation_size, pg_get_indexdef); the flags come from the index.* advisor findings.',
    unit: 'bytes, scans per second, percent',
    window: 'Latest collection; scan figures are rates over the last collection interval.',
  },
  'indexes.findings-table': {
    what: 'One row per open advisor finding on an index, with its rule, database, object and title; expand a row for the detail and the remediation.',
    source: 'getFindings rows of the advisor rules index.* (internal/advisor), open findings only.',
    unit: 'findings',
    window: 'Latest advisor evaluation.',
  },
  'indexes.foreign-keys-table': {
    what: 'One row per foreign key whose columns no index leads with: its table, its columns, the table it references and the advisor remediation.',
    source: 'getFindings rows of the advisor rule index.fk_unindexed, from the fk_indexes check.',
    unit: 'findings',
    window: 'Latest advisor evaluation of the open findings.',
  },
  'indexes.cleanup-table': {
    what: 'One row per index that looks removable: why it is listed, its size and the index writes it receives; expand a row for the drop and rebuild statements.',
    source:
      'buildCleanup (indexCleanup.ts) over the getInstanceIndexes rows and the index.* advisor findings; writes from the insert and update rates of getInstanceTables.',
    unit: 'bytes, index entries per second',
    window: 'Latest collection and latest advisor evaluation.',
  },
  'indexes.dead-rows-table': {
    what: 'One row per table, most dead rows first: dead rows, dead share, vacuum due ratio, last vacuum, HOT share, writes and trend.',
    source:
      'getInstanceTables (pg_stat_user_tables) with the autovacuum settings of the instance and the table history for the trend.',
    unit: 'rows, percent, bytes (estimated), per second',
    window: 'Latest collection; the trend follows the selected time range.',
  },
  'indexes.suggestions-table': {
    what: 'One row per suggested index: its table and key columns, the share of statement time involved, an estimated size, the table writes and a verdict.',
    source:
      'getIndexSuggestions (internal/indexadvice): the statements of pg_stat_statements that filter or sort on a column no index leads with.',
    unit: 'percent, milliseconds per second, bytes (estimated), per second',
    window: 'The window picked on the tab.',
  },
  'indexes.tax-table': {
    what: 'One row per table: how many indexes it has, their size against the table, the space that looks unused and the index entries its writes add.',
    source:
      'buildIndexTax (indexTax.ts) over the getInstanceIndexes rows and the insert, update and delete rates of getInstanceTables.',
    unit: 'indexes, bytes, percent, index entries per second',
    window: 'Latest collection; every figure is an estimate.',
  },
  'indexes.verdict': {
    what: 'How a suggested index weighs the reads it serves against the writes it costs: strong, moderate or write-heavy. Every verdict is an estimate.',
    source:
      'verdictFor (internal/indexadvice/verdict.go): the call rate of the statements that use the key divided by the index writes per second of the table.',
    window: 'The window picked on the tab.',
    howToRead:
      'A table with no index writes is never write-heavy. A first key column with 20 distinct values or fewer lowers the verdict one step.',
    thresholds: 'Ratio of 10 or more is strong, 1 or more moderate, below 1 write-heavy.',
  },
} as const satisfies Record<string, CaptionEntry>
