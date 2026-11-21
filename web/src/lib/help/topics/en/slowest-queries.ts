import type { HelpTopic } from '../../types'

export const slowestQueriesTopic: HelpTopic = {
  id: 'slowest-queries',
  group: 'monitoring',
  title: 'Slowest queries',
  summary:
    'The statements of one server that take the most time, ranked by mean time per call or by total time, with their calls and their share of the listed time.',
  page: {
    path: '/queries',
    label: 'Open Slowest queries',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'A few statements usually account for most of the execution time of a server. This page lists the statements of one server in ranked order, so you can choose which one to tune first.',
        },
        {
          kind: 'text',
          text: 'The page reads the statements of the server from `metrics_statements`, built from `pg_stat_statements`. It loads at most 200 statements per request.',
        },
        {
          kind: 'steps',
          items: [
            'Choose the {{ui:queries.slow.serverLabel}}, and optionally a Database.',
            'Choose the ranking in {{ui:queries.slow.rankBy}}: {{ui:queries.slow.rankMean}} lists the statements that take longest each time they run; {{ui:queries.common.totalTime}} lists the statements that take most time overall.',
            'Open a statement from the {{ui:queries.common.query}} column to read its detail and plan: see [Statement page](/help/slowest-queries#statement-detail).',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'The page lists at most 200 statements, the top ones by the chosen ranking. A statement outside that top is not shown, even when it is slow by the other ranking.',
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
                  term: '{{ui:queries.slow.serverLabel}}',
                  meaning:
                    'The server whose statements are listed. In the {{ui:queries.slow.serverLabel}} list, a server that is not up is marked (down).',
                },
                {
                  term: 'Database',
                  meaning:
                    'Limits the list to one database. {{ui:queries.slow.allDatabases}} lists the statements of every database on the server.',
                },
                {
                  term: '{{ui:queries.slow.rankBy}}',
                  meaning:
                    'Chooses the ranking: {{ui:queries.slow.rankMean}}, for the statements that take longest each time they run, or {{ui:queries.common.totalTime}}, for the statements that take most time overall. The choice is kept in the page address.',
                },
                {
                  term: '{{ui:queries.list.searchLabel}}',
                  meaning:
                    'Filters the statements by the typed term and highlights it in the {{ui:queries.common.query}} column. The search covers every statement collected for this server and database in the range, not only the ranked ones.',
                },
                {
                  term: 'Time range',
                  meaning:
                    'Set in the header, with presets such as Last 15 minutes and Last 30 days, or {{ui:shell.range.custom}}. The values on the page cover the selected range.',
                },
                {
                  term: 'Updated',
                  meaning: 'How long ago the statement data was last updated.',
                },
              ],
            },
          ],
        },
        {
          id: 'calculation',
          title: 'How the numbers are computed',
          blocks: [
            {
              kind: 'text',
              text: '{{ui:queries.common.meanTime}} is `total_exec_time` divided by `calls`. {{ui:queries.common.calls}} and total time are stored as rates per sample and multiplied by the sample duration; a sample without duration counts as 60 seconds.',
            },
          ],
        },
      ],
    },
    {
      id: 'summary-tiles',
      title: 'Summary tiles',
      blocks: [
        {
          kind: 'text',
          text: 'The four tiles above the table describe the statements the page loaded, not the whole server.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:queries.slow.slowestMean}}',
              meaning:
                'The highest mean execution time per call among the ranked statements, in milliseconds or the largest fitting unit.',
            },
            {
              term: '{{ui:queries.slow.totalTimeTile}}',
              meaning:
                'The execution time summed over the ranked statements, not over the whole server.',
            },
            {
              term: '{{ui:queries.common.calls}}',
              meaning: 'The number of calls summed over the ranked statements.',
            },
            {
              term: '{{ui:queries.slow.statementsRanked}}',
              meaning:
                'The number of statements the page loaded and ranked, up to 200. The detail adds `some missing` when the server returned a truncated list.',
              action: 'Narrow the Database filter to list the statements of one database.',
            },
          ],
        },
      ],
    },
    {
      id: 'table',
      title: 'Slowest queries table',
      blocks: [
        {
          kind: 'text',
          text: 'One row per statement, in the order of the chosen ranking: rank 1 is the highest value. The table shows 20 rows per page. The {{ui:queries.slow.rank}} column cannot be sorted; the order follows {{ui:queries.slow.rankBy}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:queries.slow.rank}}',
              meaning: 'Position in the chosen ranking; 1 is the highest value.',
            },
            {
              term: '{{ui:queries.common.query}}',
              meaning:
                'The statement text, with the search term highlighted. The link opens the statement page. The copy button copies the statement text returned by the server.',
              action:
                'Open the statement page to read its detail and plan: see [Statement page](/help/slowest-queries#statement-detail).',
            },
            {
              term: 'Database',
              meaning: 'The database where the statement ran.',
            },
            {
              term: '{{ui:queries.common.calls}}',
              meaning: 'The number of calls of the statement over the selected range.',
            },
            {
              term: '{{ui:queries.common.meanTime}}',
              meaning: 'Average execution time of one call, from pg_stat_statements.',
              action:
                'Open the statement page and read its plan. If a missing index may be the cause, see [{{ui:indexes.page.tabMissing}}](/help/indexes#missing).',
            },
            {
              term: '{{ui:queries.common.totalTime}}',
              meaning:
                'Execution time of all calls in the selected range, from pg_stat_statements.',
              action:
                'A high total time is the first candidate to tune, even with a small mean time, because the statement uses that time overall. Reduce the time per call, or the number of calls.',
            },
            {
              term: '{{ui:queries.list.shareOfListed}}',
              meaning:
                'The statement’s total time divided by the total time of all statements listed in the table. Statements not listed are not counted.',
              action:
                'Use the share to compare the listed statements. It is not a share of all the database time.',
            },
          ],
        },
      ],
    },
    {
      id: 'statement-detail',
      title: 'Statement page',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:queries.common.query}} link opens the statement page, with its metrics, its text and its plan history. See [{{ui:shell.nav.queryPerformance}}](/help/query-performance).',
        },
        {
          kind: 'steps',
          items: [
            'Read {{ui:queries.common.meanTime}} and {{ui:queries.common.totalTime}} over the selected range, and compare them with the other statements of the list.',
            'To read a plan with timings, use {{ui:queries.explain.planAnalyze}}: the page asks {{ui:queries.explain.analyzeTitle}} and runs it only after you confirm.',
            'Check whether a missing index would help: see [{{ui:indexes.page.tabMissing}}](/help/indexes#missing).',
            'Change one thing at a time, then compare the same statement over a range that starts after the change.',
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: '`EXPLAIN ANALYZE` executes the statement. For `INSERT`, `UPDATE`, `DELETE` or other statements with side effects, check the effect before you confirm.',
        },
        {
          kind: 'sql',
          label: 'Statements with the highest total time (PostgreSQL 13 or later)',
          code: 'SELECT queryid, calls, round(mean_exec_time::numeric, 2) AS mean_ms, round(total_exec_time::numeric, 2) AS total_ms, left(query, 120) AS query FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 20;',
        },
      ],
    },
    {
      id: 'where-to-start',
      title: 'Where to start',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Start from {{ui:queries.common.totalTime}}: the statements at the top use the most server time overall.',
            'Then check {{ui:queries.slow.rankMean}}: these are the statements that each caller waits for on every run.',
            'For each candidate, read its plan and its share of the listed time before you change anything.',
            'Apply one change at a time, and compare the figures over a range that starts after the change.',
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Empty or missing values',
      blocks: [
        {
          kind: 'terms',
          items: [
            {
              term: 'pg_stat_statements is not available',
              meaning:
                'The instance does not report `pg_stat_statements`, so there is nothing to rank.',
              action:
                'Add `pg_stat_statements` to `shared_preload_libraries`, create the extension, then restart the agent.',
            },
            {
              term: 'No monitored servers yet',
              meaning: 'No instance is monitored yet, so no statement is collected.',
              action:
                'Set up the pglens agent on a PostgreSQL host to start collecting statements.',
            },
            {
              term: '{{ui:queries.slow.noStatementsYet}}',
              meaning:
                'pg_stat_statements has no entries for the selected server, for the selected database if there is one, and for the selected range.',
              action:
                'Choose another {{ui:queries.slow.serverLabel}} or Database, or widen the time range in the header.',
            },
            {
              term: 'No statement matches the search',
              meaning:
                'The title shows the typed term in quotes, followed by “in this range”: the term matches no statement in the selected range.',
              action: 'Try another term or a wider range, or use {{ui:queries.list.showAll}}.',
            },
            {
              term: '{{ui:queries.slow.textTruncated}}',
              meaning:
                'The statement text is marked as truncated. The copy button still copies the text the server returned.',
            },
          ],
        },
      ],
    },
  ],
}
