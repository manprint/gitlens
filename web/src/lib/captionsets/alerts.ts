import type { CaptionEntry } from '../captions'

/** Captions of the alerts route group (D30); rules in the plan, phase 17. */
export const alertsCaptions = {
  'alerts.list': {
    what: 'Alerts raised by the alert rules, with their state, severity, cluster, instance and start time.',
    source:
      'Server alerts endpoint over the alerts table, filled by the alert engine from the alert rules; the 500 most recently evaluated. Suppression is read from the stored silences.',
    unit: 'alerts',
    window:
      'Every state kept by the server; resolved alerts stay listed. The engine re-evaluates the rules every 30 seconds by default.',
    howToRead:
      'Ranked firing, then resolved; within a state critical before warning before info. The filters narrow the loaded rows in the browser and are kept in the address. Notes names the silence that suppresses an alert, with the time it has left.',
  },
  'alerts.detail': {
    what: 'One alert in full: its rule, state, labels, and when it started, was last evaluated and was resolved.',
    source:
      'Server alert endpoint for one alert key, the newest row of the alerts table with that key.',
    window: 'Read when opened; started, last evaluated and resolved are UTC timestamps.',
    howToRead:
      'Labels are the identifiers the alert was raised with. A silence matches an alert on these labels.',
  },
  'alerts.delivery': {
    what: 'The server environment variables that set up each alert delivery channel; nothing is changed from this page.',
    source:
      'The channel list of the console (Slack, generic webhook, Google Chat) and the variables the server reads at start: PGLENS_ALERT_SLACK_WEBHOOK_URL, PGLENS_WEBHOOK_URL and PGLENS_GOOGLE_CHAT_WEBHOOK_URL, each with a file variant.',
    window:
      'A change of the environment needs a server restart; an address stored from Settings, Notifications applies at once and overrides it.',
    howToRead:
      'Without a configured channel, alerts are kept but not delivered. A silence stops the notification only; the alert stays visible and keeps being evaluated.',
  },
  'alerts.summary': {
    what: 'Counts of the loaded alerts: firing by severity, suppressed by a silence, and resolved.',
    source:
      'Counted in the browser over the alerts loaded from the server alerts endpoint (all states, the 500 most recently evaluated).',
    unit: 'alerts',
    window: 'The loaded alerts; the filters of the list do not change the counts.',
    howToRead:
      'A suppressed alert is counted under Suppressed and not under firing, so the firing counts are the alerts that notify. A resolved alert that was silenced is counted in both.',
  },
  'alerts.filter-state': {
    what: 'Shows only the alerts in the chosen state; Firing is the default and All states removes the filter.',
    source:
      'The state column of the alerts table (firing or resolved; pending episodes stay in the engine memory and are never stored) on the alerts loaded; the filter runs in the browser.',
    unit: 'alert state',
    window: 'The loaded alerts.',
    howToRead:
      'Firing means the rule condition held for its whole duration; resolved means it no longer holds or its subject stopped reporting. Pending is never stored, so the Pending filter shows no alert.',
  },
  'alerts.filter-severity': {
    what: 'Shows only the alerts of the chosen severity: critical, warning or info.',
    source:
      'The severity column of the alerts table, set by the rule that raised the alert; the filter runs in the browser.',
    unit: 'alert severity',
    window: 'The loaded alerts.',
  },
  'alerts.filter-cluster': {
    what: 'Shows only the alerts whose cluster ID equals the number typed; names and partial numbers match nothing.',
    source:
      'The cluster_id column of the alerts table, a decimal number; the filter compares it in the browser with the text typed.',
    unit: 'cluster ID',
    window: 'The loaded alerts.',
    howToRead: 'Leave it empty for every cluster. An alert with no cluster never matches.',
  },
  'alerts.filter-suppression': {
    what: 'Shows only the alerts a silence suppresses, or only those it does not.',
    source:
      'The suppressed flag the server computes for each alert from the silences active now; the filter runs in the browser.',
    unit: 'suppressed or not',
    window: 'Silences active now.',
  },
  'events.timeline': {
    what: 'Fleet events in the selected time range: failovers, restarts, identity changes and other changes the server recorded.',
    source:
      'Server events endpoint over the events table, written by the ingest and the alert engine; filtered by cluster, type and time range on the server.',
    unit: 'events',
    window:
      'The selected time range, newest first, cut to the event limit (100 by default, at most 1,000); events older than 90 days are purged.',
    howToRead:
      'Severity is assigned by the console from the event type. Details says what to check for that type. An event type the console does not know shows its raw name.',
  },
  'events.alert-status': {
    what: 'Every alert with its severity, state and whether a silence suppresses it, apart from the event history.',
    source:
      'Server alerts endpoint over the alerts table with the stored silences; one row per alert key.',
    unit: 'alerts',
    window: 'All states kept by the server, the 500 most recently evaluated.',
    howToRead:
      'Suppression is Unknown when the silences could not be loaded. A suppressed alert is still evaluated.',
  },
  'events.filter-cluster': {
    what: 'Lists only the events of the cluster whose ID is typed; the server refuses anything that is not a number.',
    source:
      'The cluster_id parameter of the server events endpoint, compared with the cluster_id column of the events table.',
    unit: 'cluster ID',
    window: 'The selected time range.',
    howToRead: 'Leave it empty for every cluster.',
  },
  'events.filter-type': {
    what: 'Lists only the events of one type, such as failover_detected or slot_lost.',
    source:
      'The type parameter of the server events endpoint, compared with the type column of the events table; the choices are the types in the events loaded.',
    unit: 'event type',
    window: 'The selected time range.',
  },
  'events.filter-limit': {
    what: 'The most events the server returns for the range: 100, 500 or 1,000.',
    source:
      'The limit parameter of the server events endpoint; the server keeps the newest events and caps any larger limit at 1,000.',
    unit: 'events',
    window: 'The selected time range.',
    howToRead:
      'When the list reaches 1,000 the page says older events may exist outside the view; narrow the range or the filters.',
  },
  'rules.list': {
    what: 'Built-in and stored alert rules with their severity, threshold, duration and whether they are enabled.',
    source:
      'Server alert-rules endpoint: the built-in rules compiled into the server plus the enabled rows of the alert_rules table.',
    unit: 'rules',
    window: 'Live catalogue; an edit applies at the next evaluation.',
    howToRead:
      'Tier T0 rules about agent and cluster health are always on and cannot be edited. Threshold is in the unit of the rule metric, and a rule raised by an event type has none. Duration is how long the condition must hold before the alert fires.',
  },
  'rules.edit': {
    what: 'Change the threshold, duration, severity and enabled state of one rule; the server validates and saves the change.',
    source:
      'Server alert-rules endpoint for one rule, which updates the alert_rules row and returns the saved values.',
    unit: 'rule settings',
    window: 'Applies at the next evaluation of the alert rules.',
    howToRead:
      'Each field states its effect below it. Nothing changes until Save rule succeeds; Cancel discards the form.',
  },
  'silences.list': {
    what: 'Silences that stop alert notifications: pending, active and expired ones, with their matchers, reason and time window.',
    source:
      'Server silences endpoint over the silences table, newest ending first and capped like the alert list; the state is computed in the browser from the window.',
    unit: 'silences',
    window: 'Pending before Start, active from Start until End, expired after End; times are UTC.',
    howToRead:
      'A silence suppresses the alerts that match every matcher. It stops notifications only; the alert stays listed and keeps being evaluated. End silence stops one early or cancels a pending one.',
  },
} as const satisfies Record<string, CaptionEntry>
