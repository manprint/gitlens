import type { HelpTopic } from '../../types'

export const queryPerformanceTopic: HelpTopic = {
  id: 'query-performance',
  group: 'instance',
  title: 'Query performance',
  summary:
    'The query inspector ranks the statements of one instance from `pg_stat_statements`. The query detail shows one statement with its metrics, its execution plan and its plan history.',
  page: {
    path: '/instances/:instanceId/queries',
    label: 'Open {{ui:shell.nav.queryPerformance}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:shell.nav.queryPerformance}} answers one question: which SQL statements of this instance use the most time, calls, rows, I/O or WAL, and what their execution plans look like. The page has two levels. The query inspector ranks the statements of the instance. The query detail shows one statement with its metrics, its execution plan and its plan history. The statistics come from `pg_stat_statements`, collected by the agent and stored by the server. Plans come only from the EXPLAIN commands you request from the page.',
        },
      ],
      subsections: [
        {
          id: 'how-to-open',
          title: 'How to open it',
          blocks: [
            {
              kind: 'steps',
              items: [
                'Open {{ui:shell.nav.fleet}} at `/`, choose the cluster, then the instance.',
                'In the instance page, open the tab {{ui:shell.nav.queryPerformance}}. The sidebar group {{ui:shell.nav.instance}} lists the same page.',
                'Click `Query <queryid>` in the inspector to open a statement with its plans.',
              ],
            },
          ],
        },
        {
          id: 'page-controls',
          title: 'Page controls',
          blocks: [
            {
              kind: 'text',
              text: 'The controls that change the page are the time range, the sort select, the search box and the URL parameter `db`. The tabs of the instance header and the sidebar entries of this server carry `db` into the next view, so the database scope stays the same. They do not carry the sort or the search: those start again from the defaults.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:shell.range.preset}}',
                  meaning:
                    'Selects the range the page reads, from the time range picker (the select labelled {{ui:shell.range.preset}}). Counters such as calls, time, rows, blocks and bytes are summed over that range, and the ranking uses the same sums.',
                  action:
                    'Widen the range when a statement you expect is missing: it may have run before the range or have been evicted.',
                },
                {
                  term: '{{ui:queries.list.sortLabel}}',
                  meaning:
                    'The measure the statements are ranked by before the top 50 are kept. Details: [{{ui:queries.list.sortLabel}}](/help/query-performance#sort-statements).',
                },
                {
                  term: '{{ui:queries.list.searchLabel}}',
                  meaning:
                    'Matches query text, `queryid` or database over the whole range. Details: [{{ui:queries.list.searchLabel}}](/help/query-performance#search-statements).',
                },
              ],
            },
          ],
        },
        {
          id: 'data-sources',
          title: 'Where the data comes from',
          blocks: [
            {
              kind: 'text',
              text: 'The agent’s `stat_statements` check writes the counters of `pg_stat_statements` to the server: one row per `queryid` and database. The server sums those counters over the selected range, ranks the statements and keeps the top 50. The share of listed time is computed in the browser. Plans are not collected automatically: the agent runs an EXPLAIN only when you request one from the query detail, and the server keeps the result in the plan history.',
            },
          ],
        },
      ],
    },
    {
      id: 'query-inspector',
      title: '{{ui:queries.list.inspectorTitle}}',
      blocks: [
        {
          kind: 'text',
          text: 'The route `/instances/:instanceId/queries` shows the page {{ui:queries.list.inspectorTitle}}. Its subtitle reads: every statement `pg_stat_statements` recorded on this instance in the selected time range. Change the ranking to find the slowest, most frequent or most I/O-heavy queries, and open one to see its plan. The list shows at most 50 statements.',
        },
      ],
      subsections: [
        {
          id: 'sort-statements',
          title: '{{ui:queries.list.sortLabel}}',
          blocks: [
            {
              kind: 'text',
              text: 'The sort select, next to the hint {{ui:queries.list.sortLabel}}, chooses the measure the server ranks statements by before it keeps the top 50. The choice is kept in the URL as `sort`. The default is {{ui:queries.common.totalTime}}. Clicking a column header only reorders the rows already loaded: it does not change which statements are in the list.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:queries.common.totalTime}}',
                  meaning:
                    'Ranks by the execution time of all calls in the range, from `pg_stat_statements`. This is the default: it finds the statements that cost the database most overall.',
                  action: 'Start with the top rows. Open each one and request a plan.',
                },
                {
                  term: '{{ui:queries.common.meanTime}}',
                  meaning:
                    'Ranks by the average execution time of one call. It finds statements that are slow on every call, even when they run rarely.',
                  action:
                    'Open the statement and request [{{ui:queries.explain.planOnly}} (EXPLAIN)](/help/query-performance#plan-only).',
                },
                {
                  term: '{{ui:queries.common.calls}}',
                  meaning:
                    'Ranks by the number of executions in the range. It finds the most frequent statements; a small mean time multiplied by many calls can still weigh on the database.',
                  action:
                    'Check whether the application runs the same statement in a loop. Fewer round trips often help more than tuning one call.',
                },
                {
                  term: '{{ui:queries.common.rows}}',
                  meaning:
                    'Ranks by the rows returned or affected in the range. It finds statements that move large result sets.',
                  action:
                    'Check the filters and any LIMIT in the application query: the statement may return more rows than the application uses.',
                },
                {
                  term: '{{ui:queries.list.sortSharedRead}}',
                  meaning:
                    'Ranks by the shared blocks the statements had to read in the range. A read may still come from the operating system cache, so it is not always disk I/O.',
                  action:
                    'Open the statement and read its {{ui:queries.overview.cacheHitRatio}}. Review the indexes of the tables it reads in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
                },
                {
                  term: '{{ui:queries.list.sortTempWritten}}',
                  meaning:
                    'Ranks by the temporary blocks written in the range, usually when a sort or a hash does not fit in `work_mem`.',
                  action:
                    'Check {{ui:queries.list.tempBytesPerCall}} in the table and the advisor rule `query.temp_bytes_high`. Remedies: an index that returns rows already sorted, a rewritten query, or a larger `work_mem` for that session.',
                },
                {
                  term: '{{ui:queries.list.sortWal}}',
                  meaning:
                    'Ranks by the write-ahead log bytes generated in the range. It finds the statements that write most.',
                  action:
                    'Check whether the statement writes more rows than its purpose needs, and whether it runs in a loop or in large batches.',
                },
              ],
            },
          ],
        },
        {
          id: 'statement-statistics',
          title: 'Statement statistics',
          blocks: [
            {
              kind: 'text',
              text: 'The table Statement statistics has one row per statement: one `queryid` in one database. Columns, from left: Query, {{ui:queries.common.calls}}, {{ui:queries.common.totalTime}}, {{ui:queries.common.meanTime}}, {{ui:queries.list.shareOfListed}}, Rows, {{ui:queries.overview.cacheHitRatio}}, WAL and {{ui:queries.list.tempBytesPerCall}}. A column header with a hint explains the column; the terms below give the same meaning.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:queries.common.query}}',
                  meaning:
                    '`Query <queryid>` links to the detail page of the statement. Under the link, the normalised text opens on click. `queryid` is the id `pg_stat_statements` gives the statement.',
                  action:
                    'Open the statement to see its plan. The link carries `db`, the statement’s database, so the plan request runs there.',
                },
                {
                  term: '{{ui:queries.common.calls}}',
                  meaning:
                    'Executions of the statement in the range, summed. {{ui:shell.hint.unit}}: calls. Read it together with {{ui:queries.common.meanTime}}.',
                  action:
                    'Check whether the application runs the same statement in a loop. Fewer round trips often help more than tuning one call.',
                },
                {
                  term: '{{ui:queries.common.totalTime}}',
                  meaning:
                    'Execution time of all calls in the selected range, in milliseconds, from `pg_stat_statements`.',
                  action:
                    'Start with the statements at the top: they cost the database most in total.',
                },
                {
                  term: '{{ui:queries.common.meanTime}}',
                  meaning:
                    'Average execution time of one call, in milliseconds: {{ui:queries.common.totalTime}} divided by {{ui:queries.common.calls}}, from `pg_stat_statements`.',
                  action:
                    'A high mean with few calls means one expensive execution. Request [{{ui:queries.explain.planOnly}} (EXPLAIN)](/help/query-performance#plan-only) and read the node with the highest total cost.',
                },
                {
                  term: '{{ui:queries.list.shareOfListed}}',
                  meaning:
                    'The statement’s {{ui:queries.common.totalTime}} divided by the total time of all statements listed in the table, in percent. Statements not listed are not counted.',
                  action:
                    'A large share means the statement dominates the listed workload. The advisor rule `query.total_time_share` raises a warning when one statement takes more than 30% of the total execution time the advisor loads.',
                },
                {
                  term: '{{ui:queries.common.rows}}',
                  meaning:
                    'Rows returned or affected by the statement in the range, summed over its calls. The detail page shows {{ui:queries.overview.rowsPerCall}}.',
                  action:
                    'A large count with few calls points to large result sets. Check the filters and any LIMIT in the application query.',
                },
                {
                  term: '{{ui:queries.overview.cacheHitRatio}}',
                  meaning:
                    'Shared-buffer block hits divided by hits plus reads, for this statement in the range, in percent. A read may still come from the operating system cache, so a low ratio is not always disk I/O.',
                  action:
                    'The advisor rule `query.cache_miss_high` is info: it fires when more than 30% of the blocks read come from outside shared buffers, on a statement with more than 1000 calls. Review the indexes of the tables it reads in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
                },
                {
                  term: '{{ui:queries.list.wal}}',
                  meaning:
                    '{{ui:queries.list.sortWal}} written by the statement in the range, estimated from per-second rates.',
                  action:
                    'Check whether the statement writes more rows than its purpose needs, and whether it runs in a loop or in large batches.',
                },
                {
                  term: '{{ui:queries.list.tempBytesPerCall}}',
                  meaning:
                    'Temporary bytes written per call in the range: temporary blocks written, 8 KiB each, divided by calls. Temporary files appear when a sort or a hash does not fit in `work_mem`.',
                  action:
                    'The advisor rule `query.temp_bytes_high` warns when the average is above `work_mem`, or above 4 MiB when the setting is not read. Remedies: an index that returns rows already sorted, a rewritten query, or a larger `work_mem` for that session.',
                },
              ],
            },
          ],
        },
        {
          id: 'search-statements',
          title: '{{ui:queries.list.searchLabel}}',
          blocks: [
            {
              kind: 'text',
              text: 'The box {{ui:queries.list.searchLabel}} matches query text, `queryid` or database over the whole range. The search reads every statement collected on this instance in the range, not only the 50 rows listed, so a statement can appear when the default ranking would hide it. The term stays in the URL as `q`.',
            },
          ],
        },
        {
          id: 'limit-and-eviction',
          title: 'Limit, eviction and truncation',
          blocks: [
            {
              kind: 'note',
              tone: 'info',
              text: 'The list keeps the top 50 statements. When the server has more, a notice reads Showing N of 50 statements (truncated; budget 50). The top-N budget is configured by `checks.stat_statements.top_n`.',
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'PostgreSQL keeps at most `pg_stat_statements.max` statements. Entries beyond that limit are evicted and may appear and disappear between ranges.',
            },
          ],
        },
        {
          id: 'list-states',
          title: 'Disabled and empty states',
          blocks: [
            {
              kind: 'note',
              tone: 'warning',
              text: 'When `pg_stat_statements` is not available, a banner reads “pg_stat_statements disabled.” and gives the reason: “The agent skips the stat_statements check: <reason>.” Without a reason it reads “The agent does not collect statements for this instance.” If older statements are still listed, the banner says they were collected earlier.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'No statements',
                  meaning: 'The list is empty for the selected instance, database and time range.',
                  action:
                    'Widen the time range. Check the URL parameter `db`: it limits the list to one database.',
                },
                {
                  term: 'No statement matches “<term>” in this range',
                  meaning:
                    'The search matches no statement in the range. The button {{ui:queries.list.showAll}} clears the search.',
                  action:
                    'Try another term, or widen the range. The search covers every statement collected in the range, not only the listed ones.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'query-detail',
      title: 'Query detail and plan history',
      blocks: [
        {
          kind: 'text',
          text: 'The route `/instances/:instanceId/queries/:queryid` opens from a row of the query inspector. The page has three parts: the statement with its metric tiles, the execution plan you can request now, and the plan history. The link from the list carries `db`, the statement’s database, when it is known. Without `db`, the plan history lists the plans of every database.',
        },
      ],
      subsections: [
        {
          id: 'detail-header',
          title: 'Header and freshness',
          blocks: [
            {
              kind: 'text',
              text: 'The header reads `Query <queryid>`, with the database as a badge when it is known. The button All queries returns to the inspector of the same instance. The freshness badge shows how old the statistics are: they refresh every 60 seconds, and the badge marks them stale after 180 seconds without an update.',
            },
          ],
        },
        {
          id: 'statement-text',
          title: '{{ui:queries.overview.statementText}}',
          blocks: [
            {
              kind: 'text',
              text: 'The section {{ui:queries.overview.statementText}} shows the statement as `pg_stat_statements` stores it: literal values are replaced by placeholders, so parameter values are not shown. The button Copy copies the text and then reads Copied. If the browser refuses, it reads Copy failed.',
            },
          ],
        },
        {
          id: 'metric-tiles',
          title: 'Metric tiles',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:queries.common.meanTime}}',
                  meaning: 'Average execution time of one call over the range.',
                  action:
                    'Compare it with the plan. A high value with few calls points to one expensive plan: see [{{ui:queries.explain.planOnly}} (EXPLAIN)](/help/query-performance#plan-only).',
                },
                {
                  term: '{{ui:queries.common.totalTime}}',
                  meaning:
                    'Execution time of all calls over the range. It is the measure that makes a statement weigh on the database.',
                  action:
                    'Compare it with the other statements: in the inspector, sort by {{ui:queries.common.totalTime}} and read {{ui:queries.list.shareOfListed}}.',
                },
                {
                  term: '{{ui:queries.common.calls}}',
                  meaning: 'Number of executions over the range.',
                  action:
                    'A high count with a small mean suggests a loop in the application that repeats the statement.',
                },
                {
                  term: '{{ui:queries.overview.rowsPerCall}}',
                  meaning: 'Rows returned or affected per call.',
                  action:
                    'A large value means large results on each call. Check the filters and any LIMIT in the application query.',
                },
                {
                  term: '{{ui:queries.overview.cacheHitRatio}}',
                  meaning:
                    'Share of the shared blocks this statement accessed that were found in shared buffers, in percent. A read may still come from the operating system cache, so a lower ratio is not always disk I/O.',
                  action:
                    'Review the indexes of the tables it reads in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
                },
                {
                  term: 'WAL generated',
                  meaning: 'Write-ahead log generated by all calls of the statement, in bytes.',
                  action:
                    'Check whether it writes more rows than needed, and whether it runs in a loop or in large batches.',
                },
              ],
            },
          ],
        },
        {
          id: 'missing-statement',
          title: 'Statement not in the range',
          blocks: [
            {
              kind: 'text',
              text: 'The page looks the statement up among the first 1000 statements of the range, ordered by {{ui:queries.common.totalTime}}, within `db` when it is set. If the statement is not among them, the metric tiles are replaced by a message that no `pg_stat_statements` activity was recorded for this query in the range, so only its plans are shown. The plans stay available below. Widen the time range, or check the statement in the inspector.',
            },
          ],
        },
      ],
    },
    {
      id: 'execution-plan',
      title: 'Execution plan',
      blocks: [
        {
          kind: 'text',
          text: 'The section Execution plan shows how PostgreSQL runs this statement: the operations it chooses, the rows each one handles and where the time goes. The agent runs `EXPLAIN` on the monitored database, always in a read-only transaction that is rolled back, and the server keeps the result in the plan history below. The request is a command: while it is pending, the buttons are disabled and the panel reads Command pending: waiting for the agent.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'Before planning, the agent reads the code of the user functions the statement can reach, through views and other functions. It refuses only when that code can write or run dynamic SQL (`EXECUTE`, `COPY`, `DO`) or is written in C or internal; the refusal names the function. Plain functions, such as a cast check, are planned normally.',
        },
      ],
      subsections: [
        {
          id: 'plan-only',
          title: '{{ui:queries.explain.planOnly}} (EXPLAIN)',
          blocks: [
            {
              kind: 'text',
              text: 'The button {{ui:queries.explain.planOnly}} (its accessible name is Run plan only) asks the planner for a plan and runs nothing: costs and rows are estimates. It needs the permission tier T1. Below T1 the button stays disabled, and the panel shows Requires T1 permission; current tier is T0.',
            },
          ],
        },
        {
          id: 'plan-with-analyze',
          title: '{{ui:queries.explain.planAnalyze}}',
          blocks: [
            {
              kind: 'text',
              text: 'The button {{ui:queries.explain.planAnalyze}} runs the statement to measure real times and rows, with EXPLAIN ANALYZE. The console offers it only at tier T2, and the agent must also allow it with `allow_explain_analyze`. Before the request, a dialog titled Run EXPLAIN ANALYZE? explains the impact. Cancel stops; Run EXPLAIN ANALYZE sends the request.',
            },
            {
              kind: 'note',
              tone: 'warning',
              text: 'EXPLAIN ANALYZE runs the statement. It runs inside a read-only transaction that is rolled back afterwards, but the time, resources and locks it uses are real. On a busy primary, prefer {{ui:queries.explain.planOnly}}, or run ANALYZE when the load is low.',
            },
          ],
        },
        {
          id: 'plan-parameters',
          title: 'Parameter values',
          blocks: [
            {
              kind: 'text',
              text: 'pg_stat_statements replaces the constants of a statement with `$1`, `$2`, …. When the statement has them, the box Parameter values offers one input per placeholder. Type each value as you would in SQL, without quotes; tick NULL for SQL NULL. Fill all of them or none: with some missing, both buttons stay disabled.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Values you entered',
                  meaning:
                    'The agent binds your values as parameters, never written into the statement text, and PostgreSQL plans for them.',
                },
                {
                  term: 'Values of a recent execution',
                  meaning:
                    'With the inputs empty, ANALYZE takes the statement of a session that pg_stat_activity shows running it in the same database. When no session runs it, the agent refuses and asks for the values.',
                },
                {
                  term: 'Generic plan, no values',
                  meaning:
                    'With the inputs empty, {{ui:queries.explain.planOnly}} asks for a generic plan, valid for any value (PostgreSQL 16 and later). With real values PostgreSQL may choose another plan.',
                },
              ],
            },
          ],
        },
        {
          id: 'plan-result',
          title: 'Query plan',
          blocks: [
            {
              kind: 'text',
              text: 'After the agent answers, the section Query plan shows a summary (total cost, execution and planning time, rows, buffers, where the parameter values came from), the box What to look at, and the plan. The switch Tree, Text, JSON changes the view: Tree is the table Query plan tree; Text prints the plan as psql does, to paste where EXPLAIN text is read; JSON is the plan exactly as the agent returned it. Text and JSON have a copy button.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'What to look at',
                  meaning:
                    'The problems pglens reads in the plan, most serious first: the operation that takes most of the time, row estimates 10 or more times off, a Seq Scan that discards most of the rows it reads, a sort or hash that spilled to disk, a scan repeated thousands of times, operations never executed.',
                  action:
                    'Start here. {{ui:queries.planView.showInPlan}} opens the operation in the tree, with its details.',
                },
                {
                  term: 'Operation',
                  meaning:
                    'The step as PostgreSQL names it in its text format (Seq Scan, Hash Left Join, HashAggregate, …), the table or index it works on, and its conditions (Index Cond, Filter, Hash Cond, …). Children are indented under their parent. The arrow shows details: rows removed by a filter, sort method and memory, hash batches, workers, startup cost, row width, output columns.',
                },
                {
                  term: 'Rows (actual / est.)',
                  meaning:
                    'Rows the operation produced in all its loops, and under them the planner estimate ({{ui:queries.common.rowsEstimated}} in a plan only). A ×N under or ×N over badge marks an estimate N times off.',
                  action:
                    'A large gap means stale or missing statistics: run `ANALYZE` on the table, or `CREATE STATISTICS` for columns that depend on each other, then request a new plan.',
                },
                {
                  term: 'Loops',
                  meaning:
                    'How many times the operation ran, typically once per row of the outer side of a Nested Loop. Rows and time are already totals over all loops.',
                },
                {
                  term: '{{ui:queries.common.timeInNode}}',
                  meaning:
                    'Time spent in the operation itself, without the operations below it, and its share of the execution time; the bar turns amber from 30% and red from 60%. Under Gather, the time of the parallel workers is the wall time, not their sum. In a plan only the column is {{ui:queries.planView.costInNode}}, in planner units, not a time.',
                  action: 'The widest bar is where the time goes: optimise that operation first.',
                },
                {
                  term: 'Buffers',
                  meaning:
                    'Data pages the operation used, as 8 kB blocks: cached were found in shared buffers, read came from the operating system or disk.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'plan-history',
      title: 'Plan history',
      blocks: [
        {
          kind: 'text',
          text: 'Plan history keeps every plan requested above for this statement, newest first, at most 20 entries. pglens does not sample plans by itself: an entry exists only when someone asked for a plan. It answers one question: has PostgreSQL changed the way it runs this statement, and where? pglens fingerprints each plan’s shape: its operations, tables, indexes and conditions, not its costs or times. A new shape appears after a new or dropped index, fresh statistics, a setting change or other parameter values.',
        },
      ],
      subsections: [
        {
          id: 'plan-captures',
          title: 'Captured plans',
          blocks: [
            {
              kind: 'text',
              text: 'Above the table, N captures · M distinct shapes counts the entries and their shapes. The table Plan history entries has one row per capture. Tick one row to read that plan below the table, with the same view as Query plan; tick two to compare them. Ticking a third row clears the oldest of the two.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Kind',
                  meaning: 'EXPLAIN for a plan only, ANALYZE for a plan with real times.',
                },
                {
                  term: 'Shape',
                  meaning:
                    'The first characters of the shape fingerprint (`plan_hash`): two captures with the same code run the statement the same way. {{ui:queries.planHistory.newShape}} marks a capture whose shape differs from the capture before it.',
                  action:
                    'When {{ui:queries.planHistory.newShape}} appears, compare that capture with the one before it.',
                },
                {
                  term: '{{ui:queries.common.totalCost}}, Execution time, Rows',
                  meaning:
                    'Planner cost of the whole plan, measured execution time (ANALYZE only) and the rows returned, or expected (est.) for a plan only.',
                  action:
                    'Compare an EXPLAIN with an EXPLAIN by cost, and an ANALYZE with an ANALYZE by time.',
                },
                {
                  term: 'Top operation',
                  meaning: 'The operation at the root of the plan, with its table when it has one.',
                },
              ],
            },
          ],
        },
        {
          id: 'plan-comparison',
          title: 'Plan comparison',
          blocks: [
            {
              kind: 'text',
              text: 'With two captures ticked, the section Plan comparison sets the older against the newer, whatever order you ticked them in. A sentence says whether the shape is the same, or how many operations changed. {{ui:queries.common.totalCost}}, Execution time and Rows of the two plans follow, as older → newer with the change in percent: red when it grew, green when it shrank. The table Plan comparison lists the operations of both plans, matched by their place in the tree.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Change',
                  meaning:
                    '{{ui:queries.planHistory.changeSame}}: same operation, object and conditions at that place. {{ui:queries.planHistory.changeChanged}}: another operation, table, index or condition; the old one is struck through above the new one. {{ui:queries.planHistory.changeAdded}} and {{ui:queries.planHistory.changeRemoved}}: the operation exists only in the newer or only in the older plan.',
                },
                {
                  term: '{{ui:queries.common.totalCost}}, Rows, {{ui:queries.common.timeInNode}}',
                  meaning:
                    'Each figure as older → newer. {{ui:queries.common.timeInNode}} appears when both plans are ANALYZE; Rows are actual when both are ANALYZE, estimates otherwise.',
                },
                {
                  term: '{{ui:queries.planHistory.difference}}',
                  meaning:
                    'Filter with two options: {{ui:queries.planHistory.differs}} shows only the operations that changed, were added or removed; {{ui:queries.planHistory.changeSame}} shows only the ones that match.',
                  action:
                    'Start with {{ui:queries.planHistory.differs}} to find where the plans diverged: a Seq Scan that replaced an Index Scan, or a join method that changed, is the usual cause of a slower statement.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'trends-over-time',
      title: 'Trends over time',
      blocks: [
        {
          kind: 'text',
          text: 'The query pages have no charts: their figures are totals or averages over the selected range, not time series. For the trend, open the charts of [{{ui:shell.nav.instanceOverview}}](/help/instance-overview), section Queries: Calls per second, Execution time per second, Rows per second, Statement block reads and Statement temp blocks. Choose the range with the time range picker, then return here to find the statement.',
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'The {{ui:shell.nav.instanceOverview}} charts show the instance as a whole, not one statement.',
        },
      ],
    },
    {
      id: 'compare-periods',
      title: 'Comparing two periods',
      blocks: [
        {
          kind: 'text',
          text: 'Switch the page to {{ui:queries.compare.modeCompare}} to see each statement in two periods side by side. Period B is the range of the time range picker; period A is {{ui:queries.compare.preset.previous}}, {{ui:queries.compare.preset.yesterday}}, {{ui:queries.compare.preset.week}} or a {{ui:queries.compare.preset.custom}} range. Every figure is per second of its own period (the mean time stays per call), so periods of different length compare fairly.',
        },
        {
          kind: 'steps',
          items: [
            'Choose period B with the time range picker, for example the hour of the slowdown.',
            'Click {{ui:queries.compare.modeCompare}}, then choose {{ui:queries.compare.presetLabel}}.',
            'Choose the {{ui:queries.compare.metricLabel}}. Rows come by the size of the change, largest first.',
            'Read {{ui:queries.compare.colRatio}}: ×3.0 means three times period A, and a ratio of ×2.0 or more is marked. {{ui:queries.compare.new}} ran only in B, {{ui:queries.compare.gone}} only in A.',
            'Open a statement from its link to inspect it.',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'The comparison reads `GET /api/v1/statements/compare`; its URL keeps `mode=compare`, `compare` and, for a custom period, `a_from` and `a_to`. Each period can be at most 31 days.',
        },
      ],
    },
    {
      id: 'act-on-slow-query',
      title: 'Acting on a slow or regressed query',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Open the query inspector and keep {{ui:queries.common.totalTime}} to find the statements that cost the database most. Then choose {{ui:queries.common.meanTime}} to find those slow on every call.',
            'Open the statement. Read {{ui:queries.common.meanTime}} and {{ui:queries.common.calls}} in the tiles, then the {{ui:queries.overview.statementText}}.',
            'Click {{ui:queries.explain.planOnly}}. If it is disabled, the instance tier is below T1.',
            'Read the plan tree from the top. Check the node with the highest {{ui:queries.common.totalCost}}, large gaps between Estimated rows and Actual rows, and sorts or hashes that spill to disk, which show a high {{ui:queries.list.tempBytesPerCall}}.',
            'If you need measured times, click {{ui:queries.explain.planAnalyze}}, read the dialog, and confirm only when the impact is acceptable.',
            'Compare with the plan history: select an older plan and the new one, then set {{ui:queries.planHistory.difference}} to {{ui:queries.planHistory.differs}}. A changed node type or cost points to the change that slowed the statement.',
            'Review the tables it reads in [{{ui:shell.nav.indexAnalysis}}](/help/indexes). Apply the fix yourself, on a test copy first.',
            'Check the findings in [{{ui:shell.nav.findings}}](/help/findings): the advisor rule `query.regression` is critical when a statement’s mean time is at least 3 times its own baseline and at least 100 ms.',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'The inspector itself shows no regression flag. The regression is reported by the advisor rule below.',
        },
      ],
      subsections: [
        {
          id: 'advisor-rules',
          title: 'Advisor rules for queries',
          blocks: [
            {
              kind: 'text',
              text: 'The advisor evaluates the collected statistics and writes its results to the {{ui:shell.nav.findings}} page. These are the rules for queries; each finding has a severity and a state.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Slow query',
                  meaning:
                    'Rule `query.slow_mean`, severity warning: a statement with at least 10 calls and a mean execution time above 500 ms.',
                  action:
                    'Request [{{ui:queries.explain.planOnly}} (EXPLAIN)](/help/query-performance#plan-only) and read the node with the highest total cost.',
                },
                {
                  term: 'Dominant query',
                  meaning:
                    'Rule `query.total_time_share`, severity warning: one statement takes more than 30% of the total execution time the advisor loads.',
                  action: 'Start with this statement: it dominates the workload.',
                },
                {
                  term: 'Query regression',
                  meaning:
                    'Rule `query.regression`, severity critical: the mean time is at least 3 times the statement’s own baseline and at least 100 ms. The baseline is the median of its mean time over the earlier history, about eight days, excluding the last hour. Without a baseline the finding is degraded, with the reason insufficient query history.',
                  action:
                    'Compare the plans in the plan history from before and after the change in mean time.',
                },
                {
                  term: 'High temporary write volume',
                  meaning:
                    'Rule `query.temp_bytes_high`, severity warning: average temporary bytes per call above `work_mem`, or above 4 MiB when the setting is not read.',
                  action:
                    'See [{{ui:queries.list.tempBytesPerCall}}](/help/query-performance#statement-statistics).',
                },
                {
                  term: 'High cache miss ratio',
                  meaning:
                    'Rule `query.cache_miss_high`, severity info: more than 30% of the blocks read come from outside shared buffers, on a statement with more than 1000 calls.',
                  action:
                    'Review the indexes of the tables it reads in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
                },
              ],
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
              term: 'pg_stat_statements disabled',
              meaning:
                'The agent skipped the stat_statements check, so no new statement statistics are collected. The banner gives the reason.',
              action:
                'Add `pg_stat_statements` to `shared_preload_libraries`, restart PostgreSQL, then create the extension in a monitored database. Check first with the SQL checks below.',
            },
            {
              term: '{{ui:queries.explain.planAnalyze}} disabled',
              meaning:
                'The button is disabled, or the panel shows Requires T2 permission. The console needs tier T2, and the agent must allow `allow_explain_analyze`.',
              action:
                'Check the permission tier of the instance and the agent configuration. Keep ANALYZE for the statements whose impact you accept.',
            },
            {
              term: 'No plan history yet',
              meaning: 'No plan has been requested for this statement yet.',
              action: 'Click {{ui:queries.explain.planOnly}} to create the first entry.',
            },
            {
              term: 'Command pending: waiting for the agent.',
              meaning:
                'The request is waiting for the agent to pick it up, or it is still running.',
              action: 'Wait. If the message stays, check that the agent is running and connected.',
            },
            {
              term: 'Database not monitored.',
              meaning:
                'The `db` parameter names a database that is not monitored on this instance. The banner reads “Database not monitored.” and the statement is not explained or planned here.',
              action:
                'Open the statement again from the inspector, or check the database list in [{{ui:shell.nav.settings}}](/help/settings).',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Check that the extension is installed in this database',
          code: "SELECT extname, extversion FROM pg_extension WHERE extname = 'pg_stat_statements';",
        },
        {
          kind: 'sql',
          label: 'Check the preload setting',
          code: 'SHOW shared_preload_libraries;',
        },
        {
          kind: 'sql',
          label: 'Top statements by total time (PostgreSQL 13 or later)',
          code: 'SELECT queryid, calls, round(total_exec_time::numeric, 1) AS total_ms, round(mean_exec_time::numeric, 1) AS mean_ms, left(query, 80) AS query_start FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 20;',
        },
        {
          kind: 'sql',
          label: 'Evictions since the last reset (PostgreSQL 14 or later)',
          code: 'SELECT dealloc, stats_reset FROM pg_stat_statements_info;',
        },
      ],
    },
  ],
}
