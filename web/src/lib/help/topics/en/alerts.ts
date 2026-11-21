import type { HelpTopic } from '../../types'

export const alertsTopic: HelpTopic = {
  id: 'alerts',
  group: 'alerting',
  title: 'Alerts',
  summary:
    'The alerts raised by the alert rules across the fleet: what is firing now, what resolved, what is silenced, and what to do with each one.',
  page: {
    path: '/alerts',
    label: 'Open Alerts',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'An alert is one rule that holds for one cluster or one instance. The server evaluates the rules every 30 seconds by default, using the metrics and events the agents send, and keeps the result. An alert is firing while its condition holds, and resolved once it no longer holds. The page lists them, so you see what needs attention now, what is silenced, and what already cleared.',
        },
        {
          kind: 'steps',
          items: [
            'Open the page from the {{ui:shell.nav.alerting}} group of the sidebar. It has two tabs: {{ui:alerts.section.alerts}} and {{ui:alerts.section.delivery}}.',
            'Start with {{ui:alerts.column.state}} set to {{ui:alerts.filter.firing}} and with the summary tiles: those are the alerts that need action now.',
            'Select a row to open Alert detail, then follow its rule in [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table).',
            'Silence an alert only to stop noise you know and accept: see [{{ui:shell.nav.silences}}](/help/silences#create).',
          ],
        },
      ],
      subsections: [
        {
          id: 'page-controls',
          title: 'Page controls',
          blocks: [
            {
              kind: 'text',
              text: 'The filters sit above the list. Each one is kept in the address, so a filtered view can be reloaded or shared.',
            },
            {
              kind: 'terms',
              items: [
                {
                  term: '{{ui:alerts.column.state}}',
                  meaning:
                    '{{ui:alerts.filter.firing}} (default), {{ui:alerts.filter.allStates}}, {{ui:alerts.filter.pending}} or {{ui:alerts.status.resolved}}. The engine never stores pending alerts, so the {{ui:alerts.filter.pending}} option shows nothing in practice.',
                },
                {
                  term: '{{ui:alerts.column.severity}}',
                  meaning:
                    '{{ui:alerts.filter.allSeverities}}, {{ui:alerts.filter.critical}}, {{ui:alerts.filter.warning}} or {{ui:alerts.filter.info}}. The severity is set by the rule that raised the alert.',
                },
                {
                  term: '{{ui:alerts.filter.suppression}}',
                  meaning:
                    '{{ui:alerts.filter.allAlerts}} (default), {{ui:alerts.status.suppressed}} or {{ui:alerts.filter.notSuppressed}}. An alert is suppressed when an active silence matches it: see [{{ui:alerts.filter.suppression}}](/help/alerts#suppression).',
                },
                {
                  term: '{{ui:alerts.column.cluster}}',
                  meaning: 'Shows the alerts of one cluster. Empty means all clusters.',
                },
              ],
            },
            {
              kind: 'note',
              tone: 'info',
              text: 'The links in the {{ui:alerts.column.notes}} column open only the alerts one silence matches, through the `silence_id` filter. The button {{ui:alerts.list.showAll}} removes that filter.',
            },
          ],
        },
      ],
    },
    {
      id: 'summary-tiles',
      title: 'Alert summary',
      blocks: [
        {
          kind: 'text',
          text: 'Five counts over the alerts in the list. The firing counts leave out suppressed alerts; {{ui:alerts.status.suppressed}} and {{ui:alerts.status.resolved}} are counted apart.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.summary.criticalFiring}}',
              meaning:
                'Firing alerts of severity Critical that no silence suppresses. Above zero needs action now.',
              action:
                'Set {{ui:alerts.column.state}} to {{ui:alerts.filter.firing}} and {{ui:alerts.column.severity}} to {{ui:alerts.filter.critical}}, open each row and follow its rule in [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table).',
            },
            {
              term: '{{ui:alerts.summary.warningFiring}}',
              meaning:
                'Firing alerts of severity Warning that no silence suppresses. Check them after the Critical ones.',
              action:
                'The same steps as for {{ui:alerts.summary.criticalFiring}}, with {{ui:alerts.column.severity}} set to {{ui:alerts.filter.warning}}.',
            },
            {
              term: '{{ui:alerts.summary.infoFiring}}',
              meaning:
                'Firing alerts of severity Info that no silence suppresses. They inform; they rarely need a direct step.',
            },
            {
              term: '{{ui:alerts.status.suppressed}}',
              meaning:
                'Alerts matched by an active silence. They still fire and are still evaluated, but their notifications are not sent.',
              action:
                'Open the silence named in the {{ui:alerts.column.notes}} column. End it in [{{ui:shell.nav.silences}}](/help/silences#end-silence) if it no longer applies.',
            },
            {
              term: '{{ui:alerts.status.resolved}}',
              meaning:
                'Alerts whose condition no longer holds. They stay in the list with their resolved time.',
            },
          ],
        },
      ],
    },
    {
      id: 'alert-table',
      title: 'Alert list',
      blocks: [
        {
          kind: 'text',
          text: 'The alerts that match the filters, in this order: firing, then pending, then resolved; Critical, then Warning, then Info; not suppressed before suppressed; then by cluster and key. The page loads the 500 most recently evaluated alerts. When there are more, a notice says so, and narrowing the filters brings the rest into view.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.column.alert}}',
              meaning:
                'The alert key: the rule and the object it fires for. Its parts are shown under {{ui:alerts.detail.labels}} in Alert detail.',
            },
            {
              term: '{{ui:alerts.column.severity}}',
              meaning: 'Critical, Warning or Info, set by the rule. Critical is the most urgent.',
            },
            {
              term: '{{ui:alerts.column.state}}',
              meaning:
                'firing: the condition holds now. resolved: it no longer holds. pending is never stored.',
              action:
                'For firing alerts, follow the rule in [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table).',
            },
            {
              term: '{{ui:alerts.column.summary}}',
              meaning: 'What the rule detected, in one sentence.',
            },
            {
              term: '{{ui:alerts.column.cluster}}',
              meaning: 'The cluster the alert belongs to. Unknown when the alert has none.',
            },
            {
              term: '{{ui:alerts.column.instance}}',
              meaning: 'The instance the alert is for. Unknown when the alert is not tied to one.',
            },
            {
              term: '{{ui:alerts.column.started}}',
              meaning: 'When the alert started, in the console time zone.',
            },
            {
              term: '{{ui:alerts.column.notes}}',
              meaning:
                'Two things can appear: "{{ui:alerts.row.suppressedBy}}" with the silence reason and the time left, linked to that silence; and "{{ui:alerts.row.troubleshooting}}", shown for `agent_down` and `instance_unreachable`.',
              action:
                'For `agent_down` and `instance_unreachable`, open the troubleshooting checklist first.',
            },
          ],
        },
      ],
    },
    {
      id: 'alert-detail',
      title: 'Alert detail',
      blocks: [
        {
          kind: 'text',
          text: 'Select a row to open Alert detail above the list. {{ui:alerts.detail.close}} hides it. The detail has four fields: {{ui:alerts.detail.rule}}, {{ui:alerts.column.state}}, {{ui:alerts.detail.labels}} and {{ui:alerts.detail.history}}.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.detail.rule}}',
              meaning:
                'The id of the rule that raised the alert, such as `agent_down`. Its threshold and duration are in [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table).',
            },
            {
              term: '{{ui:alerts.column.state}}',
              meaning: 'The state of the alert, as in the list.',
            },
            {
              term: '{{ui:alerts.detail.labels}}',
              meaning:
                'The labels that identify the alert, shown as JSON. A silence matcher whose name is not one of the reserved names compares against these labels.',
            },
            {
              term: '{{ui:alerts.detail.history}}',
              meaning:
                'The {{ui:alerts.column.started}}, last evaluated and resolved timestamps. Resolved shows {{ui:alerts.detail.notYet}} while the alert is open.',
            },
          ],
        },
        {
          kind: 'steps',
          items: [
            'Read {{ui:alerts.detail.rule}} and {{ui:alerts.detail.labels}} to find the object the alert is for.',
            'Take the {{ui:alerts.column.started}} time from {{ui:alerts.detail.history}} and look for the cause in the [{{ui:shell.nav.events}}](/help/events#timeline) timeline around it.',
            'If {{ui:alerts.column.state}} is resolved, read the resolved timestamp to see when it cleared.',
          ],
        },
        {
          kind: 'note',
          tone: 'info',
          text: 'The page has no acknowledge or assign action, and it does not list the notifications sent for an alert. Notification channels are set on the server: see [{{ui:alerts.section.delivery}}](/help/alerts#delivery).',
        },
      ],
    },
    {
      id: 'suppression',
      title: 'Suppression',
      blocks: [
        {
          kind: 'text',
          text: 'An alert is {{ui:alerts.status.suppressed}} when an active silence matches it. The page computes this each time it reads the alerts, from the silences active at that moment. Suppression does not change the state: a suppressed alert still fires, is still evaluated and stays in the list. What it stops is the notification. The server decides on each notification when an alert starts firing or resolves; if a silence matches then, the notification is not sent. Ending a silence removes the suppression at the next read, but notifications already held back are not sent later.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.status.suppressed}}',
              meaning:
                'An active silence matches the alert. Its notifications were not sent when the transition happened.',
              action:
                'Read the silence named in {{ui:alerts.column.notes}}. If it still applies, leave it; if it does not, end it in [{{ui:shell.nav.silences}}](/help/silences#end-silence).',
            },
            {
              term: '{{ui:alerts.filter.notSuppressed}}',
              meaning:
                'No active silence matches. Its transitions are notified when a delivery channel is configured.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'A silence stops the notifications of the transitions that happen while it is active. It does not recall a notification already sent.',
        },
      ],
    },
    {
      id: 'delivery',
      title: 'Notification delivery',
      blocks: [
        {
          kind: 'text',
          text: 'The server sends a notification when an alert starts firing and when it resolves, unless a silence matches the alert at that moment. Delivery is set on the server with environment variables: the browser never receives a webhook address. The tab lists the supported channels and their variables. [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications) shows which channels are configured, can store an address that overrides the environment, and sends a test message.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Slack',
              meaning:
                'A Slack webhook. Variable: `PGLENS_ALERT_SLACK_WEBHOOK_URL`, or `PGLENS_ALERT_SLACK_WEBHOOK_URL_FILE` to read the address from a file. The aliases `PGLENS_SLACK_WEBHOOK_URL` and `PGLENS_SLACK_WEBHOOK_URL_FILE` are read too.',
              action:
                'Set one of the variables on the server, then send a test from [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications).',
            },
            {
              term: '{{ui:alerts.channel.label.webhook}}',
              meaning: 'Any HTTP webhook. Variable: `PGLENS_WEBHOOK_URL`.',
              action:
                'Set the variable on the server, then send a test from [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications).',
            },
            {
              term: 'Google Chat',
              meaning:
                'A Google Chat space webhook. Variable: `PGLENS_GOOGLE_CHAT_WEBHOOK_URL`, or `PGLENS_GOOGLE_CHAT_WEBHOOK_URL_FILE` to read the address from a file.',
              action:
                'Set one of the variables on the server, then send a test from [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications).',
            },
            {
              term: 'Microsoft Teams',
              meaning:
                'A Teams workflow webhook ("Post to a channel when a webhook request is received"). Variable: `PGLENS_TEAMS_WEBHOOK_URL`, or `PGLENS_TEAMS_WEBHOOK_URL_FILE` to read the address from a file.',
              action:
                'Set one of the variables on the server, or store the URL from [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications), then send a test.',
            },
            {
              term: 'PagerDuty',
              meaning:
                'A PagerDuty service, through the Events API v2: an alert opens an incident and its resolution resolves it. Variable: `PGLENS_PAGERDUTY_ROUTING_KEY`, or `PGLENS_PAGERDUTY_ROUTING_KEY_FILE`.',
              action:
                'Set one of the variables on the server, or store the routing key from [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications), then send a test.',
            },
            {
              term: '{{ui:alerts.channel.label.email}}',
              meaning:
                "Mail through the server's SMTP server (`PGLENS_SMTP_HOST`, `PGLENS_SMTP_FROM`, and the other `PGLENS_SMTP_*` variables). The recipients are set only from the console.",
              action:
                'Configure the mail server, then store the recipients from [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications) and send a test.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'If no delivery channel is configured, alerts are stored but not delivered, and the tab shows a note saying so. Silencing an alert suppresses its notifications only: the alert stays visible and keeps being evaluated.',
        },
      ],
    },
    {
      id: 'handling',
      title: 'Handling a firing alert',
      blocks: [
        {
          kind: 'steps',
          items: [
            'Set {{ui:alerts.column.state}} to {{ui:alerts.filter.firing}} and {{ui:alerts.column.severity}} to {{ui:alerts.filter.critical}}. Then move down to {{ui:alerts.filter.warning}} and {{ui:alerts.filter.info}}.',
            'Open the row and read its {{ui:alerts.column.summary}}. Then open [{{ui:shell.nav.alertRules}}](/help/alert-rules#rules-table) for the rule threshold and what to check.',
            'Look at the same object in the [{{ui:shell.nav.events}}](/help/events#timeline) timeline around the {{ui:alerts.column.started}} time. An event often names the cause, such as a failover or an agent that stopped reporting.',
            'Fix the cause, then check that the alert moves to resolved and that {{ui:alerts.detail.history}} shows a resolved time.',
            'Silence the alert only when its noise is known and accepted: see [{{ui:shell.nav.silences}}](/help/silences#create).',
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
              term: '{{ui:alerts.empty.none}}',
              meaning:
                'The fleet has emitted no alert. If you expect one, check the agents in [{{ui:shell.nav.events}}](/help/events#timeline).',
            },
            {
              term: '{{ui:alerts.empty.noMatch}}',
              meaning:
                'The filters hide every alert. Clear them, or set {{ui:alerts.column.state}} to {{ui:alerts.filter.allStates}}.',
            },
            {
              term: 'Suppression details are unavailable; alerts remain visible without silence context.',
              meaning:
                'The silences could not be loaded, so alerts show without the silence context. {{ui:alerts.callout.retry}} loads them again.',
            },
            {
              term: 'Only the 500 most recently evaluated alerts are loaded.',
              meaning:
                'The list is cut at 500 alerts. Narrow it with {{ui:alerts.column.cluster}}, {{ui:alerts.column.severity}} or {{ui:alerts.filter.suppression}} to reach the others.',
            },
            {
              term: 'The silence named in this link is not in the current silences list.',
              meaning:
                'The link points to a silence that is not loaded, so the list is empty. {{ui:alerts.list.showAll}} removes the filter.',
            },
            {
              term: 'Notifications do not arrive',
              meaning:
                'Check in order: a channel variable is set on the server; a test sent from [{{ui:shell.nav.settings}}, {{ui:settings.tabs.notifications}}](/settings?tab=notifications) arrives; the alert is {{ui:alerts.filter.notSuppressed}}; and its transition happened after the channel was set, because notifications are sent only on transitions.',
            },
          ],
        },
      ],
    },
  ],
}
