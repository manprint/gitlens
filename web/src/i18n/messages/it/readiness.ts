import type { readiness as english } from '../en/readiness'
import type { Translation } from '../../types'

export const readiness: Translation<typeof english> = {
  'readiness.fix.requiresRestart': 'Richiede riavvio',
  'readiness.fix.openConsole': 'Apri nella console',
  'readiness.fix.snippet': 'snippet di {title}',
  'readiness.banner.open': '{text}. Apri la scheda Readiness.',
  'readiness.banner.body':
    'Alcune funzioni hanno meno dati del possibile. Vedi cosa manca e come risolverlo.',
  'readiness.item.warning': 'Avviso',
  'readiness.item.note': 'Nota',
  'readiness.item.hide': 'Nascondi dettagli',
  'readiness.item.show': 'Mostra dettagli',
  'readiness.item.impact': 'Cosa perdi',
  'readiness.item.evidence': 'Evidenza:',
  'readiness.section.endpoint': 'readiness dell’istanza',
  'readiness.section.loadingLabel': 'Caricamento della readiness',
  'readiness.section.loading': 'Caricamento della readiness…',
  'readiness.section.help': 'Readiness',
  'readiness.section.title': 'Readiness',
  'readiness.section.emptyDescription':
    'Tutto ciò che pglens può usare è configurato su questo server.',
  'readiness.section.emptyTitle': 'Monitorato per intero',
  'readiness.channel.console': 'Nella console',
  'readiness.channel.agent_config': 'Configurazione dell’agent',
  'readiness.channel.sql': 'SQL',
  'readiness.channel.postgres_config': 'Configurazione di PostgreSQL',
  'readiness.channel.host': 'Sull’host',
  'readiness.badge.full': 'Completo',
  'readiness.badge.partial':
    '{warnings, plural, one {Parziale · # avviso} other {Parziale · # avvisi}}',
  'readiness.badge.labelFull': 'Monitoraggio: completo',
  'readiness.badge.labelPartial':
    'Monitoraggio: parziale, {warnings, plural, one {# avviso} other {# avvisi}}',
  'readiness.badge.labelPartialNotes':
    'Monitoraggio: parziale, {warnings, plural, one {# avviso} other {# avvisi}}, {notes, plural, one {# nota} other {# note}}',
}
