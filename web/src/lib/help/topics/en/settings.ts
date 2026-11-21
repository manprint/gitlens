import type { HelpTopic } from '../../types'

export const settingsTopic: HelpTopic = {
  id: 'settings',
  group: 'admin',
  title: 'Settings',
  summary:
    'Everything the Settings page holds: the inventory of instances, agents and databases with their permission tiers, the browser preferences, data retention, advisor rules and observation windows, notification channels, names, the command audit and the server information.',
  page: {
    path: '/settings',
    label: 'Open Settings',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'The page title is {{ui:settings.page.title}}. Settings is where you check what pglens monitors and set how the console and the server behave. The page has seven tabs. The tab stays in the address as `tab`; the default tab, Inventory, is not written there. Tabs with views, namely Inventory, Advisor rules and Audit & server, keep the view in the address as `view`.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.tabs.inventory}}',
              meaning:
                'What pglens monitors: instances, permission tiers, agents and databases. See [{{ui:settings.tabs.inventory}}](/help/settings#inventory).',
            },
            {
              term: '{{ui:settings.tabs.preferences}}',
              meaning:
                'Settings of this browser: theme, default time range, refresh interval, table page size, time zone and language. See [{{ui:settings.tabs.preferences}}](/help/settings#preferences).',
            },
            {
              term: '{{ui:settings.tabs.retention}}',
              meaning:
                'How many days of history each metrics family keeps. See [{{ui:settings.tabs.retention}}](/help/settings#retention).',
            },
            {
              term: '{{ui:settings.tabs.advisor}}',
              meaning:
                'Which advisor rules run, and the observation windows of two checks. See [{{ui:settings.tabs.advisor}}](/help/settings#advisor-rules).',
            },
            {
              term: '{{ui:settings.tabs.notifications}}',
              meaning:
                'Where alerts are delivered: Slack, Generic webhook, Google Chat, Microsoft Teams, PagerDuty and email. See [{{ui:settings.tabs.notifications}}](/help/settings#notifications).',
            },
            {
              term: '{{ui:settings.tabs.names}}',
              meaning:
                'Readable names for clusters and instances. See [{{ui:settings.tabs.names}}](/help/settings#names).',
            },
            {
              term: '{{ui:settings.tabs.audit}}',
              meaning:
                'The command audit of each instance, and the server information. See [{{ui:settings.tabs.audit}}](/help/settings#audit-server).',
            },
          ],
        },
      ],
    },
    {
      id: 'inventory',
      title: 'Inventory',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:settings.tabs.inventory}} tab lists what pglens monitors. It has four views, selected with `view`: {{ui:settings.views.instances}} (the default), {{ui:settings.views.tiers}}, {{ui:settings.views.agents}} and {{ui:settings.views.databases}}. The inventory is derived from the instances that agents report; the server keeps no separate list of agents. The callout {{ui:settings.page.unavailableTitle}} appears when a part of the inventory cannot be loaded.',
        },
      ],
      subsections: [
        {
          id: 'instances',
          title: 'Instances',
          blocks: [
            {
              kind: 'text',
              text: 'One row per monitored PostgreSQL instance, with its {{ui:settings.inventory.colCluster}}, {{ui:settings.inventory.colRole}}, PostgreSQL version, {{ui:settings.inventory.colPermTier}}, {{ui:settings.inventory.colLastSeen}}, {{ui:settings.inventory.colState}} and {{ui:settings.inventory.colHostMetrics}}. Search matches the address, the cluster, the role, the tier and the instance ID; the Role and State filters narrow the rows further. {{ui:settings.inventory.clearFilter}} restores every row.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.inventory.colInstance}}',
                  meaning:
                    'The name of the instance, or its address when no name is set. See [{{ui:settings.tabs.names}}](/help/settings#names).',
                },
                {
                  term: '{{ui:settings.inventory.colAddress}}',
                  meaning: 'The host and port that the agent reports for the instance.',
                },
                {
                  term: '{{ui:settings.inventory.colCluster}}',
                  meaning:
                    'The cluster that owns the instance. Not assigned means that no cluster owns the instance yet.',
                },
                {
                  term: '{{ui:settings.inventory.colRole}}',
                  meaning: 'The role of the instance in its cluster.',
                },
                {
                  term: 'PostgreSQL',
                  meaning:
                    'The PostgreSQL version of the instance. The supported versions are 15 to 19, see [{{ui:settings.views.server}}](/help/settings#server-info).',
                },
                {
                  term: '{{ui:settings.inventory.colPermTier}}',
                  meaning:
                    'The tier the agent may use on this instance: T0, T1 or T2. See [{{ui:settings.views.tiers}}](/help/settings#permission-tiers).',
                },
                {
                  term: '{{ui:settings.inventory.colLastSeen}}',
                  meaning: 'When the instance last reported, in the console time zone.',
                  action:
                    'If the time is old, the agent of this instance has stopped: see [{{ui:settings.views.agents}}](/help/settings#agents).',
                },
                {
                  term: '{{ui:settings.inventory.colState}}',
                  meaning:
                    '{{ui:settings.inventory.up}}: the instance reported within the last 60 seconds. {{ui:settings.inventory.down}}: it did not.',
                },
                {
                  term: '{{ui:settings.inventory.colHostMetrics}}',
                  meaning:
                    '{{ui:settings.inventory.hostAvailable}}: the host metrics loaded. {{ui:settings.inventory.hostAvailableStale}}: they loaded, but the server flags them as stale. {{ui:settings.inventory.hostUnavailable}}: the host metrics endpoint could not be loaded.',
                  action:
                    'For {{ui:settings.inventory.hostUnavailable}}, check the agent on the host: its host metrics endpoint could not be loaded.',
                },
              ],
            },
            {
              kind: 'steps',
              items: [
                'Open the {{ui:settings.tabs.inventory}} tab, view {{ui:settings.views.instances}}.',
                'Type an address, a cluster, a role, a tier or an instance ID in the search box.',
                'Narrow the rows with the Role and State filters.',
                'Click {{ui:settings.inventory.clearFilter}} to show every row again.',
              ],
            },
          ],
        },
        {
          id: 'permission-tiers',
          title: 'Permission tiers',
          blocks: [
            {
              kind: 'text',
              text: 'A permission tier sets what the agent of an instance may do beyond reading statistics. The {{ui:settings.views.tiers}} section describes the three tiers, T0, T1 and T2, and lists the instances in each one. {{ui:settings.inventory.showList}} opens them.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: 'T0',
                  meaning: 'Reads statistics only.',
                },
                {
                  term: 'T1',
                  meaning: 'Also runs `EXPLAIN` for query plans.',
                },
                {
                  term: 'T2',
                  meaning: 'Also cancels or ends sessions.',
                  action:
                    'Keep T2 only for instances where cancelling or ending sessions is wanted.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: '`EXPLAIN ANALYZE` runs the query. It is run only when the target allows it in its own settings; the interface offers it at T2 only.',
            },
          ],
        },
        {
          id: 'agents',
          title: 'Agents',
          blocks: [
            {
              kind: 'text',
              text: 'The {{ui:settings.views.agents}} view is derived from the instance telemetry: one row for each instance, with the agent name derived from it, the instance address, the last report time and the state. The server keeps no separate list of agents. Search matches the agent, the address and the reason.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.inventory.colAgent}}',
                  meaning: 'The agent name, derived from the instance.',
                },
                {
                  term: '{{ui:settings.inventory.colInstanceAddress}}',
                  meaning: 'The address of the instance.',
                },
                {
                  term: '{{ui:settings.inventory.colLastSeen}}',
                  meaning: 'When the agent last reported, in the console time zone.',
                },
                {
                  term: '{{ui:settings.inventory.colState}}',
                  meaning:
                    '{{ui:settings.inventory.down}} when the instance has not reported within 60 seconds, or an agent-down alert is open for it. Otherwise {{ui:settings.inventory.reporting}}.',
                  action:
                    'Check that the agent runs on the host and can reach the server. The `Agent down` alert is described in [{{ui:shell.nav.alerts}}](/help/alerts).',
                },
                {
                  term: '{{ui:settings.inventory.colReason}}',
                  meaning: 'Why the state is what it is, in words.',
                },
              ],
            },
          ],
        },
        {
          id: 'databases',
          title: 'Databases by instance',
          blocks: [
            {
              kind: 'text',
              text: 'The view lists the databases that the agent of one instance reports. Choose the instance with the Databases instance picker, which appears with at least two instances; the choice stays in the address as `dbInstance`. Unmonitored databases come first, so the ones to review are at the top.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.inventory.colDatabase}}',
                  meaning: 'The name of the database.',
                },
                {
                  term: '{{ui:settings.inventory.colMonitored}}',
                  meaning:
                    '{{ui:settings.inventory.yes}}: pglens collects the metrics of the database. {{ui:settings.inventory.no}}: it does not.',
                },
                {
                  term: '{{ui:settings.inventory.colSkipReason}}',
                  meaning:
                    'Why the database is not monitored. A dash means it is monitored. `db_budget` means the agent reached its database budget: by default it keeps the 10 most active databases per instance.',
                  action:
                    'If a database you need shows `db_budget`, review the database budget in the agent configuration.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'preferences',
      title: 'Preferences',
      blocks: [
        {
          kind: 'text',
          text: 'Preferences are kept in this browser only, under the key `pglens.preferences`. Other browsers and other users keep their own. Each control has a line that says what it changes. {{ui:settings.preferences.reset}} restores every preference.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.preferences.theme}}',
              meaning:
                '{{ui:settings.preferences.theme.light}}, {{ui:settings.preferences.theme.dark}} or {{ui:settings.preferences.theme.system}}; {{ui:settings.preferences.theme.system}} follows the operating system. The theme button in the header changes the same preference. Default: {{ui:settings.preferences.theme.light}}.',
            },
            {
              term: '{{ui:settings.preferences.defaultTimeRange}}',
              meaning:
                'The range that pages open with when the address names none: {{ui:shell.range.1h}}, {{ui:shell.range.2h}}, {{ui:shell.range.4h}}, {{ui:shell.range.6h}}, {{ui:shell.range.8h}}, {{ui:shell.range.24h}}, {{ui:shell.range.3d}} or {{ui:shell.range.7d}}. A range in the address or in a link still chooses another. Default: {{ui:shell.range.1h}}.',
            },
            {
              term: '{{ui:settings.preferences.refreshInterval}}',
              meaning:
                '{{ui:settings.preferences.refreshInterval.auto}} keeps the interval of each page. {{ui:settings.preferences.refreshInterval.off}} or every 15, 30 or 60 seconds apply to the pages that reload every 15 seconds or slower, and move their stale warning with them; {{ui:settings.preferences.refreshInterval.off}} stops them and hides the warning. {{ui:shell.nav.locks}}, metric charts, {{ui:shell.nav.events}} and running commands keep their faster reload. Default: {{ui:settings.preferences.refreshInterval.auto}}.',
            },
            {
              term: '{{ui:settings.preferences.tablePageSize}}',
              meaning:
                'The rows per page of the tables when the address sets none: 20, 50 or 100. The {{ui:shell.table.rowsPerPage}} control under a table changes only that table. Default: 20.',
            },
            {
              term: '{{ui:settings.preferences.timeZone}}',
              meaning:
                'The zone every time in the console is shown in: tables, charts, tooltips, the time range and the date fields of the editors. Server default (the zone set on the server with `PGLENS_TZ`, UTC when unset), This browser (the zone of this computer) or UTC. Links, exports and the API keep UTC. Default: Server default.',
            },
            {
              term: '{{ui:settings.preferences.language}}',
              meaning:
                'The language of the console: every page, this guide, and the advisor findings and readiness texts the server writes for this browser. Server default (the language set on the server with `PGLENS_LANG`, English when unset), This browser (the first of the browser’s languages the console has, else English) or a language by name. Vacuum, WAL, checkpoint, deadlock, ctid, xmin, `pg_*`, setting names and SQL stay in English in every language. Links, exports and the API do not change. Default: Server default.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Open {{ui:shell.nav.settings}} and the {{ui:settings.tabs.preferences}} tab.',
            'Change the control. Its line says what it affects.',
            'To return every preference to its default, click {{ui:settings.preferences.reset}}.',
          ],
        },
      ],
    },
    {
      id: 'retention',
      title: 'Data retention',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:settings.tabs.retention}} tab sets how many days of history each metrics family keeps. The table, {{ui:settings.retention.tableLabel}}, has one row per family, with {{ui:settings.retention.colNow}}, a {{ui:settings.retention.colNew}} field, {{ui:settings.retention.colPreview}} and {{ui:settings.retention.colApply}}. Deletion is not immediate: the retention policy drops whole chunks older than the retention at its daily run, up to a day after Apply.',
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Shortening the retention deletes history permanently at the next policy run. Take a backup first. A shorter retention asks for confirmation before anything is deleted.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.retention.colFamily}}',
              meaning:
                'The metrics group a row controls. The families are Instance and database metrics, Statement statistics, Active session history, Replication lag and slots, Table statistics, Index statistics and Bloat estimates.',
            },
            {
              term: '{{ui:settings.retention.colNow}}',
              meaning:
                'The days the family keeps now, with a badge `default` or `custom`. The default is 30 days, and 90 days for Bloat estimates; `custom` means the value differs from it. Set outside pglens: the policy was set by something other than pglens, and the row shows it. No policy: keeps all history: nothing is deleted.',
            },
            {
              term: '{{ui:settings.retention.colNew}}',
              meaning:
                'The whole number of days you type. The server sets the accepted range, and the longest value is 365 days. Errors say why: Enter a number of days, Enter a whole number of days, or Enter between X and Y days.',
            },
            {
              term: '{{ui:settings.retention.colPreview}}',
              meaning:
                'Counts the chunks and bytes the new value would delete now, and changes nothing. The result reads `Keeps more history: nothing is deleted.`, or `Nothing older than N days is stored now.`, or `Deletes N chunks (~X MiB) at the next policy run.`',
              action:
                'Always use {{ui:settings.retention.preview}} before {{ui:settings.retention.apply}}: it shows how much history would go away.',
            },
            {
              term: '{{ui:settings.retention.colApply}}',
              meaning:
                'Saves the new retention. It is enabled once the preview matches the value typed. A longer retention applies at once; a shorter one asks for confirmation first.',
              action: 'Read the confirmation before you continue.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Open {{ui:settings.tabs.retention}} and find the row of the family.',
            'Type the new number of days in {{ui:settings.retention.colNew}}.',
            'Click {{ui:settings.retention.preview}} and read the result.',
            'Click {{ui:settings.retention.apply}}, tick the checkbox I understand that this history is deleted permanently at the next policy run, then click {{ui:settings.retention.confirmDelete}}.',
          ],
        },
      ],
    },
    {
      id: 'advisor-rules',
      title: 'Advisor rules',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:settings.tabs.advisor}} tab controls which advisor rules run and how far back two checks look. It has two views, selected with `view`: {{ui:settings.views.rules}} (the default) and {{ui:settings.views.windows}}. A rule that does not run produces no findings.',
        },
      ],
      subsections: [
        {
          id: 'rules',
          title: 'Rules',
          blocks: [
            {
              kind: 'text',
              text: 'One row per rule, with {{ui:settings.advisor.colRule}} (its id), {{ui:settings.advisor.colCategory}}, {{ui:settings.advisor.colSeverity}}, {{ui:settings.advisor.colScope}}, {{ui:settings.advisor.colMinTier}} and {{ui:settings.advisor.enabled}}. The category is the part of the rule id before the first dot. The filters {{ui:settings.advisor.colCategory}}, {{ui:settings.advisor.colSeverity}} and {{ui:settings.advisor.filterRuns}} narrow the list; {{ui:settings.advisor.filterRuns}} offers {{ui:settings.advisor.enabled}} and {{ui:settings.advisor.disabled}}. A counter reads All rules run when no rule is disabled.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.advisor.colRule}}',
                  meaning: 'The id of the rule.',
                },
                {
                  term: '{{ui:settings.advisor.colSeverity}}',
                  meaning: 'How serious the findings of the rule are, as the advisor defines it.',
                },
                {
                  term: '{{ui:settings.advisor.colScope}}',
                  meaning: 'What the rule applies to, as the advisor defines it.',
                },
                {
                  term: '{{ui:settings.advisor.colMinTier}}',
                  meaning:
                    'The lowest permission tier at which the rule runs: T0, T1 or T2. See [{{ui:settings.views.tiers}}](/help/settings#permission-tiers).',
                },
                {
                  term: '{{ui:settings.advisor.enabled}}',
                  meaning:
                    'The switch decides whether the rule runs. Off: the rule is not evaluated, and its findings are resolved at the next evaluation. On again: the rule runs again at the next evaluation.',
                  action:
                    'Switch a rule off only when its findings do not apply to this fleet. Its open findings resolve at the next evaluation.',
                },
                {
                  term: '{{ui:settings.advisor.custom}}',
                  meaning:
                    'The rule was switched from the console, so it differs from the setting of the advisor.',
                },
              ],
            },
            {
              kind: 'steps',
              items: [
                'Open {{ui:settings.tabs.advisor}}; the {{ui:settings.views.rules}} view is the default.',
                'Search for the rule, or narrow the list with {{ui:settings.advisor.colCategory}}, {{ui:settings.advisor.colSeverity}} or {{ui:settings.advisor.filterRuns}}.',
                'Switch {{ui:settings.advisor.enabled}} off. The label becomes {{ui:settings.advisor.disabled}} and the row shows {{ui:settings.advisor.custom}}.',
                'To restore the rule, switch {{ui:settings.advisor.enabled}} on again.',
              ],
            },
          ],
        },
        {
          id: 'observation-windows',
          title: 'Observation windows',
          blocks: [
            {
              kind: 'text',
              text: 'Two checks look back over history. {{ui:settings.lib.advisor.window.label.unusedIndex}} sets how long an index must go without a scan before the unused-index check reports it. {{ui:settings.lib.advisor.window.label.backupHistory}} sets how long an instance must have been monitored before the backup checks read `no base backup seen` as a finding. A short window reports sooner and can raise false positives.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.lib.advisor.window.label.unusedIndex}}',
                  meaning:
                    'How long an index must go without a scan, in the instance history or in the statistics, before the unused-index check reports it. Default: 7 days.',
                },
                {
                  term: '{{ui:settings.lib.advisor.window.label.backupHistory}}',
                  meaning:
                    'How long an instance must have been monitored before the backup checks read `no base backup seen` as a finding, not as not enough history. Default: 7 days.',
                },
                {
                  term: 'Unit',
                  meaning: 'Hours or days. The field accepts whole numbers only.',
                },
                {
                  term: 'Between 1 hour and 90 days',
                  meaning:
                    'The range shown under each field. A value outside it is refused with Enter window between 1 hour and 90 days.',
                },
                {
                  term: '{{ui:settings.advisor.saveWindows}}',
                  meaning:
                    'Saves the windows you changed. A status line then confirms the saved values.',
                },
                {
                  term: '{{ui:settings.advisor.resetDefault}}',
                  meaning: 'Deletes the stored value, so the window is 7 days again.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'warning',
              text: 'Short windows can raise false positives. A window shorter than the 7-day default shows: Shorter than the 7-day default: more findings, some of them false positives.',
            },
            {
              kind: 'steps',
              items: [
                'Open {{ui:settings.tabs.advisor}} and the {{ui:settings.views.windows}} view.',
                'Type a whole number in the field of the window, and choose hours or days.',
                'Click {{ui:settings.advisor.saveWindows}}.',
                'To return a window to 7 days, click {{ui:settings.advisor.resetDefault}}.',
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'notifications',
      title: 'Notifications',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:settings.tabs.notifications}} tab lists the channels that deliver alerts: Slack, {{ui:alerts.channel.label.webhook}}, Google Chat, Microsoft Teams, PagerDuty and {{ui:alerts.channel.label.email}}. For each channel it shows whether it is configured, the host it posts to, and who set it, {{ui:alerts.channel.source.ui}} or {{ui:alerts.channel.source.env}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Slack',
              meaning:
                'Posts alerts to a Slack webhook. Set it with `PGLENS_ALERT_SLACK_WEBHOOK_URL`, or with `PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE` for a file. The aliases `PGLENS_SLACK_WEBHOOK_URL` and `PGLENS_SLACK_WEBHOOK_URL_FILE` are read too.',
            },
            {
              term: '{{ui:alerts.channel.label.webhook}}',
              meaning: 'Posts alerts to a URL of your choice. Set it with `PGLENS_WEBHOOK_URL`.',
            },
            {
              term: 'Google Chat',
              meaning:
                'Posts alerts to a Google Chat webhook. Set it with `PGLENS_GOOGLE_CHAT_WEBHOOK_URL`, or with `PGLENS_GOOGLE_CHAT_WEBHOOK_URL_FILE` for a file.',
            },
            {
              term: 'Microsoft Teams',
              meaning:
                'Posts alerts as an Adaptive Card to a Teams workflow. In Teams, create a workflow from the template "Post to a channel when a webhook request is received" and use its URL. Set it with `PGLENS_TEAMS_WEBHOOK_URL`, or with `PGLENS_TEAMS_WEBHOOK_URL_FILE` for a file, or store it here.',
            },
            {
              term: 'PagerDuty',
              meaning:
                'Opens and resolves PagerDuty incidents through the Events API v2. The address is the integration (routing) key of the service, 32 letters or digits; the console shows only `events.pagerduty.com`. Set it with `PGLENS_PAGERDUTY_ROUTING_KEY`, or with `PGLENS_PAGERDUTY_ROUTING_KEY_FILE` for a file, or store it here.',
            },
            {
              term: '{{ui:alerts.channel.label.email}}',
              meaning:
                'Mails alerts to 1 to 20 recipients, separated by commas, stored here; the console shows only how many. The mail server is set on the server with `PGLENS_SMTP_HOST` and `PGLENS_SMTP_FROM`, plus `PGLENS_SMTP_PORT`, `PGLENS_SMTP_USERNAME`, `PGLENS_SMTP_PASSWORD` and `PGLENS_SMTP_TLS` when needed. Without `PGLENS_SMTP_HOST` the recipients cannot be stored.',
            },
            {
              term: '{{ui:settings.notifications.setBy}}',
              meaning:
                'Environment: the server environment provides the URL. Console: a URL was stored from this page, and it overrides the environment.',
            },
            {
              term: '{{ui:settings.notifications.host}}',
              meaning:
                'The host the channel posts to. The full URL is never shown again after it is stored.',
            },
            {
              term: '{{ui:settings.notifications.sendTest}}',
              meaning:
                'Posts the message `pglens test notification` through the channel that alerts use, and shows whether it was delivered within 10 seconds. The result is {{ui:alerts.channel.result.delivered}}, No answer within 10 s, or {{ui:alerts.channel.result.failed}} with the reason. Nothing is stored.',
            },
            {
              term: '{{ui:settings.notifications.setUrl}} / {{ui:settings.notifications.replaceUrl}}',
              meaning:
                'Stores a URL in the pglens database. It overrides the server environment and applies at once to the next alert and test. The field is write-only: the console never shows the URL again.',
            },
            {
              term: '{{ui:settings.notifications.removeUrl}}',
              meaning:
                'Deletes the stored URL. The channel then uses the URL of the server environment again, or stays not configured when the environment has none.',
            },
            {
              term: '{{ui:settings.notifications.language}}',
              meaning:
                'The language of the alert messages of the channel: what the alert means, the data that shows it and what to do. The server default is the language of the server (`PGLENS_LANG`). A change applies from the next alert. Vacuum, WAL, SQL and setting names stay in English, and so does the test message.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'A stored URL is kept in the pglens database without encryption, so database backups contain it. Treat those backups as secrets.',
        },
        {
          kind: 'steps',
          items: [
            'Open {{ui:settings.tabs.notifications}} and find the row of the channel.',
            'Click {{ui:settings.notifications.setUrl}}, or {{ui:settings.notifications.replaceUrl}} when a URL is already stored.',
            'Type the URL and save. It needs https://; http:// is accepted only for loopback and private addresses. It may not contain spaces, tabs, line breaks or a fragment (#), and it is at most 2048 characters long.',
            'Click {{ui:settings.notifications.sendTest}} and read the result.',
          ],
        },
        {
          kind: 'steps',
          items: [
            'Set the variable of the channel in the server environment.',
            'Restart the server.',
            'Click {{ui:settings.notifications.sendTest}} to check the delivery.',
          ],
        },
      ],
    },
    {
      id: 'names',
      title: 'Names',
      blocks: [
        {
          kind: 'text',
          text: 'Names give readable labels to clusters and instances. The names appear in titles, breadcrumbs, server pickers, filters, and in the alert and event tables, in place of identifiers and addresses. Without a name, the label falls back to the identifier or the address.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:settings.names.filterRow}}',
              meaning: 'The filter that narrows the table to clusters or to instances.',
            },
            {
              term: '{{ui:settings.names.colClusterName}}',
              meaning: 'The name shown for a cluster, in place of its identifier.',
            },
            {
              term: '{{ui:settings.names.colInstanceName}}',
              meaning: 'The name shown for an instance, in place of its address.',
            },
            {
              term: 'Cluster id / Address',
              meaning:
                'The identifier of a cluster, and the address of an instance. Rename does not change them.',
            },
            {
              term: '{{ui:settings.names.colRename}}',
              meaning:
                'Opens the rename dialog of the row, Cluster or Instance, with a name field of at most 64 characters. {{ui:settings.rename.save}} stores the name; {{ui:settings.rename.cancel}} keeps the previous one. An empty name restores the default label.',
              action: 'If a name is refused, use at most 64 characters.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Open {{ui:shell.nav.settings}} and the {{ui:settings.tabs.names}} tab.',
            'Find the row, or narrow the table with the {{ui:settings.names.filterRow}} filter.',
            'Click {{ui:settings.names.colRename}}, type the name and click {{ui:settings.rename.save}}.',
            'To remove a name, empty the field and save: the default label returns.',
          ],
        },
      ],
    },
    {
      id: 'audit-server',
      title: 'Audit & server',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:settings.tabs.audit}} tab has two views, selected with `view`: {{ui:settings.views.commands}} (the default) and {{ui:settings.views.server}}. {{ui:settings.views.commands}} records each command the server asked an agent to run, with its result. {{ui:settings.views.server}} shows the build, the API contract, the session expiry, the product limits and how the console signs in.',
        },
      ],
      subsections: [
        {
          id: 'command-audit',
          title: 'Command audit',
          blocks: [
            {
              kind: 'text',
              text: 'Each row is one event of an instance: a request, its result, or its expiry. Choose the instance with the Command audit instance picker, which appears with at least two instances; the choice stays in the address as `auditInstance`. The table shows the newest 100 entries, with times in the console time zone. The search box and the State and Command filters narrow them.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.audit.colTime}}',
                  meaning: 'When the event was recorded, in the console time zone.',
                },
                {
                  term: '{{ui:settings.audit.colCommand}}',
                  meaning: 'The command the server asked the agent to run.',
                },
                {
                  term: '{{ui:settings.audit.colArgs}}',
                  meaning: 'The arguments of the command.',
                },
                {
                  term: '{{ui:settings.audit.colRequestedBy}}',
                  meaning:
                    'The credential that asked: `api-token`, or `ui-session` with a short hash of the session. pglens has no user identity, so a person is never shown.',
                },
                {
                  term: '{{ui:settings.audit.colState}}',
                  meaning:
                    'requested, ok, error, rejected or expired. requested: the request waits for the agent. ok or error: the result the agent returned. rejected: the request was refused. expired: it ran out without a result.',
                  action:
                    'A request that stays requested, or ends expired, was not claimed by the agent. Check the agent in [{{ui:settings.views.agents}}](/help/settings#agents).',
                },
                {
                  term: '{{ui:settings.audit.colTiming}}',
                  meaning:
                    'When the request was made, when the agent claimed it and when it expires, plus the detail of the result. Expired shows when the request expired.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'When the instance has no entries, the table reads No command activity.',
            },
          ],
        },
        {
          id: 'server-info',
          title: 'Server info',
          blocks: [
            {
              kind: 'text',
              text: '{{ui:settings.views.server}} describes the release in use: its build, its API contract and its limits, followed by how the console signs in and by the reference links.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:settings.server.interfaceBuild}}',
                  meaning: 'The build of the console you are using.',
                },
                {
                  term: '{{ui:settings.server.apiContract}}',
                  meaning: 'The version of the API contract that this console was built for.',
                },
                {
                  term: '{{ui:settings.server.sessionExpiry}}',
                  meaning:
                    'When your sign-in session expires. If the server gives no expiry, the value reads The session endpoint did not provide an expiry.',
                },
                {
                  term: '{{ui:settings.limits.rawRetention.label}}',
                  meaning:
                    'Raw data is kept for 30 days by default, and 90 days for bloat estimates. Data older than its retention is deleted, not condensed: pglens keeps no summaries. The live value of each metrics family is in [{{ui:settings.tabs.retention}}](/help/settings#retention).',
                },
                {
                  term: '{{ui:settings.limits.postgresVersions.label}}',
                  meaning: 'PostgreSQL 15 through 19 are supported.',
                },
                {
                  term: '{{ui:settings.limits.replication.label}}',
                  meaning: 'Streaming replication is supported; other replication modes are not.',
                },
                {
                  term: '{{ui:settings.limits.databasesPerInstance.label}}',
                  meaning: 'At most 10 databases per instance are monitored by default.',
                },
                {
                  term: '{{ui:settings.limits.bloat.label}}',
                  meaning:
                    'Bloat, the space wasted inside tables by old row versions, is an estimate worked out from statistics. It can be wrong in either direction.',
                },
                {
                  term: '{{ui:settings.limits.activeSessionHistory.label}}',
                  meaning:
                    'pglens regularly samples what busy sessions do. It is a sample, not a complete trace.',
                },
                {
                  term: '{{ui:settings.limits.connectionPoolers.label}}',
                  meaning:
                    'A pooler such as PgBouncer shares database connections between many clients. There is no pooler view in this release.',
                },
                {
                  term: '{{ui:settings.server.authTitle}}',
                  meaning:
                    'One shared password protects the browser session. There are no user accounts, roles or per-user audit. Sessions are held in server memory and do not survive a restart. Agents authenticate with their own token. There is no enrollment queue or browser revocation control.',
                },
                {
                  term: '{{ui:settings.server.refsTitle}}',
                  meaning:
                    'Links to the API reference, {{ui:shell.nav.alertRules}}, {{ui:shell.nav.silences}} and the Product limits documentation.',
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
              term: '{{ui:settings.page.unavailableTitle}}',
              meaning: 'A part of the inventory could not be loaded.',
              action: 'Reload the page. If the callout stays, check the server.',
            },
            {
              term: '{{ui:settings.page.emptyFleetTitle}}',
              meaning: 'No agent has reported an instance yet, so the inventory is empty.',
              action:
                'Install and configure the pglens agent on a PostgreSQL host to add the first instance.',
            },
            {
              term: '{{ui:settings.inventory.emptyAgentsTitle}}',
              meaning: 'The Agents view has no monitored instance to derive an agent from.',
            },
            {
              term: '{{ui:settings.inventory.noMatchTitle}}',
              meaning: 'No row matches the search and the filters.',
              action: 'Click {{ui:settings.inventory.clearFilter}}, or change the search.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Open {{ui:settings.tabs.inventory}}, view {{ui:settings.views.instances}}. An instance that is not {{ui:settings.inventory.up}} has not reported in the last 60 seconds.',
            'Open {{ui:settings.views.agents}}: a {{ui:settings.inventory.down}} state gives the reason in the {{ui:settings.inventory.colReason}} column.',
            'Check {{ui:settings.inventory.colHostMetrics}}: {{ui:settings.inventory.hostUnavailable}} means the host metrics endpoint could not be loaded.',
            'Open {{ui:settings.views.databases}}: an unmonitored database shows its reason in the {{ui:settings.inventory.colSkipReason}} column.',
          ],
        },
      ],
    },
  ],
}
