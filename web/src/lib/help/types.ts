/**
 * The languages the Inline Help is written in: one folder each in `topics/`.
 * A console language without its folder reads the English help.
 */
export const HELP_LANGS = ['en', 'it'] as const
export type HelpLang = (typeof HELP_LANGS)[number]

/** A text in every help language, for the names the console shows without the guide. */
export type LangText = Readonly<Record<HelpLang, string>>

/**
 * A help text in the topic's language. Inside it, `` `code` `` is shown as code,
 * `[label](/help/topic#section)` as a link (any in-app path works) and
 * `{{ui:key}}` as the console label of that message key, in the same language.
 */
export type Text = string

export interface HelpTerm {
  term: Text
  meaning: Text
  /** What to do about it, when there is something to do. */
  action?: Text
}

export type HelpBlock =
  | { kind: 'text'; text: Text }
  | { kind: 'terms'; items: readonly HelpTerm[] }
  | { kind: 'steps'; items: readonly Text[] }
  | { kind: 'sql'; label: Text; code: string }
  | { kind: 'note'; tone: 'info' | 'warning'; text: Text }

export interface HelpSection {
  /** The anchor: `/help/<topic>#<id>`, unique within the topic (subsections included). */
  id: string
  title: Text
  blocks: readonly HelpBlock[]
  /** One level of subsections, each with its own anchor. */
  subsections?: readonly HelpSection[]
}

export type HelpGroup = 'start' | 'monitoring' | 'instance' | 'alerting' | 'admin'

export interface HelpTopic {
  id: string
  group: HelpGroup
  title: Text
  summary: Text
  /** The console page the topic documents, linked from its header. */
  page?: { path: string; label: Text }
  sections: readonly HelpSection[]
}

export const HELP_GROUP_TITLE: Readonly<Record<HelpGroup, LangText>> = {
  start: { en: 'Getting started', it: 'Per iniziare' },
  monitoring: { en: 'Monitoring', it: 'Monitoraggio' },
  instance: { en: 'Instance pages', it: 'Pagine di istanza' },
  alerting: { en: 'Alerting', it: 'Alert' },
  admin: { en: 'Administration', it: 'Amministrazione' },
}

/** The help language of a console language: its own when the help has it, else English. */
export function helpLang(locale: string): HelpLang {
  return (HELP_LANGS as readonly string[]).includes(locale) ? (locale as HelpLang) : 'en'
}
