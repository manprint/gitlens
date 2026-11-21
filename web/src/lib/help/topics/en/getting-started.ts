import type { HelpTopic } from '../../types'

export const gettingStartedTopic: HelpTopic = {
  id: 'getting-started',
  group: 'start',
  title: 'Getting started',
  summary:
    'What pglens is, how the console is organised, and how to read any page: the sidebar, the header, the time range, the freshness badge, search, tables, sign-in and this guide.',
  page: {
    path: '/',
    label: 'Open Fleet',
  },
  sections: [
    {
      id: 'what-pglens-is',
      title: 'What pglens is',
      blocks: [
        {
          kind: 'text',
          text: 'pglens monitors PostgreSQL. An agent runs on each PostgreSQL host, reads its statistics on a schedule and sends snapshots to the pglens server. The server stores the snapshots, derives the findings and alerts from them and serves this console. Most pages show those stored snapshots. The server can also ask an agent to run a command, such as a query plan or an action on a session, but only on instances whose permission tier allows it, and every request is recorded.',
        },
        {
          kind: 'text',
          text: 'The hierarchy is server, cluster, instance, database. One pglens server holds many clusters, a cluster holds one or more instances, and an instance holds its databases. The {{ui:settings.tabs.names}} tab of {{ui:shell.nav.settings}} gives readable names to clusters and instances.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Agent',
              meaning:
                'The collector on a PostgreSQL host. Its name is derived from the instance it reports; the server keeps no separate list of agents. Its state, {{ui:settings.inventory.reporting}} or Down, is shown in {{ui:shell.nav.settings}}, tab {{ui:settings.tabs.inventory}}, view {{ui:settings.views.agents}}.',
              action:
                'If the state is Down, check that the agent process runs on the host and can reach the server.',
            },
            {
              term: 'Server',
              meaning:
                'The pglens server. It stores the snapshots in TimescaleDB, computes findings and alerts, and serves the console. Sign-in is to this server.',
            },
            {
              term: 'Cluster',
              meaning:
                'A group of instances shown together, for example a primary and its replicas. Each card on {{ui:shell.nav.fleet}} is a cluster. An instance that no cluster owns yet is shown as {{ui:settings.inventory.notAssigned}}.',
            },
            {
              term: 'Instance',
              meaning:
                'One PostgreSQL server reported by an agent, identified by its address and port. The Instance pages show one instance at a time. Where a name is set, it replaces the address: see [{{ui:shell.nav.settings}}](/help/settings#names).',
            },
            {
              term: 'Database',
              meaning:
                'A database inside an instance. By default the agent monitors at most 10 databases per instance; the others are listed with a skip reason: see [{{ui:shell.nav.settings}}](/help/settings#databases).',
            },
          ],
        },
      ],
    },
    {
      id: 'sidebar',
      title: 'Sidebar',
      blocks: [
        {
          kind: 'text',
          text: 'The sidebar lists the pages in the groups {{ui:shell.nav.monitoring}}, Instance, {{ui:shell.nav.alerting}} and {{ui:shell.nav.inlineHelp}}, with {{ui:shell.nav.settings}} at the bottom. The Instance group appears once a server is chosen. {{ui:shell.nav.slowestQueries}} and {{ui:shell.nav.indexAnalysis}} name their server in the address, with the `instance` parameter. On narrow screens the sidebar is hidden: the {{ui:shell.header.openNavigation}} button in the header opens it.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:shell.nav.chooseServer}}',
              meaning:
                'Picks the PostgreSQL instance that the Instance pages show. The last instance you chose is remembered in this browser.',
            },
            {
              term: '{{ui:shell.nav.fleet}}',
              meaning:
                'Home page (`/`): one card per cluster, and a summary bar that filters the cards by health: {{ui:fleet.summary.all}}, {{ui:fleet.summary.healthy}}, {{ui:fleet.summary.degraded}}, {{ui:fleet.summary.critical}}. Details in [{{ui:shell.nav.fleet}}](/help/fleet).',
            },
            {
              term: '{{ui:shell.nav.slowestQueries}}',
              meaning:
                'The 20 queries with the highest execution time on the chosen server, with plans and history. Details in [{{ui:shell.nav.slowestQueries}}](/help/slowest-queries).',
            },
            {
              term: '{{ui:shell.nav.indexAnalysis}}',
              meaning:
                'Missing, unused, duplicate and invalid indexes, with the psql statements to review and run yourself. Details in [{{ui:shell.nav.indexAnalysis}}](/help/indexes).',
            },
            {
              term: '{{ui:shell.nav.findings}}',
              meaning:
                'Problems the advisor detected, one row per finding. Details in [{{ui:shell.nav.findings}}](/help/findings).',
            },
            {
              term: '{{ui:shell.nav.instanceOverview}}',
              meaning:
                'Summary of the chosen instance. Details in [{{ui:shell.nav.instanceOverview}}](/help/instance-overview).',
            },
            {
              term: '{{ui:shell.nav.queryPerformance}}',
              meaning:
                'Query statistics of the chosen instance. Details in [{{ui:shell.nav.queryPerformance}}](/help/query-performance).',
            },
            {
              term: '{{ui:shell.nav.waitEvents}}',
              meaning:
                'What the sessions of the chosen instance are waiting for, as it happens. Details in [{{ui:shell.nav.waitEvents}}](/help/wait-events).',
            },
            {
              term: '{{ui:shell.nav.locks}}',
              meaning:
                'Who blocks whom, and the sessions active now. Details in [{{ui:shell.nav.locks}}](/help/locks).',
            },
            {
              term: '{{ui:shell.nav.alerts}}',
              meaning:
                '{{ui:shell.nav.alerts}} raised by the alert rules. Details in [{{ui:shell.nav.alerts}}](/help/alerts).',
            },
            {
              term: '{{ui:shell.nav.events}}',
              meaning: 'The history of events. Details in [{{ui:shell.nav.events}}](/help/events).',
            },
            {
              term: '{{ui:shell.nav.alertRules}}',
              meaning:
                'The rules that raise alerts, with their conditions. Details in [{{ui:shell.nav.alertRules}}](/help/alert-rules).',
            },
            {
              term: '{{ui:shell.nav.silences}}',
              meaning:
                '{{ui:shell.nav.silences}} mute matching alerts. Details in [{{ui:shell.nav.silences}}](/help/silences).',
            },
            {
              term: 'User guide',
              meaning: 'Opens this guide, with the chapters of every page.',
            },
            {
              term: '{{ui:shell.nav.settings}}',
              meaning:
                '{{ui:settings.tabs.inventory}}, preferences, data retention, advisor rules, notifications, names, command audit and server information. Details in [{{ui:shell.nav.settings}}](/help/settings).',
            },
          ],
        },
      ],
    },
    {
      id: 'header',
      title: 'Header',
      blocks: [
        {
          kind: 'text',
          text: 'The header is above every page. It holds the time range picker, the theme button, the connection indicator, the keyboard shortcuts and {{ui:shell.header.signOut}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.preferences.theme}}',
              meaning:
                'Cycles the colours of the console between {{ui:settings.preferences.theme.light}}, {{ui:settings.preferences.theme.dark}} and {{ui:settings.preferences.theme.system}}. The same setting is in [{{ui:shell.nav.settings}}](/help/settings#preferences).',
            },
            {
              term: 'Connection indicator',
              meaning:
                'Shows whether the browser reaches the server: reachable, not reachable, or browser is offline. It also carries the time of the last successful response, or No successful response yet.',
              action:
                'If it says not reachable, check that the server runs and that this browser can reach it.',
            },
            {
              term: '{{ui:shell.header.shortcuts}}',
              meaning:
                'Opens the list of shortcuts, also with `?`. The sequences are `g` then `f` for {{ui:shell.nav.fleet}}, `g` then `q` for {{ui:shell.nav.slowestQueries}}, `g` then `a` for {{ui:shell.nav.alerts}} and `g` then `s` for {{ui:shell.nav.settings}}. `/` focuses the page filter.',
            },
            {
              term: '{{ui:shell.header.signOut}}',
              meaning:
                'Ends the session in this browser. See [Sign in and sessions](/help/getting-started#sign-in).',
            },
          ],
        },
      ],
    },
    {
      id: 'time-range',
      title: 'Time range',
      blocks: [
        {
          kind: 'text',
          text: 'The time range picker in the header sets the window that charts and tables read. The window stays in the address as `range` (a preset such as `1h`), or as `from` and `to` (an absolute range, always in UTC), so a shared link opens the same window. Every time on screen is in the console time zone, set in [{{ui:shell.nav.settings}}](/help/settings#preferences). A range in the address wins over the default set in [{{ui:shell.nav.settings}}](/help/settings#preferences).',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:shell.range.preset}}',
              meaning:
                'The select with the presets: {{ui:shell.range.15m}}, {{ui:shell.range.1h}} (the default), {{ui:shell.range.2h}}, {{ui:shell.range.4h}}, {{ui:shell.range.6h}}, {{ui:shell.range.8h}}, {{ui:shell.range.24h}}, {{ui:shell.range.3d}}, {{ui:shell.range.7d}}, {{ui:shell.range.14d}} and {{ui:shell.range.30d}}. The list ends at 30 days.',
            },
            {
              term: 'Custom time range',
              meaning:
                'Opens the absolute form, titled with the console time zone (Custom time range (Europe/Rome), for example), with From and To in that zone, applied with {{ui:shell.range.apply}}. The end must be after the start, and the range cannot be longer than 30 days.',
            },
            {
              term: '{{ui:shell.range.zoomOutTitle}}',
              meaning:
                'For a preset range, moves to the next longer preset, up to {{ui:shell.range.30d}}.',
            },
          ],
        },
      ],
    },
    {
      id: 'freshness',
      title: 'Freshness badge',
      blocks: [
        {
          kind: 'text',
          text: 'The page header shows how old the data of the page is. The threshold that marks data as stale depends on the page. Pages reload on their own interval, unless {{ui:settings.preferences.refreshInterval}} in [{{ui:shell.nav.settings}}](/help/settings#preferences) changes it.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Updated <age> ago',
              meaning:
                'How long ago the page last received data from the server. A small age is what you expect while the agents report.',
            },
            {
              term: '{{ui:shell.fresh.noData}}',
              meaning:
                'The page has received no data yet: no snapshot has reached the server for it.',
              action:
                'Check the agents in [{{ui:shell.nav.settings}}](/help/settings#agents), then the instance state in [{{ui:shell.nav.settings}}](/help/settings#instances).',
            },
            {
              term: 'Stale',
              meaning:
                'The data is older than the threshold of the page. The warning gives the age and the threshold, as in `Stale — <age> old (threshold <t>)`.',
              action: 'Check the agent that feeds the page and the server, then reload the page.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'A {{ui:settings.preferences.refreshInterval}} of Off stops the reload of the pages that reload every 15 seconds or slower, and hides their stale warning. Locks, metric charts, events and running commands keep their faster reload.',
        },
      ],
    },
    {
      id: 'search',
      title: 'Search and filters',
      blocks: [
        {
          kind: 'text',
          text: 'Search and filters above a table narrow its rows; they change no data. {{ui:shell.nav.indexAnalysis}} also has {{ui:indexes.search.box}}, which looks for indexes, tables, columns and queries of the server. It is on the {{ui:shell.nav.indexAnalysis}} page, not in the header.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:indexes.search.box}}',
              meaning:
                'Finds the indexes, tables, columns and queries that match every word typed. Results are grouped as Indexes, Tables, Columns and Queries. {{ui:indexes.search.noMatch}} means nothing matches; {{ui:indexes.search.clear}} empties the box.',
            },
            {
              term: '{{ui:settings.inventory.clearFilter}}',
              meaning:
                'In the {{ui:settings.tabs.inventory}} tables of {{ui:shell.nav.settings}}, removes the search and the filters and shows every row again.',
            },
          ],
        },
      ],
    },
    {
      id: 'tables',
      title: 'Tables',
      blocks: [
        {
          kind: 'text',
          text: 'Tables share the same controls: sorting, paging, expandable rows, a column menu and column hints. The footer reads `Showing a–b of N` with the name of the rows, for example missing indexes.',
        },
      ],
      subsections: [
        {
          id: 'sorting',
          title: 'Sorting',
          blocks: [
            {
              kind: 'text',
              text: 'Each sortable column header has two buttons, ascending and descending. Press the active one again to clear the sort. A table sorts by one column at a time.',
            },
          ],
        },
        {
          id: 'paging',
          title: 'Paging and page size',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Rows per page',
                  meaning:
                    'Either 20, 50 or 100. The default comes from [{{ui:shell.nav.settings}}](/help/settings#preferences), Table page size; the control under a table changes that table only.',
                },
                {
                  term: 'First page, Previous page, Next page, Last page',
                  meaning: 'Move through the pages of the table.',
                },
              ],
            },
          ],
        },
        {
          id: 'expandable-rows',
          title: 'Expandable rows',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Show details',
                  meaning:
                    'Opens the details of a row under it; Hide details closes them. An open row stays open when the table is sorted again. Changing the page closes every open row.',
                },
              ],
            },
          ],
        },
        {
          id: 'columns-and-hints',
          title: 'Columns and hints',
          blocks: [
            {
              kind: 'terms',
              items: [
                {
                  term: 'Columns',
                  meaning: 'The menu that shows or hides the columns of the table.',
                },
                {
                  term: 'Info button',
                  meaning:
                    'Opens the caption of a value or column: what it is and, where they apply, its Source, Unit, Window, How to read and Thresholds. A click or tap keeps it open; Escape closes it.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'breadcrumbs',
      title: 'Breadcrumbs',
      blocks: [
        {
          kind: 'text',
          text: 'The trail above the page title shows where you are. {{ui:settings.tabs.names}} set in [{{ui:shell.nav.settings}}](/help/settings#names) replace identifiers in it, and help pages show the title of their chapter.',
        },
      ],
    },
    {
      id: 'sign-in',
      title: 'Sign in and sessions',
      blocks: [
        {
          kind: 'text',
          text: 'The console asks for one shared password, on the page Sign in to pglens. The password is set on the server with `PGLENS_UI_PASSWORD`. There are no user accounts: whoever knows the password has the same access.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Password',
              meaning:
                'The shared password. Too many attempts block sign-in for a few minutes: the page asks you to wait and try again. A wrong password shows Invalid password.',
              action:
                'If sign-in keeps failing, check the password set with `PGLENS_UI_PASSWORD` on the server.',
            },
            {
              term: 'Session',
              meaning:
                'The session stays active in this browser until you sign out. Sessions are kept in server memory. The session expiry is shown in [{{ui:shell.nav.settings}}](/help/settings#server-info).',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Sessions live in server memory, so a restart of the server signs out every user. After a restart, sign in again.',
        },
      ],
    },
    {
      id: 'using-this-guide',
      title: 'Using this {{ui:shell.nav.inlineHelp}}',
      blocks: [
        {
          kind: 'text',
          text: 'This guide is the {{ui:shell.nav.inlineHelp}} of the console. Each page has a chapter, grouped in the menu as Getting started, {{ui:shell.nav.monitoring}}, Instance pages, {{ui:shell.nav.alerting}} and Administration. Every chapter has an overview, one section for each visible area of its page, and a troubleshooting section where it helps. Pages link to their chapter from the page header.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Search the guide',
              meaning:
                'Filters the sections of all chapters by the words typed. In Italian it also matches the English labels that the console shows.',
            },
            {
              term: 'On this page',
              meaning: 'Lists the sections of the open chapter, in order.',
            },
            {
              term: 'Link to this section',
              meaning:
                'The # icon beside a heading links to that section; its address can be shared.',
            },
            {
              term: 'Help language',
              meaning:
                'The English / Italiano switch. The text of the chapters changes language, while the labels of the console stay in English.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Open the chapter of the page from its header link, or from the menu of this guide.',
            'Read the summary of the chapter, then the section of the area you are looking at.',
            'Check each term: its meaning, unit, thresholds and action.',
            'Use Search the guide for a word that is not in the menu.',
          ],
        },
      ],
    },
    {
      id: 'troubleshooting',
      title: 'Troubleshooting',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Read the freshness badge of the page: [{{ui:shell.fresh.noData}}](/help/getting-started#freshness) or [Stale](/help/getting-started#freshness) tells you the data is missing or old.',
            'Check the time range: a short range can hold no samples. See [Time range](/help/getting-started#time-range).',
            'Open [{{ui:shell.nav.settings}}](/help/settings#agents), {{ui:settings.tabs.inventory}}, {{ui:settings.views.agents}}: a Down state means no report from the agent within the expected interval. The Reason column says why.',
            'Open [{{ui:shell.nav.settings}}](/help/settings#instances), {{ui:settings.tabs.inventory}}, Instances: an instance that is not Up has not reported in the last 60 seconds.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Partial · N warnings',
              meaning:
                'The readiness of an instance is partial: some data it could collect is missing. Full means nothing is missing. The banner Some features have less data than they could. See what is missing and how to fix it. appears where a page has such instances.',
              action:
                'Open the readiness section of the instance. Each warning has a fix with its snippet and a Copy button. A fix marked Requires restart takes effect only after a restart.',
            },
          ],
        },
      ],
    },
  ],
}
