import {
  getPreferencesSnapshot,
  subscribePreferences,
  type LanguagePreference,
} from '@/lib/preferences'

import type { DomainTexts } from './domainTypes'
import { formatMessage, type MessageArgs } from './format'
import { localeCodes, localeInfo, matchLocale, PSEUDO_LOCALE, REFERENCE_LOCALE } from './locales'
import { messages as english, type Catalog, type MessageKey } from './messages/en'
import { pseudoLocalize } from './pseudo'

/*
 * The language the console is written in (plan 006 D6): the operator's choice
 * in Settings › Preferences, else the server default (PGLENS_LANG, handed over
 * by GET /api/v1/session), else the browser's languages, else English. English
 * ships in the main bundle; every other catalog is a lazy chunk, loaded before
 * the console switches to it, so a page never shows half of each.
 */

const loaders = import.meta.glob<{ messages: Partial<Catalog> }>([
  './messages/*/index.ts',
  '!./messages/en/index.ts',
])

const domainLoaders = import.meta.glob<{ domain: DomainTexts }>('./domain/*/index.ts')

const catalogs = new Map<string, Partial<Catalog>>([[REFERENCE_LOCALE, english]])
const domains = new Map<string, DomainTexts>()
const pseudoCatalog = new Map<MessageKey, string>()
const listeners = new Set<() => void>()
let serverLanguage: string | null = null
let displayed = REFERENCE_LOCALE

function browserLanguages(): readonly string[] {
  if (typeof navigator === 'undefined') return []
  return navigator.languages.length > 0 ? navigator.languages : [navigator.language]
}

export function resolveLocale(
  choice: LanguagePreference,
  server: string | null = serverLanguage,
  browser: readonly string[] = browserLanguages(),
): string {
  if (choice !== 'server' && choice !== 'browser' && localeCodes().includes(choice)) return choice
  if (choice === 'server' && server !== null) return server
  return matchLocale(browser) ?? REFERENCE_LOCALE
}

/** The language the console should be in now, loaded or not. */
export function targetLocale(): string {
  return resolveLocale(getPreferencesSnapshot().language)
}

/** The language the console is written in right now: always a loaded one. */
export function activeLocale(): string {
  return displayed
}

/** The Intl locale of the console language, the default of the lib/format formatters. */
export function activeIntl(): string {
  return localeInfo(displayed).intl
}

/**
 * A domain text (plan 006 F4) in the console language: the loaded translation,
 * the pseudo-locale form, or the English text when the language has none.
 */
export function domainText(
  english: string,
  pick: (domain: DomainTexts) => string | undefined,
): string {
  if (displayed === PSEUDO_LOCALE) return pseudoLocalize(english)
  const domain = domains.get(displayed)
  return (domain === undefined ? undefined : pick(domain)) ?? english
}

export function serverLocale(): string | null {
  return serverLanguage
}

async function load(code: string): Promise<void> {
  if (code === PSEUDO_LOCALE || catalogs.has(code)) return
  const loader = loaders[`./messages/${code}/index.ts`]
  if (!loader) return
  const domainLoader = domainLoaders[`./domain/${code}/index.ts`]
  try {
    const [module, domainModule] = await Promise.all([loader(), domainLoader?.()])
    if (domainModule) domains.set(code, domainModule.domain)
    catalogs.set(code, module.messages)
  } catch {
    // The chunk did not load (offline, new deploy): the console stays in English.
  }
}

function notify(): void {
  for (const listener of listeners) listener()
}

function applyDocumentLanguage(): void {
  if (typeof document !== 'undefined') document.documentElement.lang = displayed
}

/** Loads the target language, then switches the console to it. */
export async function refreshLocale(): Promise<void> {
  const target = targetLocale()
  await load(target)
  if (targetLocale() !== target) return
  const next = target === PSEUDO_LOCALE || catalogs.has(target) ? target : REFERENCE_LOCALE
  if (next !== displayed) {
    displayed = next
    notify()
  }
  applyDocumentLanguage()
}

/** Records the server default (PGLENS_LANG); an unknown language counts as none. */
export function setServerLanguage(code: string | undefined): Promise<void> {
  const next = code ? matchLocale([code]) : null
  if (next === serverLanguage) return Promise.resolve()
  serverLanguage = next
  return refreshLocale().then(notify)
}

export function subscribeLocale(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

subscribePreferences(() => {
  if (targetLocale() !== displayed) void refreshLocale()
})

function pseudoMessage(key: MessageKey): string {
  let message = pseudoCatalog.get(key)
  if (message === undefined) {
    message = pseudoLocalize(english[key])
    pseudoCatalog.set(key, message)
  }
  return message
}

/** The text of `key` in `locale`; a key the language lacks reads in English. */
export function translate(
  key: MessageKey,
  args: MessageArgs = {},
  locale: string = displayed,
): string {
  if (locale === PSEUDO_LOCALE) return formatMessage('en-US', pseudoMessage(key), args)
  const message = catalogs.get(locale)?.[key]
  if (message === undefined)
    return formatMessage(localeInfo(REFERENCE_LOCALE).intl, english[key], args)
  return formatMessage(localeInfo(locale).intl, message, args)
}
