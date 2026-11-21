import type { HelpTopic } from '../../types'

export const waitEventsTopic: HelpTopic = {
  id: 'wait-events',
  group: 'instance',
  title: 'Wait events',
  summary:
    '{{ui:shell.nav.waitEvents}} shows what the busy server processes are doing and waiting for, from the Active Session History ({{ui:shell.crumb.ash}}) sampled by the agent. Use it to see whether time goes to {{ui:ash.waitGroup.cpu}}, {{ui:ash.waitGroup.io}}, locks, the client or other waits.',
  page: {
    path: '/instances/:instanceId/ash',
    label: 'Open {{ui:shell.nav.waitEvents}}',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:shell.nav.waitEvents}} answers one question: where do the busy processes of the server spend their time, and what are they waiting for? Each client connection is served by one server process, called a backend. The agent samples the backends once per second, stores the counts per 10-second window, and the page shows the average number of backends that were not idle. The sampling is statistical: it shows where time goes, not exact durations.',
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
                'Open the tab {{ui:shell.nav.waitEvents}} in the instance header, or the entry {{ui:shell.nav.waitEvents}} in the sidebar group {{ui:alerts.column.instance}}.',
                'Choose the time range with the picker labelled Time range preset. The charts cover that range in buckets of 10 seconds or more, at most 180 per chart.',
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
              text: 'The controls that change the page are the time range, the chart layout, the drill path and the URL parameter `db`. The sidebar and the instance tabs carry `db` to the other views of this server, and drop the other URL parameters of this page.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'Time range preset',
                  meaning:
                    'Sets the range that every chart and the {{ui:ash.top.title}} list read.',
                  action:
                    'Widen the range when a chart shows gaps or the sampling warning. See [Troubleshooting](/help/wait-events#troubleshooting).',
                },
                {
                  term: '{{ui:ash.layout.label}}',
                  meaning:
                    'Switches between one chart per wait group and one stacked chart. See [{{ui:ash.layout.label}}](/help/wait-events#chart-layout).',
                },
                {
                  term: 'Drill path',
                  meaning:
                    'The breadcrumb {{ui:ash.drill.label}} shows the level you are on and lets you go back. See [Drill path](/help/wait-events#drill-path).',
                },
                {
                  term: 'db',
                  meaning: 'URL parameter that limits the samples to one database.',
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
              text: 'The agent reads `pg_stat_activity` once per second. It counts the client and parallel worker backends whose state is not idle, leaving out its own pglens sessions. A backend with no wait event is counted as {{ui:ash.waitGroup.cpu}}. The agent stores the counts per 10-second window, and at most 100 wait keys are retained per window: the rest is folded into other, so the total is kept.',
            },
            {
              kind: 'note',
              tone: 'info',
              text: '{{ui:shell.crumb.ash}} uses 1-second statistical sampling, so queries shorter than one second are under-represented. The charts plot the largest groups of the range and fold the rest into other too.',
            },
          ],
        },
      ],
    },
    {
      id: 'drill-path',
      title: 'Drill path',
      blocks: [
        {
          kind: 'text',
          text: 'The breadcrumb labelled {{ui:ash.drill.label}} shows the level you are on: {{ui:ash.group.waitEventType}}, then {{ui:shell.nav.waitEvents}}, then {{ui:shell.crumb.queries}}. Each level narrows the previous one. Each crumb that is not the current level is a button: it returns to that level and clears the filters below it. The series named other does not open a level.',
        },
        {
          kind: 'steps',
          items: [
            'Start at {{ui:ash.group.waitEventType}} and find the kind of wait that dominates.',
            'Select the series of that kind. The page opens {{ui:shell.nav.waitEvents}} for that type.',
            'Select the wait event. The page opens {{ui:shell.crumb.queries}} for that event.',
            'Read the {{ui:ash.top.title}} list below the {{ui:shell.crumb.queries}} chart.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.group.waitEventType}}',
              meaning:
                'Top level: one series per wait event type, for example {{ui:ash.waitGroup.cpu}}, IO or Lock.',
              action: 'Start here to see which kind of wait takes most of the time.',
            },
            {
              term: '{{ui:shell.nav.waitEvents}}',
              meaning:
                'Second level: one series per wait event inside the selected type. The crumb reads {{ui:shell.nav.waitEvents}}: followed by the selected type, or all.',
              action:
                'Note the event name, then select it to see the queries sampled while waiting in it.',
            },
            {
              term: '{{ui:shell.crumb.queries}}',
              meaning:
                'Third level, reached by selecting a wait event: one series per query id, drawn as one stacked chart, with the {{ui:ash.top.title}} list below.',
              action:
                'Open a query from {{ui:ash.top.title}} to see its statement and plans in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#query-detail).',
            },
          ],
        },
      ],
    },
    {
      id: 'chart-layout',
      title: '{{ui:ash.layout.label}}',
      blocks: [
        {
          kind: 'text',
          text: 'The toggle labelled {{ui:ash.layout.label}}, in the page header, switches between two views of the same data. {{ui:ash.layout.separate}} draws one chart per wait group, each on its own scale, so a small group is not flattened by a large one. {{ui:ash.layout.stacked}} draws one chart with all series stacked, so the height is the average number of non-idle backends. {{ui:ash.layout.separate}} is the default and is not written to the URL; {{ui:ash.layout.stacked}} is kept as `view=stacked`. Queries always use the stacked chart.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.layout.separate}}',
              meaning:
                'Default layout: one card per wait group, each with its own scale, legend and subtitle.',
              action:
                'Use it to find the group where a series grows, and to compare series inside one group.',
            },
            {
              term: '{{ui:ash.layout.stacked}}',
              meaning:
                'One chart: each band is one series, and the total height is the average number of non-idle backends. All groups share one scale, so a large band can flatten a small one.',
              action:
                'Use it to see the total load in one view, then switch to {{ui:ash.layout.separate}} to read the small groups.',
            },
          ],
        },
      ],
    },
    {
      id: 'wait-group-cards',
      title: 'Wait group cards',
      blocks: [
        {
          kind: 'text',
          text: 'In the {{ui:ash.layout.separate}} layout, each wait group is a card. The title is the group name, and the subtitle gives the average number of active backends in the card and its share of all waits. The card has its own scale, chart and legend. The groups are: {{ui:ash.waitGroup.cpu}}, {{ui:ash.waitGroup.io}}, {{ui:ash.waitGroup.locks}}, {{ui:ash.waitGroup.client}}, {{ui:ash.waitGroup.other}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.waitGroup.cpu}}',
              meaning:
                'Backends that report no wait event, counted as running on {{ui:ash.waitGroup.cpu}}. Series type: {{ui:ash.waitGroup.cpu}}. A high value means the server is busy computing, not waiting.',
              action:
                'Check the statements that use most time in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#query-inspector).',
            },
            {
              term: '{{ui:ash.waitGroup.io}}',
              meaning: 'Backends waiting for a read or write to storage. Series type: IO.',
              action:
                'Check the statements with a low {{ui:queries.overview.cacheHitRatio}} in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#statement-statistics), then the indexes of the tables they read in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
            },
            {
              term: '{{ui:ash.waitGroup.locks}}',
              meaning:
                'Backends waiting for a heavyweight lock (Lock), an internal lightweight lock (LWLock), or a buffer (BufferPin, Buffer). Series types: Lock, LWLock, BufferPin, Buffer.',
              action:
                'Open [{{ui:shell.nav.locks}}](/help/locks) to find the {{ui:locks.blocking.blockingSession}}.',
            },
            {
              term: '{{ui:ash.waitGroup.client}}',
              meaning:
                'Backends waiting for the client application to send or receive data ({{ui:locks.sessions.client}}), or for another server process such as a parallel worker or replication (IPC). Series types: {{ui:locks.sessions.client}}, IPC.',
              action:
                'For {{ui:locks.sessions.client}}, check the application: it may be slow to send requests or to read results. For IPC, check the parallel workers and replication.',
            },
            {
              term: '{{ui:ash.waitGroup.other}}',
              meaning:
                'Timer, background-activity, extension and other waits: series types Timeout, {{ui:locks.activity.title}}, Extension, and any type not classified above.',
              action:
                'A type the page does not know goes to this group, and its legend reads {{ui:locks.detail.waitEvent}} type followed by the type name.',
            },
          ],
        },
      ],
    },
    {
      id: 'legend',
      title: 'Legend',
      blocks: [
        {
          kind: 'text',
          text: 'Each card has a legend. A row shows the series name, a colour swatch and the average number of active backends of that series. Click a row to show that series alone; the row stays marked as pressed while it is isolated.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.chart.axis}}',
              meaning:
                'Average number of non-idle backends of the series, computed as samples divided by the sampling ticks of the bucket.',
            },
            {
              term: 'Legend row',
              meaning:
                'Click to show only that series. The row is marked as pressed while it is isolated.',
              action:
                'Isolate the series that grows to read its exact shape, then click it again to see all series.',
            },
          ],
        },
      ],
    },
    {
      id: 'wait-types',
      title: '{{ui:ash.group.waitEventType}} reference',
      blocks: [
        {
          kind: 'text',
          text: 'The descriptions below are the ones the page uses for each wait event type.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:ash.waitGroup.cpu}}',
              meaning: 'Backend running on {{ui:ash.waitGroup.cpu}}, with no wait event reported.',
            },
            {
              term: 'IO',
              meaning: 'Waiting for a read or write to storage.',
            },
            {
              term: 'Lock',
              meaning: 'Waiting for a heavyweight lock held by another transaction.',
            },
            {
              term: 'LWLock',
              meaning:
                'Waiting for an internal lightweight lock (buffer, WAL, catalog contention).',
            },
            {
              term: 'BufferPin',
              meaning: 'Waiting for exclusive access to a data buffer.',
            },
            {
              term: 'Buffer',
              meaning: 'Waiting for exclusive access to a data buffer.',
            },
            {
              term: '{{ui:locks.sessions.client}}',
              meaning: 'Waiting for the client application to send or receive data.',
            },
            {
              term: 'IPC',
              meaning: 'Waiting for another server process (parallel workers, replication).',
            },
            {
              term: 'Timeout',
              meaning: 'Sleeping until a timer fires.',
            },
            {
              term: '{{ui:locks.activity.title}}',
              meaning: 'Background process idle in its main loop.',
            },
            {
              term: 'Extension',
              meaning: 'Waiting inside an extension.',
            },
            {
              term: 'other',
              meaning: '{{ui:shell.nav.waitEvents}} not classified above.',
            },
          ],
        },
      ],
    },
    {
      id: 'queries',
      title: '{{ui:shell.crumb.queries}}',
      blocks: [
        {
          kind: 'text',
          text: 'At the {{ui:shell.crumb.queries}} level each series is a query id, drawn as one stacked chart whatever the layout. The page shows this note: {{ui:shell.crumb.queries}} are plotted in one stacked chart: each series is a query, not a wait group. The query id comes from the `query_id` column of `pg_stat_activity`, which is filled only when `compute_query_id` is `on`, or when it is `auto` and a module such as `pg_stat_statements` is loaded.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:findings.evidence.queryid}}',
              meaning:
                'One series per query id, the same id that {{ui:shell.nav.queryPerformance}} shows. The smallest queries are folded into other.',
              action:
                'Open the query from [{{ui:ash.top.title}}](/help/wait-events#top-queries) to see its statement and plans.',
            },
          ],
        },
      ],
    },
    {
      id: 'top-queries',
      title: '{{ui:ash.top.title}}',
      blocks: [
        {
          kind: 'text',
          text: 'Below the {{ui:shell.crumb.queries}} chart, {{ui:ash.top.title}} lists the queries sampled most often in the chosen wait scope. Each entry is titled {{ui:locks.sessions.query}} with its query id and links to the query page; it shows the average number of active backends, the sample count and the normalised query text. The entries are ranked by samples, and the list keeps the top 20. {{ui:shell.crumb.ash}} never reads live query text: the text is the normalised text from `pg_stat_statements`, and literal values are not available.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Ranked by samples',
              meaning:
                'The scope sentence above the list names the wait event type or event the ranking is limited to, or says it covers every wait event.',
            },
            {
              term: '{{ui:ash.chart.axis}}',
              meaning:
                'Average backends for the query: samples divided by the sampling ticks of the whole range. It reads Unknown average active backends when it is missing.',
              action:
                'Compare it with Mean time and Calls of the statement in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#statement-statistics) before tuning.',
            },
            {
              term: 'samples across ticks',
              meaning:
                'Number of samples taken for the query, and the sampling ticks of the range.',
            },
            {
              term: 'No top queries',
              meaning: 'No query ids were sampled in the selected time range.',
              action:
                'Widen the range, then see the query id notice in [Troubleshooting](/help/wait-events#troubleshooting).',
            },
          ],
        },
      ],
    },
    {
      id: 'url-parameters',
      title: 'URL parameters',
      blocks: [
        {
          kind: 'text',
          text: 'The page state is kept in the address, so a view can be shared. The stacked layout is written as `view=stacked`; {{ui:ash.layout.separate}} is the default and is not written.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'group',
              meaning:
                'Level of the drill: `wait_event_type` when absent, or `wait_event`, or `queryid`.',
            },
            {
              term: 'wait_event_type',
              meaning:
                'The wait event type selected at the {{ui:shell.nav.waitEvents}} level. Absent means all types.',
            },
            {
              term: 'wait_event',
              meaning:
                'The wait event selected to reach the {{ui:shell.crumb.queries}} level. It restricts the {{ui:shell.crumb.queries}} chart and {{ui:ash.top.title}} to that event.',
            },
            {
              term: 'db',
              meaning: 'Limits the samples to one database.',
            },
            {
              term: 'view',
              meaning:
                '`stacked` selects the stacked layout. Absent means {{ui:ash.layout.separate}}.',
            },
          ],
        },
      ],
    },
    {
      id: 'reading-together',
      title: 'Reading the groups together',
      blocks: [
        {
          kind: 'text',
          text: 'A single group rarely explains a slow server. Read the groups together, then follow the matching page.',
        },
        {
          kind: 'steps',
          items: [
            'If {{ui:ash.waitGroup.locks}} grows, open [{{ui:shell.nav.locks}}](/help/locks) and find the {{ui:locks.blocking.blockingSession}}.',
            'If {{ui:ash.waitGroup.io}} grows, check the statements with a low {{ui:queries.overview.cacheHitRatio}} in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#statement-statistics).',
            'If {{ui:ash.waitGroup.cpu}} grows with no waits, read the plans of the top statements in [{{ui:shell.nav.queryPerformance}}](/help/query-performance#execution-plan).',
            'If {{ui:locks.sessions.client}} grows, check the application that sends requests or reads results slowly.',
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
              term: '{{ui:shell.crumb.ash}} sampling disabled.',
              meaning:
                'The agent has {{ui:shell.crumb.ash}} sampling off, so the page has no samples. The notice reads: The configuration key checks.ash.enabled in the agent configuration file is off.',
              action:
                'Set `checks.ash.enabled` to true in the agent configuration file. Keeping {{ui:shell.crumb.ash}} disabled avoids its sampling cost but removes wait-analysis visibility.',
            },
            {
              term: '{{ui:shell.crumb.ash}} sampling warning:',
              meaning:
                'The server warning for the range: it appears when the range holds fewer than 60 sampling ticks, one per second the agent sampled. It is followed by the number of backend samples collected, one per non-idle backend per tick. The page states that the conclusions are unreliable.',
            },
            {
              term: 'No non-null query_id was observed in this {{ui:shell.crumb.ash}} range',
              meaning:
                'The {{ui:shell.crumb.queries}} level has no query ids. The notice gives the reason and Required: `compute_query_id=on`. When `compute_query_id` is off, the notice links to the Readiness tab of the instance, which lists the item and its fix.',
              action:
                'Set `compute_query_id` to on, or to auto with a module such as `pg_stat_statements` loaded.',
            },
            {
              term: 'No wait event types observed',
              meaning: 'Shown when the selected level has no series for the range.',
              action:
                'Widen the range. If it stays empty, check the {{ui:shell.crumb.ash}} sampling notices above.',
            },
            {
              term: 'No wait events observed',
              meaning:
                'The same notice at the {{ui:shell.nav.waitEvents}} level, when the selected type has no series for the range.',
            },
            {
              term: 'Gap',
              meaning: 'A bucket with no stored sample.',
              action: 'A gap is an absence of samples, so check that the agent ran in that period.',
            },
          ],
        },
        {
          kind: 'sql',
          label: 'Waits right now, from pg_stat_activity',
          code: "SELECT wait_event_type, wait_event, state, count(*) FROM pg_stat_activity WHERE state <> 'idle' GROUP BY wait_event_type, wait_event, state ORDER BY count(*) DESC;",
        },
        {
          kind: 'sql',
          label: 'Query id setting (PostgreSQL 14 or later)',
          code: 'SHOW compute_query_id;',
        },
      ],
    },
  ],
}
