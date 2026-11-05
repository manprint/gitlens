import type { CaptionEntry } from '../captions'

const ASH_SOURCE =
  'ASH samples: the agent reads pg_stat_activity once per second (client and parallel worker backends whose state is not idle, leaving out its own pglens/ sessions) and stores the counts per 10 second window; a backend with no wait event is counted as CPU.'
const ASH_UNIT = 'average backends (samples divided by the sampling ticks of the bucket)'
const ASH_WINDOW = 'Selected time range, in buckets of 10 seconds or more (at most 180 per chart).'
const ASH_GAPS = 'A gap means no sample was stored for that bucket.'

/** Captions of the activity route group (D30); rules in the plan, phase 17. */
export const activityCaptions = {
  'ash.breakdown-types': {
    what: 'Average number of non-idle backends over time, split by the kind of wait they were in, with CPU for backends that reported no wait.',
    source: ASH_SOURCE,
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `A value of 2 means two backends were not idle on average in that bucket. Select a series to see its wait events. ${ASH_GAPS}`,
  },
  'ash.breakdown-events': {
    what: 'Average number of non-idle backends over time, split by the exact wait event they were in; CPU stands for no wait event.',
    source: ASH_SOURCE,
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `Select an event to see the queries sampled while waiting in it. The smallest events are folded into other, so the total is kept. ${ASH_GAPS}`,
  },
  'ash.breakdown-queries': {
    what: 'Average number of non-idle backends over time, split by the query they were running, identified by its query id.',
    source:
      'ASH samples: the agent reads pg_stat_activity once per second and stores the query_id column per 10 second window; it is filled only when compute_query_id is on, or auto with a module such as pg_stat_statements loaded.',
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `Each series is a query, not a wait group; the smallest queries are folded into other. ${ASH_GAPS}`,
  },
  'ash.stack-types': {
    what: 'One stacked chart: each series is a wait event type, and the height of the stack is the average number of non-idle backends.',
    source: ASH_SOURCE,
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `Select a series to see its wait events. ${ASH_GAPS}`,
  },
  'ash.stack-events': {
    what: 'One stacked chart: each series is a wait event, and the height of the stack is the average number of non-idle backends.',
    source: ASH_SOURCE,
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `Select a series to see the queries sampled in that event. ${ASH_GAPS}`,
  },
  'ash.stack-queries': {
    what: 'One stacked chart: each series is a query id, and the height of the stack is the average number of non-idle backends.',
    source:
      'ASH samples: the query_id column of pg_stat_activity, read once per second and stored per 10 second window.',
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `The top queries below the chart list the text of each id. ${ASH_GAPS}`,
  },
  'ash.chart-cpu': {
    what: 'Average number of non-idle backends that reported no wait event, counted as running on CPU.',
    source: ASH_SOURCE,
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `This chart has its own scale, so a small group is not flattened by a large one. ${ASH_GAPS}`,
  },
  'ash.chart-io': {
    what: 'Average number of non-idle backends waiting for a read or write to storage.',
    source:
      'ASH samples: pg_stat_activity.wait_event_type of the I/O type, read once per second and stored per 10 second window.',
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `This chart has its own scale. Select a legend row to show that series alone. ${ASH_GAPS}`,
  },
  'ash.chart-locks': {
    what: 'Average number of non-idle backends waiting for a lock or for internal contention such as a buffer pin.',
    source:
      'ASH samples: pg_stat_activity.wait_event_type Lock, LWLock, BufferPin or Buffer, read once per second and stored per 10 second window.',
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `Lock is a heavyweight lock held by another transaction; LWLock is an internal lightweight lock. This chart has its own scale. ${ASH_GAPS}`,
  },
  'ash.chart-client': {
    what: 'Average number of non-idle backends waiting for the client application or for another server process.',
    source:
      'ASH samples: pg_stat_activity.wait_event_type Client or IPC, read once per second and stored per 10 second window.',
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `This chart has its own scale. Select a legend row to show that series alone. ${ASH_GAPS}`,
  },
  'ash.chart-other': {
    what: 'Average number of non-idle backends in timer, background-activity, extension and other waits not shown in the other charts.',
    source:
      'ASH samples: pg_stat_activity.wait_event_type Timeout, Activity or Extension, any other type, and the series folded into other.',
    unit: ASH_UNIT,
    window: ASH_WINDOW,
    howToRead: `This chart has its own scale. ${ASH_GAPS}`,
  },
  'ash.top-queries': {
    what: 'The queries sampled most often in the chosen wait scope, with their normalised text and average number of non-idle backends.',
    source:
      'ASH samples summed per query id over the range (top 20), with the newest normalised text of that id from query_texts, collected from pg_stat_statements; ASH never reads live query text.',
    unit: 'samples; average backends (samples divided by the sampling ticks of the whole range)',
    window: 'Selected time range.',
    howToRead:
      'Literal values are not available: the text is normalised. The scope line names the wait event type or event the ranking is limited to.',
  },
  'ash.chart-layout': {
    what: 'Chooses one chart per wait group, each on its own scale, or a single stacked chart of all groups.',
    source:
      'A view choice kept in the page address (view=stacked); the data and the buckets do not change.',
    unit: 'layout choice: separate or stacked',
    window: 'This page view.',
  },
  'activity.section': {
    what: 'Connections, transaction and state ages, prepared transactions, wraparound distance and deadlocks of the latest activity sample, with breakdowns.',
    source:
      'The activity check of the agent (pg_stat_activity, pg_prepared_xacts, pg_database) and the deadlock counter of pg_stat_database, read through the instance activity endpoint.',
    window:
      'Newest reading of the last 30 seconds, sampled every 10 seconds; the page refreshes every 5 seconds.',
    howToRead: 'A card shows Unknown when the agent sent no value for it; zero is a real reading.',
  },
  'activity.longest-transaction': {
    what: 'Age of the oldest open transaction among client sessions.',
    source:
      'pg_stat_activity: the largest now() - xact_start over client backends, leaving out the agent session (pg_max_xact_age_seconds).',
    unit: 'seconds, shown as a duration',
    window: 'Latest sample, taken every 10 seconds.',
    howToRead:
      'Zero means no client session had an open transaction. Compare with the idle-in-transaction card to see whether the oldest one is still running a statement.',
  },
  'activity.longest-idle-transaction': {
    what: 'Longest time a client session has sat inside a transaction without running a statement.',
    source:
      'pg_stat_activity: the largest now() - state_change of sessions in the state idle in transaction or idle in transaction (aborted) (pg_max_idle_in_transaction_seconds).',
    unit: 'seconds, shown as a duration',
    window: 'Latest sample, taken every 10 seconds.',
    howToRead: 'Zero means no session is idle in a transaction.',
  },
  'activity.oldest-state-age': {
    what: 'Longest time any client session has stayed in its current state, taken over every state.',
    source:
      'pg_stat_activity: the largest now() - state_change of each state (pg_max_state_age_seconds); the card shows the largest of them.',
    unit: 'seconds, shown as a duration',
    window: 'Latest sample, taken every 10 seconds.',
    howToRead: 'Idle connections kept open by a pool raise it too, so it is not a fault by itself.',
  },
  'activity.prepared-transactions': {
    what: 'Transactions prepared for two-phase commit that are not yet committed or rolled back.',
    source: 'pg_prepared_xacts: count of rows (pg_prepared_xacts).',
    unit: 'transactions',
    window: 'Latest sample, taken every 10 seconds.',
    howToRead: 'Zero is normal when no application uses two-phase commit.',
  },
  'activity.oldest-prepared-transaction': {
    what: 'Time since the oldest prepared transaction was prepared.',
    source:
      'pg_prepared_xacts: now() - min(prepared), zero when there is none (pg_oldest_prepared_xact_seconds).',
    unit: 'seconds, shown as a duration',
    window: 'Latest sample, taken every 10 seconds.',
  },
  'activity.frozen-xid-age': {
    what: 'Transaction IDs used since the oldest database was last frozen: the distance travelled towards transaction ID wraparound.',
    source:
      'pg_database: the largest age(datfrozenxid) over all databases (pg_max_datfrozenxid_age).',
    unit: 'transaction IDs',
    window: 'Latest sample, taken every 10 seconds.',
    thresholds:
      'The advisor rule txn.wraparound_risk is critical above 1,000,000,000 transaction IDs.',
  },
  'activity.deadlock-rate': {
    what: 'Deadlocks detected per second, added up over all databases.',
    source:
      'pg_stat_database.deadlocks (pg_deadlocks_total), stored as a rate per second; the newest reading of each database is added up.',
    unit: 'per second',
    window: 'Newest reading of the last 90 seconds; collected every 30 seconds.',
    howToRead:
      'A small rate keeps two significant digits so a few deadlocks an hour do not show as zero. The statements involved are only in the PostgreSQL log.',
  },
  'activity.saturation': {
    what: 'Whether open client connections are above the saturation threshold of max_connections.',
    source:
      'pg_connections_used_ratio (connections used divided by max_connections), compared in the browser with 0.8.',
    unit: 'ratio of max_connections',
    window: 'Latest sample.',
    thresholds:
      'Saturated above 80 % of max_connections, the limit of the advisor rule conn.saturation.',
  },
  'activity.by-database': {
    what: 'Client connections open now, counted per database.',
    source:
      'pg_stat_activity: client backends grouped by datname (pg_connections_by_database); the ten largest groups are listed, the rest are added up as other.',
    unit: 'connections',
    window: 'Latest sample.',
  },
  'activity.by-state': {
    what: 'Client backends counted per session state, such as active, idle and idle in transaction.',
    source:
      'pg_stat_activity: client backends grouped by state and wait event type (pg_backends); the card adds the wait types of each state.',
    unit: 'backends',
    window: 'Latest sample.',
  },
  'activity.xmin-holder': {
    what: 'The oldest holder of the xmin horizon: the session, slot, standby or prepared transaction that keeps vacuum from removing dead rows.',
    source:
      'The xmin_horizon fact of the activity check: the oldest of pg_stat_activity (backend_xmin, backend_xid), pg_replication_slots, pg_stat_replication and pg_prepared_xacts; no query text is read.',
    unit: 'transaction IDs; time since start in seconds',
    window: 'Latest sample, taken every 10 seconds; a fact older than 30 seconds is not shown.',
    howToRead:
      'Time since start is not measured for a slot or a standby. When no holder is shown, none held an xmin in the latest sample.',
  },
  'activity.by-application': {
    what: 'Client connections open now, counted per application name.',
    source:
      'pg_stat_activity: client backends grouped by application_name (pg_connections_by_application); the ten largest are listed, the rest are added up as other.',
    unit: 'connections',
    window: 'Latest sample; reported only while checks.activity.by_application is on.',
    howToRead: 'The key is on unless set to false in the agent configuration.',
  },
  'activity.sessions-active': {
    what: 'Client sessions of the latest lock sample, with their state, database, user and the first line of the query when it is known.',
    source:
      'getLocks sessions: client backends of pg_stat_activity (at most 100); the query text is kept only for sessions in a blocking tree.',
    unit: 'sessions',
    window: 'Latest lock sample, taken every 10 seconds.',
    howToRead:
      'This list appears when the server does not tell application sessions from pglens ones.',
  },
  'locks.blocking-tree': {
    what: 'Sessions waiting for a lock, each under the session that blocks it, starting from root blockers that wait for no one.',
    source:
      'getLocks nodes: pg_stat_activity with pg_blocking_pids() for sessions waiting on a heavyweight lock, joined to the waiting lock in pg_locks (mode, type, relation, wait start); the tree is built in the browser.',
    unit: 'sessions; wait duration in seconds',
    window: 'Latest lock sample, taken every 10 seconds; the page refreshes every 5 seconds.',
    howToRead:
      'A wait shorter than the sampling interval can be missed. A root blocker is not waiting itself. A cycle means sessions block each other.',
  },
  'readiness.severity': {
    what: 'A warning means a pglens feature is lost or degraded on this server; a note means something is off by design or optional.',
    source: 'getReadiness: the severity of the pglens readiness rule that raised the item.',
    unit: 'items',
    window: 'Latest values; recomputed at most every 30 s.',
  },
  'readiness.requires-restart': {
    what: 'The fix changes a setting that is read only at start, so the agent or the server must restart afterwards.',
    source: 'getReadiness: the requires_restart flag the rule sets on each fix.',
    window: 'Latest values; recomputed at most every 30 s.',
    howToRead: 'Without the restart the change has no effect.',
  },
} as const satisfies Record<string, CaptionEntry>
