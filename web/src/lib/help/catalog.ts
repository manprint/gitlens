import { activeLocale } from '@/i18n/store'

import { helpLang, type HelpGroup, type LangText } from './types'

/**
 * The chapters of the Inline Help, in menu order: id, group and title. It holds
 * no content, so the breadcrumbs and the help links can name a chapter without
 * loading the guide. Each topic file in `topics/<lang>/` must match its entry (a test
 * checks it).
 */
export const HELP_CATALOG: readonly { id: string; group: HelpGroup; title: LangText }[] = [
  { id: 'getting-started', group: 'start', title: { en: 'Getting started', it: 'Per iniziare' } },
  { id: 'fleet', group: 'monitoring', title: { en: 'Fleet', it: 'Panoramica della flotta' } },
  { id: 'cluster', group: 'monitoring', title: { en: 'Cluster detail', it: 'Dettaglio cluster' } },
  {
    id: 'slowest-queries',
    group: 'monitoring',
    title: { en: 'Slowest queries', it: 'Query più lente' },
  },
  {
    id: 'indexes',
    group: 'monitoring',
    title: { en: 'Index analysis', it: 'Analisi degli indici' },
  },
  {
    id: 'findings',
    group: 'monitoring',
    title: { en: 'Findings', it: 'Segnalazioni dell’advisor' },
  },
  {
    id: 'instance-overview',
    group: 'instance',
    title: { en: 'Instance overview', it: 'Panoramica dell’istanza' },
  },
  {
    id: 'query-performance',
    group: 'instance',
    title: { en: 'Query performance', it: 'Prestazioni delle query' },
  },
  {
    id: 'workbook',
    group: 'instance',
    title: { en: 'Tuning workbook', it: 'Quaderno di tuning' },
  },
  { id: 'wait-events', group: 'instance', title: { en: 'Wait events', it: 'Wait events' } },
  { id: 'locks', group: 'instance', title: { en: 'Locks and activity', it: 'Lock e attività' } },
  { id: 'logs', group: 'instance', title: { en: 'PostgreSQL logs', it: 'Log di PostgreSQL' } },
  { id: 'alerts', group: 'alerting', title: { en: 'Alerts', it: 'Alert' } },
  { id: 'events', group: 'alerting', title: { en: 'Events', it: 'Eventi' } },
  { id: 'alert-rules', group: 'alerting', title: { en: 'Alert rules', it: 'Regole di alert' } },
  { id: 'silences', group: 'alerting', title: { en: 'Silences', it: 'Silenziamenti' } },
  { id: 'settings', group: 'admin', title: { en: 'Settings', it: 'Impostazioni' } },
  { id: 'mcp', group: 'admin', title: { en: 'MCP server', it: 'Server MCP' } },
]

/** The title of a chapter in the help language of `locale`, or undefined. */
export function helpTopicTitle(id: string, locale: string = activeLocale()): string | undefined {
  return HELP_CATALOG.find((entry) => entry.id === id)?.title[helpLang(locale)]
}
