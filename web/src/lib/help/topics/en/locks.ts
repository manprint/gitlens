import type { HelpTopic } from '../../types'

export const locksTopic: HelpTopic = {
  id: 'locks',
  group: 'instance',
  title: 'Locks and activity',
  summary:
    'The sessions of one PostgreSQL server, the locks they wait on and the chains of blocking between them, sampled every 10 seconds: the blocking tree, the application and pglens sessions, the activity of the instance, the oldest cleanup blocker, the incidents the agent captured, and the safe actions {{ui:locks.signal.cancelQuery}} and {{ui:locks.signal.terminateBackend}}.',
  page: {
    path: '/instances/:instanceId/locks',
    label: 'Open {{ui:shell.nav.locks}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:shell.nav.locks}} shows the sessions of one PostgreSQL server, the locks they wait on and the chains of blocking between them. The agent samples locks and activity every 10 seconds, and the page shows the latest sample. Open the page from the {{ui:shell.nav.locks}} tab under the instance header. The open view is kept in the address as `?view=`.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Blocking',
              meaning:
                'The blocking tree of the latest lock sample, described in [{{ui:locks.blocking.title}}](/help/locks#blocking-tree).',
            },
            {
              term: '{{ui:locks.sessions.applicationTitle}}',
              meaning:
                'The client sessions of applications, one row per session, described in [Sessions](/help/locks#sessions).',
            },
            {
              term: 'pglens sessions',
              meaning:
                'The sessions that pglens itself opens on the server, shown in the same table layout, described in [Sessions](/help/locks#sessions).',
            },
            {
              term: 'Activity',
              meaning:
                'Connection counts, prepared transactions, the deadlock rate and the oldest cleanup blocker, described in [Activity](/help/locks#activity-tiles).',
            },
            {
              term: '{{ui:locks.incidents.title}}',
              meaning:
                'The deadlocks, lock waits, long or idle transactions and long queries the agent captured in the last 7 days, each with its sessions and statements, described in [{{ui:locks.incidents.title}}](/help/locks#captured-incidents).',
            },
          ],
        },
      ],
      subsections: [
        {
          id: 'freshness',
          title: 'Freshness of the data',
          blocks: [
            {
              kind: 'text',
              text: 'The page compares the age of the latest sample with 20 seconds. Older lock data shows the warning {{ui:locks.blocking.staleData}} Older activity data shows the warning {{ui:locks.activity.staleData}} While the lock sample is stale, the signal actions stay disabled and read {{ui:locks.signal.staleDisabled}}',
            },
          ],
        },
      ],
    },
    {
      id: 'blocking-tree',
      title: '{{ui:locks.blocking.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:locks.blocking.title}} shows the sessions of the latest lock sample as a forest. A session that waits on a lock sits under the session that blocks it. The roots, the sessions that block others, are ordered by the number of blocked sessions, then by wait duration, so the largest chain comes first. The text above the tree gives the time of the latest sample, its age and the sampling interval of 10 seconds.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'User',
              meaning: 'The role of the session.',
            },
            {
              term: '{{ui:locks.sessions.application}}',
              meaning: 'The application name of the session.',
            },
            {
              term: '{{ui:locks.sessions.database}}',
              meaning: 'The database the session is connected to.',
            },
            {
              term: '{{ui:locks.detail.blockedBy}}',
              meaning: 'The PIDs of the sessions that block this one.',
            },
            {
              term: 'State',
              meaning:
                'The state of the session, as PostgreSQL reports it, for example `active` or `idle in transaction`.',
            },
            {
              term: '{{ui:locks.detail.waitEvent}}',
              meaning:
                'The wait event the session is in, as PostgreSQL reports it. For the waits over a longer window, see [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:locks.blocking.lockMode}}',
              meaning:
                'The mode of the lock the waiting session asks for, for example `ShareLock` or `AccessShareLock`.',
            },
            {
              term: '{{ui:locks.blocking.waitsOn}}',
              meaning:
                'The lock the session asks for, its type and the relation it is on. A relation in another database than the agent’s is not named.',
            },
            {
              term: '{{ui:locks.blocking.waitDuration}}',
              meaning: 'How long the session has waited, measured on the server at the sample.',
            },
            {
              term: '{{ui:locks.blocking.waitingSince}}',
              meaning: 'The time the wait began.',
            },
            {
              term: '{{ui:locks.sessions.query}}',
              meaning: 'The query text of the session.',
            },
            {
              term: '{{ui:locks.blocking.rootBlocker}}',
              meaning:
                'A session that blocks others and is not blocked itself. The row reads blocks the sessions below; not itself blocked.',
            },
            {
              term: '{{ui:locks.blocking.cycle}}',
              meaning:
                'The blocking relationship forms a cycle. The tree stops expanding at this session, and the row reads The blocking relationship forms a cycle; expansion stops at this marker.',
            },
            {
              term: 'Also waits on this blocker',
              meaning:
                'A session that waits on a blocker already shown elsewhere in the tree. Its details appear under the first blocker that reaches it.',
            },
            {
              term: '{{ui:locks.blocking.emptyTitle}}',
              meaning:
                'The empty state when the latest sample has no blocked session. Its text reads No blocked sessions were present in the most recent 10-second sample.',
              action:
                'This is not an error. A blocking episode shorter than the sampling interval can fall between two samples, so check [{{ui:shell.nav.waitEvents}}](/help/wait-events) for the waits over time.',
            },
          ],
        },
      ],
      subsections: [
        {
          id: 'keyboard',
          title: 'Keyboard',
          blocks: [
            {
              kind: 'text',
              text: 'Each node of the tree is a tree item. Arrow Right expands a collapsed node that has children, Arrow Left collapses an expanded node, and Arrow Down and Arrow Up move to the next and previous visible node. A node with children has an Expand or Collapse button, named Expand lock session or Collapse lock session followed by the {{ui:locks.sessions.pid}}.',
            },
          ],
        },
      ],
    },
    {
      id: 'activity-tiles',
      title: 'Activity',
      blocks: [
        {
          kind: 'text',
          text: 'The Activity view summarises the connections and the transactions of the instance, sampled every 10 seconds. Breakdowns show the connections per state and per database, and per application when the agent reports it.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.activity.connectionsUsed}}',
              meaning: 'The connections in use on the instance.',
            },
            {
              term: '{{ui:locks.activity.saturation}}',
              meaning:
                'Connections in use as a share of `max_connections`. The status reads Near max_connections. above the 80 % threshold of the advisor rule `conn.saturation`, and Below the saturation threshold. below it. Without a limit sample the tile reads {{ui:locks.activity.noLimitSample}}',
              action:
                'Look for idle sessions in the {{ui:locks.sessions.applicationTitle}} view. When many are idle, put a connection pooler in front of the server. Raise `max_connections` only when the server memory allows it.',
            },
            {
              term: '{{ui:locks.activity.longestTransaction}}',
              meaning: 'The age of the oldest open transaction of the instance.',
              action:
                'Sort the {{ui:locks.sessions.applicationTitle}} view by {{ui:locks.sessions.transactionAge}}, and check whether the application commits its transactions.',
            },
            {
              term: '{{ui:locks.activity.longestIdle}}',
              meaning:
                'The age of the longest session that is idle in transaction. Such a session keeps its locks and holds back cleanup while it waits.',
              action:
                'Filter the {{ui:locks.sessions.applicationTitle}} view with {{ui:locks.filter.idleInTransactionOnly}}, and fix the application code that leaves the transaction open.',
            },
            {
              term: '{{ui:locks.activity.oldestStateAge}}',
              meaning: 'How long the oldest session has been in its current state.',
            },
            {
              term: '{{ui:locks.activity.byDatabase}}',
              meaning:
                'The connections of the instance grouped by database. The block reads {{ui:locks.activity.noSamples}} when no row was sent.',
            },
            {
              term: '{{ui:locks.activity.byState}}',
              meaning:
                'The connections of the instance grouped by state, as PostgreSQL reports it.',
            },
            {
              term: '{{ui:locks.activity.preparedTransactions}}',
              meaning:
                'The number of open prepared transactions, the two-phase commit transactions. A prepared transaction keeps its locks until someone commits or rolls it back.',
              action:
                'List them with the SQL below, and commit or roll back the ones that no coordinator still needs.',
            },
            {
              term: '{{ui:locks.activity.oldestPrepared}}',
              meaning: 'The age of the oldest prepared transaction.',
            },
            {
              term: '{{ui:locks.activity.sinceFreeze}}',
              meaning:
                'Transaction IDs used since the oldest database was last frozen. The note under the tile says that the advisor raises a critical finding at about 1 billion transaction IDs, before they run out.',
              action:
                'Run `VACUUM` on the tables of the databases with the oldest age, starting from the oldest, and check the age again after the run.',
            },
            {
              term: '{{ui:locks.activity.deadlockRate}}',
              meaning:
                'Deadlocks per second, the sum of each database’s latest rate. Identifying the statements involved requires PostgreSQL log analysis.',
              action:
                'Read the deadlock reports in the PostgreSQL log. [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#summary-tiles) describes the {{ui:instance.overview.tile.deadlocksInRange}} tile.',
            },
            {
              term: '{{ui:locks.activity.byApplication}}',
              meaning:
                'The connections of the instance grouped by application name. When the agent does not report per-application counts, the block shows Disabled, names the feature Per-application activity counts and the setting `checks.activity.by_application`.',
            },
          ],
        },
        {
          kind: 'sql',
          label: '{{ui:locks.activity.preparedTransactions}}, oldest first',
          code: 'SELECT gid, prepared, owner, database FROM pg_prepared_xacts ORDER BY prepared;',
        },
        {
          kind: 'sql',
          label: 'Transaction ID age of each database',
          code: 'SELECT datname, age(datfrozenxid) AS xid_age FROM pg_database ORDER BY xid_age DESC;',
        },
      ],
    },
    {
      id: 'xmin-holder',
      title: '{{ui:locks.activity.xminHolder}}',
      blocks: [
        {
          kind: 'text',
          text: 'The oldest cleanup blocker is the session or the replication slot that holds the oldest xmin horizon. VACUUM cannot remove a dead row newer than that horizon, so while this holder stays open its tables keep accumulating dead rows and bloat.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.activity.holderKind}}',
              meaning:
                'What holds the horizon: a Session, a Replication slot, a Standby or a Prepared transaction.',
            },
            {
              term: '{{ui:locks.sessions.pid}}',
              meaning:
                'The identity of the holder, named by its kind: {{ui:locks.sessions.pid}} for a Session, {{ui:cluster.slots.colName}} for a Replication slot, application_name for a Standby, Transaction gid for a Prepared transaction.',
              action:
                'End the session from [Signal actions](/help/locks#signal-actions) when its transaction can be lost, or let the application commit it.',
            },
            {
              term: '{{ui:locks.activity.ageXids}}',
              meaning:
                'How many transaction IDs old the horizon is. The larger the value, the older the holder.',
            },
            {
              term: '{{ui:locks.activity.timeSinceStart}}',
              meaning:
                'How long the holder has existed: for a Session the start of its transaction, for a Prepared transaction the prepare time. The cell reads not measured for this holder kind when the agent gives no start time.',
            },
            {
              term: 'xmin holder unknown: no activity snapshot',
              meaning:
                'The block shows this when there is no activity snapshot to name the holder.',
            },
            {
              term: 'Replication slot',
              meaning:
                'A replication slot holds the horizon while its consumer has not confirmed the changes. When the consumer is gone, the slot keeps the horizon and the WAL.',
              action:
                'Check the WAL that the slot keeps in [WAL directory](/help/instance-overview#wal-directory), and the bloat it causes in [Tables and indexes](/help/instance-overview#relations).',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Replication slots and the horizons they hold',
          code: 'SELECT slot_name, slot_type, active, xmin, catalog_xmin FROM pg_replication_slots;',
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'pg_drop_replication_slot removes the slot for good. A consumer that reconnects finds no slot, and the WAL the slot kept can then be removed. Run it only after you have confirmed that nothing reads the slot.',
        },
        {
          kind: 'sql',
          label: 'Drop a replication slot nothing reads',
          code: "SELECT pg_drop_replication_slot('slot_name');",
        },
      ],
    },
    {
      id: 'sessions',
      title: 'Sessions',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:locks.sessions.applicationTitle}} view and the pglens sessions view each show one table of sessions, with the columns {{ui:locks.sessions.pid}}, User, {{ui:locks.sessions.application}}, {{ui:locks.sessions.database}}, {{ui:locks.sessions.client}}, State, {{ui:locks.sessions.wait}}, {{ui:locks.sessions.transactionAge}}, {{ui:locks.sessions.queryAge}} and {{ui:locks.sessions.query}}. The table is sorted by {{ui:locks.sessions.transactionAge}}, largest first, so the oldest transactions come first. Its empty states read {{ui:locks.sessions.applicationEmpty}} and {{ui:locks.sessions.pglensEmpty}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.sessions.pid}}',
              meaning: 'The process ID of the session.',
            },
            {
              term: 'User',
              meaning: 'The role of the session.',
            },
            {
              term: '{{ui:locks.sessions.application}}',
              meaning: 'The application name of the session.',
            },
            {
              term: '{{ui:locks.sessions.database}}',
              meaning: 'The database the session is connected to.',
            },
            {
              term: '{{ui:locks.sessions.client}}',
              meaning: 'The client address of the session.',
            },
            {
              term: 'State',
              meaning: 'The state of the session, as PostgreSQL reports it.',
            },
            {
              term: '{{ui:locks.sessions.wait}}',
              meaning:
                'The wait event of the session. A session that waits on a lock appears in the {{ui:locks.blocking.title}} too.',
            },
            {
              term: '{{ui:locks.sessions.transactionAge}}',
              meaning:
                'How long the current transaction has been open. A high value on a session that is idle in transaction keeps its locks and holds back cleanup.',
              action:
                'Open the session details and read its State. Use [Signal actions](/help/locks#signal-actions) only after you have checked the application.',
            },
            {
              term: '{{ui:locks.sessions.queryAge}}',
              meaning: 'How long the current query has been running.',
            },
            {
              term: '{{ui:locks.sessions.query}}',
              meaning:
                'The query text of the session. The session details label it {{ui:locks.detail.queryTextRaw}} or {{ui:locks.detail.queryTextNormalised}}, according to its source.',
              action:
                'Find the statement in [{{ui:shell.nav.slowestQueries}}](/help/slowest-queries) when it is a recurring statement.',
            },
          ],
        },
        {
          kind: 'text',
          text: 'When the sample is cut, a note above the table says how many of the connections of this origin it holds, and that it keeps the sessions with the oldest transactions.',
        },
        {
          kind: 'sql',
          label: 'Sessions with the oldest open transactions',
          code: "SELECT pid, usename, datname, state, wait_event_type, wait_event, now() - xact_start AS xact_age FROM pg_stat_activity WHERE backend_type = 'client backend' AND state <> 'idle' ORDER BY xact_start;",
        },
      ],
      subsections: [
        {
          id: 'filters',
          title: 'Filters',
          blocks: [
            {
              kind: 'text',
              text: 'The filters above the table narrow the rows. They change only what the table shows.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:locks.detail.waitEvent}} type',
                  meaning: 'Keeps the sessions with the chosen wait event type.',
                },
                {
                  term: '{{ui:locks.sessions.database}}',
                  meaning: 'Keeps the sessions of the chosen database.',
                },
                {
                  term: '{{ui:locks.filter.waiting}}',
                  meaning:
                    'The option {{ui:locks.filter.waitingOnly}} keeps the sessions that wait on something.',
                },
                {
                  term: '{{ui:locks.filter.idleInTransaction}}',
                  meaning:
                    'The option {{ui:locks.filter.idleInTransactionOnly}} keeps the sessions that are idle in transaction.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'connection-summary',
      title: '{{ui:locks.connections.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:locks.connections.title}} counts the client connections of the instance. The tabs choose one of six groups: {{ui:locks.connections.tab.byOrigin}}, {{ui:locks.connections.tab.byApplication}}, {{ui:locks.connections.tab.byUser}}, {{ui:locks.connections.tab.byClient}}, {{ui:locks.connections.tab.byDatabase}} and {{ui:locks.connections.tab.byState}}. Each key of the group shows its count.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.connections.tab.byOrigin}}',
              meaning:
                'The origin of the connection: application for the connections of applications, and pglens for the connections of pglens.',
            },
            {
              term: '{{ui:locks.connections.tab.byApplication}}',
              meaning: 'Counts per application name.',
            },
            {
              term: '{{ui:locks.connections.tab.byUser}}',
              meaning: 'Counts per role.',
            },
            {
              term: '{{ui:locks.connections.tab.byClient}}',
              meaning: 'Counts per client address.',
            },
            {
              term: '{{ui:locks.connections.tab.byDatabase}}',
              meaning: 'Counts per database.',
            },
            {
              term: '{{ui:locks.connections.tab.byState}}',
              meaning: 'Counts per session state.',
            },
            {
              term: '(empty name)',
              meaning:
                'The key of the connections that carry no name in the group, shown as (empty name).',
            },
          ],
        },
        {
          kind: 'text',
          text: 'When the lock sample is cut, a note says that the sample holds only the sessions with the oldest transactions, and that these counts cover every client connection of the instance.',
        },
      ],
    },
    {
      id: 'session-detail',
      title: 'Session details',
      blocks: [
        {
          kind: 'text',
          text: 'The session details show these fields of one session.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.sessions.pid}}',
              meaning: 'The process ID of the backend.',
            },
            {
              term: '{{ui:locks.detail.origin}}',
              meaning:
                'Whether the session is an application connection or a pglens connection, shown by the origin badge.',
            },
            {
              term: 'State',
              meaning: 'The state of the session, as PostgreSQL reports it.',
            },
            {
              term: '{{ui:locks.detail.waitEvent}} type',
              meaning: 'The class of the wait event, as PostgreSQL reports it.',
            },
            {
              term: '{{ui:locks.detail.waitEvent}}',
              meaning:
                'The wait event itself, as PostgreSQL reports it. See [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:locks.sessions.transactionAge}}',
              meaning: 'Time since the current transaction started.',
            },
            {
              term: '{{ui:locks.sessions.queryAge}}',
              meaning: 'Time since the current query started.',
            },
            {
              term: '{{ui:locks.detail.stateAge}}',
              meaning: 'Time since the session entered its current state.',
            },
            {
              term: '{{ui:locks.detail.backendStart}}',
              meaning: 'When the backend process started, in the console time zone.',
            },
            {
              term: '{{ui:locks.detail.clientAddress}}',
              meaning: 'The address of the client that connected.',
            },
            {
              term: '{{ui:locks.sessions.application}}',
              meaning: 'The application name of the session.',
            },
            {
              term: 'User',
              meaning: 'The role of the session.',
            },
            {
              term: '{{ui:locks.sessions.database}}',
              meaning: 'The database the session is connected to.',
            },
            {
              term: '{{ui:locks.detail.queryId}}',
              meaning:
                'The identifier of the query, as PostgreSQL computes it when query identifiers are enabled.',
            },
            {
              term: '{{ui:locks.detail.blockedBy}}',
              meaning: 'The PIDs of the sessions that block this one.',
            },
            {
              term: 'Blocking',
              meaning: 'The PIDs of the sessions this one blocks.',
            },
            {
              term: '{{ui:locks.detail.queryTextRaw}}',
              meaning: 'The query text as the server reports it.',
            },
            {
              term: '{{ui:locks.detail.queryTextNormalised}}',
              meaning: 'The query text after the agent normalises it.',
            },
          ],
        },
      ],
    },
    {
      id: 'signal-actions',
      title: 'Signal actions',
      blocks: [
        {
          kind: 'text',
          text: 'Two actions act on one session. They appear in the {{ui:locks.blocking.title}}, in the session details and under the oldest cleanup blocker. {{ui:locks.signal.cancelQuery}} asks the server to cancel the query the session runs, with `pg_cancel_backend`, and the session stays connected. {{ui:locks.signal.terminateBackend}} ends the session, with `pg_terminate_backend`. Both act only on client backends. For a process that is not a client backend the actions are unavailable, and the note reads Signal actions unavailable: {{ui:locks.sessions.pid}} … is not a client backend.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.signal.cancelQuery}}',
              meaning:
                'Calls `pg_cancel_backend`. It stops the running query and leaves the session connected. A session that is idle in transaction runs no query, so the action does nothing there: use {{ui:locks.signal.terminateBackend}}.',
            },
            {
              term: '{{ui:locks.signal.terminateBackend}}',
              meaning:
                'Calls `pg_terminate_backend`. The client connection is closed and its open transaction is rolled back. The dialog says so before the command is sent.',
            },
            {
              term: '{{ui:locks.signal.cancelTitle}}',
              meaning:
                'The title of the confirmation dialog for {{ui:locks.signal.cancelQuery}}. It asks PostgreSQL to cancel the current query. Confirm with the button {{ui:locks.signal.cancelQuery}}.',
            },
            {
              term: '{{ui:locks.signal.terminateTitle}}',
              meaning:
                'The title of the confirmation dialog for {{ui:locks.signal.terminateBackend}}. Confirm with the button {{ui:locks.signal.terminateBackend}}.',
            },
            {
              term: '{{ui:locks.signal.keepRunning}}',
              meaning: 'Closes the dialog without sending the command.',
            },
            {
              term: 'allow_signal',
              meaning:
                'The setting `allow_signal` of the target still applies. The lock data does not carry it, so a refusal by the agent arrives as the result of the command, not as a disabled button.',
            },
            {
              term: '{{ui:locks.signal.staleDisabled}}',
              meaning:
                'The buttons are disabled while the lock sample is stale. They enable again when the next sample arrives.',
            },
            {
              term: '{{ui:locks.signal.viewAudit}}',
              meaning:
                'A link shown after a command has finished. It opens the audit tab of the {{ui:shell.nav.settings}} page.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Cancelling or ending sessions needs permission tier T2. Grant it with `monitoring_user.sql -v tier1=1 -v tier2=1`; tier 2 also needs tier 1.',
        },
        {
          kind: 'steps',
          items: [
            'Find the session in the {{ui:locks.blocking.title}}, and read its State, {{ui:locks.blocking.waitDuration}} and {{ui:locks.sessions.query}}.',
            'Choose {{ui:locks.signal.cancelQuery}} for a running query you want to stop, or {{ui:locks.signal.terminateBackend}} when the session itself must end.',
            'Read the dialog, then confirm with the button of the same name, or choose {{ui:locks.signal.keepRunning}}.',
            'Check the result in {{ui:locks.signal.viewAudit}}.',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: '{{ui:locks.signal.terminateBackend}} rolls back all the work of the session that is not committed, and the application sees a closed connection. Confirm that the transaction can be lost before you confirm.',
        },
      ],
    },
    {
      id: 'captured-incidents',
      title: '{{ui:locks.incidents.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'The agent watcher reads `pg_locks` and `pg_stat_activity`, read-only, when ASH sees a lock wait, a long transaction or a long query, and files what it finds as an event. The {{ui:locks.incidents.title}} view lists those events of the last 7 days for this server, newest first, at most 100. Select one to read it in full: the table and the row (ctid), the lock mode, the longest wait, who waited for whom, and every session with its user, application, client, state, timing, the row and tables it held, and its statement, which Copy copies. The alert notification names the same table and the first statements.',
        },
        {
          kind: 'text',
          text: 'A deadlock lives only for `deadlock_timeout` (1 s by default) before PostgreSQL cancels one of its transactions, so the watcher often misses the cycle itself. It then reads the deadlock counter of `pg_stat_database`, and when it grows it files a rebuilt deadlock: the sessions it saw waiting on a lock, or blocking one, in that database just before. For an `INSERT` that waits on a duplicate key, the table is the one the waiting session writes.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:locks.incidents.emptyTitle}}',
              meaning:
                'The agent filed no capture for this server in the last 7 days, or its watcher is off.',
              action:
                'Check `checks.watch.enabled` in the agent configuration; waits shorter than half a second are not captured.',
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
              term: '{{ui:locks.blocking.selectInstance}}',
              meaning: 'The page shows this when no instance is selected.',
              action:
                'Open the page from the {{ui:shell.nav.locks}} tab under the header of an instance.',
            },
            {
              term: 'No lock sample has been stored for this instance yet.',
              meaning:
                'The blocking tree has no sample yet. Locks are sampled every 10 seconds, and shorter episodes can be missed.',
              action:
                'Check that the agent runs and reaches the server. The header of [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#header) shows the Last seen time.',
            },
            {
              term: '{{ui:locks.blocking.staleData}}',
              meaning:
                'The latest lock sample is older than 20 seconds. The signal actions stay disabled.',
              action:
                'Check the Last seen value in the header of the instance. A Last seen that stops moving means the agent is not reporting.',
            },
            {
              term: '{{ui:locks.activity.staleData}}',
              meaning: 'The latest activity sample is older than 20 seconds.',
              action:
                'Check the agent as for the lock data, in [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#header).',
            },
            {
              term: '{{ui:locks.blocking.emptyTitle}}',
              meaning: 'The latest sample has no blocked session. This is the normal state.',
              action:
                'When the application still stalls, check the waits over a longer window in [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:locks.activity.noLimitSample}}',
              meaning:
                'The {{ui:locks.activity.saturation}} tile has no value for the limit, so it cannot compute a share of `max_connections`.',
              action:
                'Check that the agent runs and reports the instance. See [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#header).',
            },
            {
              term: '{{ui:locks.activity.noSamples}}',
              meaning: 'A breakdown of the Activity view has no rows in the sample.',
              action:
                'Check that the agent runs and reports the instance. See [{{ui:shell.nav.instanceOverview}}](/help/instance-overview#header).',
            },
            {
              term: '{{ui:locks.signal.staleDisabled}}',
              meaning: 'The lock sample is stale, so the buttons are disabled.',
              action: 'Wait for the next sample. The buttons enable when it arrives.',
            },
          ],
        },
      ],
    },
  ],
}
