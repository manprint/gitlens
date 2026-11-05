/**
 * Plain-language definitions of the PostgreSQL and pglens terms the console
 * uses (D30). Every acronym in a caption or hint must be defined here or be one
 * of `COMMON_ACRONYMS`; `Term` renders a term with its definition.
 */

export interface GlossaryEntry {
  /** One sentence, shown with the term. */
  short: string
  /** Optional longer explanation. */
  long?: string
}

export const glossary = {
  TPS: { short: 'Transactions per second: committed transactions divided by elapsed seconds.' },
  WAL: {
    short: 'Write-ahead log: the journal PostgreSQL writes every change to before the data files.',
    long: 'Replication, point-in-time recovery and crash recovery all read the WAL.',
  },
  LSN: { short: 'Log sequence number: a byte position in the write-ahead log.' },
  XID: { short: 'Transaction ID: the 32-bit number PostgreSQL gives each writing transaction.' },
  MVCC: {
    short:
      'Multi-version concurrency control: updates write new row versions, so readers never wait for writers.',
    long: 'Old row versions stay as dead tuples until vacuum removes them.',
  },
  TOAST: {
    short: 'The storage PostgreSQL uses for large values, kept out of line in a side table.',
  },
  HOT: {
    short:
      'Heap-only tuple update: an update that changes no indexed column and so writes no index entry.',
  },
  ASH: {
    short:
      'Active session history: samples of what every connected session was doing, taken every few seconds.',
  },
  FK: { short: 'Foreign key: a column that must match a key of another table.' },
  IPC: {
    short:
      'Inter-process communication: a wait for another server process, such as a parallel worker.',
  },
  PK: { short: 'Primary key: the column or columns that identify each row of a table.' },
  SLRU: {
    short:
      'Simple least-recently-used caches PostgreSQL keeps for transaction status and similar small data.',
  },
  GIN: {
    short: 'Generalized inverted index: an index type for arrays, JSON and full-text search.',
  },
  GiST: { short: 'Generalized search tree: an index type for geometric, range and similar data.' },
  BRIN: {
    short: 'Block range index: a small index that stores summaries of ranges of table pages.',
  },
  OID: { short: 'Object identifier: the internal number PostgreSQL gives catalog objects.' },
  DDL: { short: 'Data definition language: statements that create, change or drop objects.' },
  autovacuum: {
    short: 'The background process that vacuums and analyzes tables when enough rows changed.',
  },
  vacuum: {
    short: 'The operation that removes dead row versions so their space can be reused.',
  },
  bloat: {
    short: 'Space in a table or index taken by dead or empty rows rather than live data.',
  },
  'dead tuples': {
    short: 'Row versions no transaction can see any more, waiting for vacuum to remove them.',
  },
  checkpoint: {
    short:
      'The point where PostgreSQL has written every changed page to the data files, so older WAL is no longer needed for recovery.',
  },
  'replication slot': {
    short:
      'A named marker on the primary that keeps the WAL a consumer still needs, even when it is disconnected.',
  },
  'replay lag': {
    short: 'How far a standby is behind the primary in applying the WAL it has received.',
  },
  standby: { short: 'A server that replays the WAL of a primary and serves read-only queries.' },
  primary: { short: 'The server that accepts writes in a replicated cluster.' },
  wraparound: {
    short:
      'The point where transaction IDs would run out; vacuum must freeze old rows well before it.',
  },
  'sequential scan': {
    short: 'Reading a whole table from start to end instead of using an index.',
  },
  'index scan': { short: 'Finding rows through an index instead of reading the whole table.' },
  pg_stat_statements: {
    short:
      'The PostgreSQL extension that records execution statistics for every normalised statement.',
  },
  'cache hit ratio': {
    short:
      'The share of page reads served from shared buffers instead of the operating system or disk.',
  },
  xmin: {
    short:
      'The oldest transaction a session or slot still needs; vacuum cannot remove row versions newer than it.',
  },
  queryid: {
    short:
      'The identifier pg_stat_statements gives a normalised statement; comparable only within one cluster.',
  },
  'wait event': {
    short:
      'What a session is waiting for at the moment it is sampled, such as a lock, I/O or a client.',
  },
  backend: {
    short:
      'The server process PostgreSQL starts for each client connection; it runs that connection’s queries.',
  },
  'shared buffers': {
    short: 'PostgreSQL’s own memory cache of table and index pages.',
  },
  tuple: { short: 'One version of a row stored in a table.' },
  relation: { short: 'A table, an index or another stored object such as a sequence.' },
  multixact: {
    short: 'A group of transactions that hold a lock on the same row; it has its own small cache.',
  },
  tier: {
    short:
      'A level of permission pglens has on a monitored server (T0 to T2); higher tiers allow more checks.',
  },
  heap: {
    short: "A table's main storage, without its indexes and the TOAST data kept out of line.",
  },
  'permission tier': {
    short:
      'The level of database privileges pglens has on a server (T0 to T2); higher tiers enable more checks.',
  },
  T0: { short: 'Permission tier 0: the monitoring role can read statistics only.' },
  T1: { short: 'Permission tier 1: everything in T0, plus running EXPLAIN to show query plans.' },
  T2: {
    short:
      'Permission tier 2: everything in T1, plus cancelling or ending other sessions where the target allows it.',
  },
  EXPLAIN: {
    short: 'The PostgreSQL command that shows the steps chosen to run a statement.',
  },
  ANALYZE: {
    short:
      'The EXPLAIN option that also runs the statement and reports the real time of each step.',
  },
} as const satisfies Record<string, GlossaryEntry>

export type Term = keyof typeof glossary

/** Acronyms readers know without a definition. */
export const COMMON_ACRONYMS = ['UTC', 'CPU', 'SQL', 'PID', 'I/O', 'ID'] as const

export function isTerm(value: string): value is Term {
  return Object.hasOwn(glossary, value)
}
