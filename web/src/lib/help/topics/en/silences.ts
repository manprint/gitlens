import type { HelpTopic } from '../../types'

export const silencesTopic: HelpTopic = {
  id: 'silences',
  group: 'alerting',
  title: 'Silences',
  summary:
    'Stored silences that suppress notifications during planned work: their matchers, reason and window, how to create one and check its {{ui:alerts.silenceEditor.preview}}, and how to end one early.',
  page: {
    path: '/alerts/silences',
    label: 'Open Silences',
  },
  sections: [
    {
      id: 'overview',
      title: 'What the page is for',
      blocks: [
        {
          kind: 'text',
          text: 'The {{ui:shell.nav.silences}} page (title {{ui:alerts.silences.title}}) lists the silences stored on the server, and lets you create or end them. A silence suppresses notifications only: the alert stays visible on the [{{ui:alerts.section.alerts}}](/alerts) page and its rule keeps evaluating it. Use a silence for planned work, such as a maintenance window, when alerts are expected and should not notify. The header links to [{{ui:alerts.section.alerts}}](/alerts) and [{{ui:alerts.silences.rules}}](/alerts/rules), and has the button {{ui:alerts.silences.create}}, which opens the editor and changes to {{ui:alerts.silences.closeEditor}} while it is open.',
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
                  term: '{{ui:alerts.column.state}}',
                  meaning:
                    'Limits the list to {{ui:alerts.filter.pending}}, {{ui:alerts.silences.active}} or {{ui:alerts.silences.expired}} silences.',
                },
                {
                  term: '{{ui:shell.table.search}}',
                  meaning: 'Finds a silence by its matchers or its reason.',
                },
              ],
            },
          ],
        },
      ],
    },
    {
      id: 'matchers',
      title: 'Matchers',
      blocks: [
        {
          kind: 'text',
          text: 'A matcher is a name and a value. An alert is silenced when it matches every matcher of the silence, because matchers combine with AND. The value must equal the value of the alert exactly. The name is one of rule_id, severity, cluster_id, instance_id or datname, or any other alert label.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'Reserved names',
              meaning:
                'rule_id, severity, cluster_id, instance_id and datname. Each matches the alert field of the same name.',
            },
            {
              term: 'Alert label',
              meaning: 'Any other name matches a label of the alert with the same value.',
            },
            {
              term: '`name=value`',
              meaning:
                'How an exact matcher appears in the {{ui:alerts.silences.matchers}} column. Several matchers are separated by commas.',
            },
            {
              term: '`name~value`',
              meaning: 'A regex matcher. The editor does not create regex matchers.',
            },
            {
              term: '`not name=value`',
              meaning: 'A negated matcher. The editor does not create negated matchers.',
            },
          ],
        },
      ],
    },
    {
      id: 'window',
      title: 'Window and state',
      blocks: [
        {
          kind: 'text',
          text: 'A silence is in effect from {{ui:alerts.silences.start}} until {{ui:alerts.silences.end}}. {{ui:alerts.silences.start}} is included and {{ui:alerts.silences.end}} is not. In the editor, {{ui:alerts.silences.start}} and {{ui:alerts.silences.end}} are read in the console time zone and stored in UTC. The table shows the stored times in the console time zone. A silence is {{ui:alerts.filter.pending}} before {{ui:alerts.silences.start}}, {{ui:alerts.silences.active}} from {{ui:alerts.silences.start}} until {{ui:alerts.silences.end}}, and {{ui:alerts.silences.expired}} from {{ui:alerts.silences.end}} on.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.filter.pending}}',
              meaning:
                '{{ui:alerts.silences.start}} is in the future. The table shows {{ui:alerts.filter.pending}} with the time left until {{ui:alerts.silences.start}}.',
            },
            {
              term: '{{ui:alerts.silences.active}}',
              meaning:
                'The silence suppresses now. The table shows {{ui:alerts.silences.active}} with the time left until {{ui:alerts.silences.end}}.',
            },
            {
              term: '{{ui:alerts.silences.expired}}',
              meaning:
                '{{ui:alerts.silences.end}} has passed, or the silence was ended. The row stays in the list.',
            },
          ],
        },
      ],
    },
    {
      id: 'list',
      title: 'Stored silences',
      blocks: [
        {
          kind: 'text',
          text: 'The table lists every stored silence, {{ui:alerts.silences.expired}} ones included, sorted by {{ui:alerts.silences.end}} from the earliest to the latest. Columns: {{ui:alerts.silences.matchers}}, {{ui:alerts.silences.reason}}, {{ui:alerts.silences.start}}, {{ui:alerts.silences.end}}, {{ui:alerts.column.state}} and {{ui:alerts.silences.actions}}. {{ui:alerts.silences.start}} and {{ui:alerts.silences.end}} are shown in the console time zone. Each row has an {{ui:alerts.silences.endButton}} button.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.silences.matchers}}',
              meaning:
                'The matchers of the silence, shown as described in [Matchers](/help/silences#matchers).',
            },
            {
              term: '{{ui:alerts.silences.reason}}',
              meaning: 'The reason written when the silence was created.',
            },
            {
              term: '{{ui:alerts.column.state}}',
              meaning:
                '{{ui:alerts.filter.pending}}, {{ui:alerts.silences.active}} or {{ui:alerts.silences.expired}}, with the time left, as described in [Window and state](/help/silences#window).',
            },
            {
              term: '{{ui:alerts.silences.actions}}',
              meaning:
                '{{ui:alerts.silences.endButton}}, which ends the silence early. See [End a silence](/help/silences#end-silence).',
            },
          ],
        },
      ],
    },
    {
      id: 'create',
      title: 'Create silence',
      blocks: [
        {
          kind: 'text',
          text: '{{ui:alerts.silences.create}} opens the editor above the list, with one empty matcher, {{ui:alerts.silences.start}} set to now and {{ui:alerts.silences.end}} set to one hour later. The editor title is {{ui:alerts.silenceEditor.title}}, and its subtitle says that a silence suppresses notifications only. The editor creates a silence with exact matchers, a reason and a window.',
        },
        {
          kind: 'steps',
          items: [
            'Click {{ui:alerts.silences.create}} in the page header. The button changes to {{ui:alerts.silences.closeEditor}} while the editor is open.',
            'Fill Matcher 1 name and Matcher 1 value. {{ui:alerts.silenceEditor.addMatcher}} adds a row. {{ui:alerts.silenceEditor.remove}}, on a row, deletes it; {{ui:alerts.silenceEditor.remove}} appears only with two or more rows.',
            'Enter the {{ui:alerts.silences.reason}}: say why notifications should be suppressed.',
            'Set {{ui:alerts.silences.start}} and {{ui:alerts.silences.end}}, or click a preset: 1 hour, 4 hours or 24 hours sets {{ui:alerts.silences.start}} to now and {{ui:alerts.silences.end}} that many hours later.',
            'Read the {{ui:alerts.silenceEditor.preview}}. It lists the firing alerts this silence would suppress.',
            'If the warning appears, tick its checkbox, labelled I understand this will suppress notifications for all N currently firing alerts, where N is the number of firing alerts.',
            'Click {{ui:alerts.silenceEditor.submit}}. While the request runs, the button reads {{ui:alerts.silenceEditor.creating}}. On success the editor closes and the message {{ui:alerts.silences.created}} appears above the table.',
            'To leave without saving, click {{ui:alerts.silenceEditor.cancel}}.',
          ],
        },
        {
          kind: 'text',
          text: 'Messages that can appear before a silence is saved:',
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.silenceEditor.matcherIncomplete}}',
              meaning:
                'A matcher row has a name without a value, or a value without a name. It also appears when every row is empty. Fill both fields of each row, or remove the row with {{ui:alerts.silenceEditor.remove}}.',
            },
            {
              term: '{{ui:alerts.silenceEditor.reasonRequired}}',
              meaning: 'The {{ui:alerts.silences.reason}} field is empty. Enter a reason.',
            },
            {
              term: '{{ui:alerts.silenceEditor.datesInvalid}}',
              meaning:
                '{{ui:alerts.silences.start}} or {{ui:alerts.silences.end}} is not a valid date and time.',
            },
            {
              term: '{{ui:alerts.silenceEditor.endBeforeStart}}',
              meaning:
                '{{ui:alerts.silences.end}} is not later than {{ui:alerts.silences.start}}. Move {{ui:alerts.silences.end}} to a later time.',
            },
            {
              term: 'Confirm that this silence will suppress all N currently firing alerts.',
              meaning:
                'The silence would suppress every firing alert. Tick the warning checkbox, then click {{ui:alerts.silenceEditor.submit}} again. N is the number of firing alerts.',
            },
            {
              term: 'Complete a matcher to see which currently firing alerts would be silenced.',
              meaning: 'Shown until every filled matcher has both a name and a value.',
            },
            {
              term: 'Would suppress notifications for N currently firing alert(s):',
              meaning: 'The firing alerts this silence would suppress, listed below it.',
            },
            {
              term: '{{ui:alerts.silenceEditor.noMatch}}',
              meaning:
                'No firing alert matches. The silence still saves, and it suppresses alerts that match later.',
            },
          ],
        },
      ],
    },
    {
      id: 'effect',
      title: 'What a silence changes',
      blocks: [
        {
          kind: 'text',
          text: 'A silence suppresses notifications only. An alert that a silence matches keeps firing and stays in the [{{ui:alerts.section.alerts}}](/alerts) list, where it is shown as [{{ui:alerts.status.suppressed}}](/help/alerts#suppression). Its rule keeps evaluating it. Suppression is worked out each time the alerts are read, from the silences active at that moment.',
        },
        {
          kind: 'terms',
          items: [
            {
              term: 'FIRING',
              meaning:
                'A notification is sent when an alert starts firing, not on every evaluation. While a silence matches the alert, this notification is not sent.',
            },
            {
              term: 'RESOLVED',
              meaning:
                'The notification sent when an alert resolves is not sent while a silence matches the alert either.',
            },
            {
              term: 'Notifications already sent',
              meaning: 'A silence does not recall a notification that was already sent.',
            },
          ],
        },
      ],
    },
    {
      id: 'end-silence',
      title: 'End a silence',
      blocks: [
        {
          kind: 'text',
          text: 'A silence cannot be edited. To change one, end it and create another. Ending a silence never removes its row: the row stays in the list, and its window changes as described below.',
        },
        {
          kind: 'steps',
          items: [
            'In [{{ui:alerts.silences.stored}}](/help/silences#list), find the row and click {{ui:alerts.silences.endButton}}.',
            'Confirm the dialog. It asks to end the silence and says that notifications will no longer be suppressed.',
            'Read the message above the table: Ended silence, followed by the id of the silence.',
          ],
        },
        {
          kind: 'terms',
          items: [
            {
              term: '{{ui:alerts.silences.active}}',
              meaning:
                'Ends now. It stops suppressing at once, and its row reads {{ui:alerts.silences.expired}}.',
            },
            {
              term: '{{ui:alerts.filter.pending}}',
              meaning:
                'Its {{ui:alerts.silences.end}} moves to its {{ui:alerts.silences.start}}, so it never suppresses an alert. It reads {{ui:alerts.filter.pending}} until {{ui:alerts.silences.start}}, then {{ui:alerts.silences.expired}}.',
            },
            {
              term: '{{ui:alerts.silences.expired}}',
              meaning: 'Nothing changes. Its {{ui:alerts.silences.end}} is kept.',
            },
          ],
        },
        {
          kind: 'note',
          tone: 'warning',
          text: 'Ending a silence does not resend the notifications it held back.',
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
              term: '{{ui:alerts.silences.loading}}',
              meaning:
                'The list is loading. Wait a moment, then reload the page if it stays this way.',
            },
            {
              term: '{{ui:alerts.silences.emptyTitle}}',
              meaning:
                'No silence has been created yet. Create one with [{{ui:alerts.silences.create}}](/help/silences#create).',
            },
            {
              term: 'Current firing alerts are unavailable; the silence list remains available but creation preview is disabled.',
              meaning:
                'The firing alerts could not be read. You can still see and end silences, but the {{ui:alerts.silenceEditor.preview}} cannot verify matches and the warning checkbox does not appear. Check the matchers against the [{{ui:alerts.section.alerts}}](/alerts) page before saving, then click {{ui:alerts.silences.retryFiring}}.',
            },
            {
              term: 'The silence does not suppress an alert',
              meaning:
                'Check that the state is {{ui:alerts.silences.active}}, not {{ui:alerts.filter.pending}} or {{ui:alerts.silences.expired}}, and that each matcher value equals the value of the alert exactly. Compare them with the alert detail on the [{{ui:alerts.section.alerts}}](/alerts) page.',
            },
          ],
        },
      ],
    },
  ],
}
