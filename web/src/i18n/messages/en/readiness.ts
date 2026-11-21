// Plan 006 F3: the readiness pages. Keys stay stable: the other languages use them.
export const readiness = {
  'readiness.fix.requiresRestart': 'Requires restart',
  'readiness.fix.openConsole': 'Open in the console',
  'readiness.fix.snippet': '{title} snippet',
  'readiness.banner.open': '{text}. Open the readiness tab.',
  'readiness.banner.body':
    'Some features have less data than they could. See what is missing and how to fix it.',
  'readiness.item.warning': 'Warning',
  'readiness.item.note': 'Note',
  'readiness.item.hide': 'Hide details',
  'readiness.item.show': 'Show details',
  'readiness.item.impact': 'What you lose',
  'readiness.item.evidence': 'Evidence:',
  'readiness.section.endpoint': 'instance readiness',
  'readiness.section.loadingLabel': 'Loading readiness',
  'readiness.section.loading': 'Loading readiness…',
  'readiness.section.help': 'Readiness',
  'readiness.section.title': 'Readiness',
  'readiness.section.emptyDescription': 'Everything pglens can use is set up on this server.',
  'readiness.section.emptyTitle': 'Monitored in full',
  'readiness.channel.console': 'In the console',
  'readiness.channel.agent_config': 'Agent configuration',
  'readiness.channel.sql': 'SQL',
  'readiness.channel.postgres_config': 'PostgreSQL configuration',
  'readiness.channel.host': 'On the host',
  'readiness.badge.full': 'Full',
  'readiness.badge.partial':
    '{warnings, plural, one {Partial · # warning} other {Partial · # warnings}}',
  'readiness.badge.labelFull': 'Monitoring: full',
  'readiness.badge.labelPartial':
    'Monitoring: partial, {warnings, plural, one {# warning} other {# warnings}}',
  'readiness.badge.labelPartialNotes':
    'Monitoring: partial, {warnings, plural, one {# warning} other {# warnings}}, {notes, plural, one {# note} other {# notes}}',
} as const
