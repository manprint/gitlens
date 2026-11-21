import type { HelpTopic } from '../../types'

export const logsTopic: HelpTopic = {
  id: 'logs',
  group: 'instance',
  title: 'PostgreSQL logs',
  summary:
    'The notable lines of the PostgreSQL server log of one instance (errors, crashes, timeouts, deadlocks, checkpoints and more), read by the agent, classified and shipped to the server, with the settings they need and what is never shipped.',
  page: {
    path: '/instances/:instanceId/logs',
    label: 'Open {{ui:shell.nav.logs}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:shell.nav.logs}} lists the log lines the agent shipped for one instance in the time range, newest first. Each line has a class: the agent drops ordinary lines and ships only the classes listed in [Classes](/help/logs#classes).',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:logs.classes.title}}',
              meaning:
                'The number of lines of each class in the time range. Click a class to filter the list, {{ui:logs.classes.all}} to clear it.',
            },
            {
              term: '{{ui:logs.events.title}}',
              meaning:
                'Time, severity, class, database, user and message of each line. Expand a line for its application, PID, SQLSTATE, query ID and the detail, hint, context and statement the agent shipped. {{ui:logs.row.query}} opens the statement in the query inspector when the line carries a query ID.',
            },
            {
              term: '{{ui:logs.filter.label}}',
              meaning:
                'Class, severity, database and a text the message must contain. The filters live in the address, so a filtered view can be shared. {{ui:logs.events.older}} pages back 100 lines at a time.',
            },
          ],
        },
      ],
    },
    {
      id: 'setup',
      title: 'Turning the log reader on',
      blocks: [
        {
          kind: 'text',
          text: 'The log reader is off by default. It reads only structured logs: jsonlog (PostgreSQL 15 or later) or csvlog. While a requirement is missing, the instance readiness report shows `RDY-LOG-01` with the fix.',
        },
        {
          kind: 'steps',
          items: [
            'On the server, turn on the logging collector and a structured destination, then restart PostgreSQL.',
            'Keep the server messages in English (`lc_messages`): checkpoints, autovacuum runs, slow statements and temporary files are recognised by their English text.',
            'In the agent configuration set `checks.logs.enabled: true` and choose the source (see [Sources](/help/logs#sources)), then restart the agent.',
          ],
        },
        {
          kind: 'sql',
          label: 'Server settings (as a superuser)',
          code: "ALTER SYSTEM SET logging_collector = on;\nALTER SYSTEM SET log_destination = 'jsonlog';\nALTER SYSTEM SET lc_messages = 'C';",
        },
      ],
      subsections: [
        {
          id: 'sources',
          title: 'Sources',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '`file`',
                  meaning:
                    'The agent reads `log_directory` on the database host. The agent user needs read access to it.',
                },
                {
                  term: '`sql`',
                  meaning:
                    'The agent reads the current log file through SQL, from any host. The monitoring role needs the grants below.',
                },
              ],
            },
            {
              kind: 'sql',
              label: 'Grants for the sql source',
              code: 'GRANT EXECUTE ON FUNCTION pg_read_binary_file(text, bigint, bigint, boolean) TO <monitoring_role>;\nGRANT EXECUTE ON FUNCTION pg_current_logfile(text) TO <monitoring_role>;',
            },
            {
              kind: 'text',
              text: 'The agent remembers how far it read each file, so a restart neither repeats nor skips lines; on its first run it starts at the end of the current file. Each interval it reads at most `max_bytes_per_interval` bytes and ships at most 200 lines.',
            },
          ],
        },
      ],
    },
    {
      id: 'privacy',
      title: 'What is never shipped',
      blocks: [
        {
          kind: 'text',
          text: 'A message is cut at 2000 bytes. Detail, hint, context and statement are shipped only with `statement_text: true` in the agent configuration. A text that may hold a credential, such as a password, is always withheld.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Reading the log never changes the server: the agent only reads files or calls read-only functions.',
        },
      ],
    },
    {
      id: 'classes',
      title: 'Classes',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: '`panic`, `server.crash`, `corruption`, `disk.full`, `memory.out`',
              meaning:
                'Severe faults. Each has an alert rule: see [Log alerts](/help/logs#alerts).',
            },
            {
              term: '`server.start`, `server.recovery`',
              meaning: 'The server started, or recovered after an unclean shutdown.',
            },
            {
              term: '`lock.deadlock`, `lock.wait`, `lock.timeout`, `serialization.failure`',
              meaning: 'Lock conflicts, with the statement when it is shipped.',
            },
            {
              term: '`statement.timeout`, `statement.canceled`, `idle_tx.timeout`, `slow.statement`',
              meaning:
                'Statements stopped by a timeout or a cancel, sessions closed in an idle transaction, and statements longer than `log_min_duration_statement`.',
            },
            {
              term: '`auth.failed`, `connection.limit`',
              meaning: 'Failed logins and connections refused for lack of slots.',
            },
            {
              term: '`checkpoint`, `checkpoint.frequent`, `autovacuum.run`, `temp_file`',
              meaning:
                'Maintenance lines; they need `log_checkpoints`, `log_autovacuum_min_duration` or `log_temp_files`.',
            },
            {
              term: '`replication.conflict`, `wraparound.warning`',
              meaning:
                'Queries canceled on a standby, and the warnings before transaction ID wraparound.',
            },
            {
              term: '`error.statement`, `fatal.other`, `warning.other`',
              meaning: 'Any other ERROR, FATAL or WARNING line.',
            },
          ],
        },
      ],
    },
    {
      id: 'alerts',
      title: 'Log alerts',
      blocks: [
        {
          kind: 'text',
          text: 'Four alert rules fire on the first matching line: `log.panic` and `log.corruption` and `log.disk_full` (critical), `log.out_of_memory` (warning). Their notification quotes up to three of the latest matching lines. See [Alert rules](/help/alert-rules).',
        },
      ],
    },
  ],
}
