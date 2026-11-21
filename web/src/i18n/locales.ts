import registry from './locales.json'

/*
 * The registry of the console languages (plan 006 D1): one entry per language
 * in locales.json, read by the console and by the Go tests of internal/i18n.
 * A `stable` language has every key of the English catalog; a `beta` one may
 * miss keys, which then read in English.
 */

export type LocaleStatus = 'stable' | 'beta'

export interface LocaleInfo {
  /** BCP 47 code, the name of the catalog folder in messages/. */
  code: string
  /** The language's own name, shown in the language selector. */
  name: string
  /** The Intl locale that formats numbers and plural rules. */
  intl: string
  status: LocaleStatus
}

export const REFERENCE_LOCALE = 'en'

/**
 * English with accented letters, 35% longer and in brackets (I18N-003): text
 * still written in the code stays plain, clipped text shows. Development and
 * tests only, never offered by a production build.
 */
export const PSEUDO_LOCALE = 'en-XA'

export const LOCALES: readonly LocaleInfo[] = registry as LocaleInfo[]

export function pseudoLocaleEnabled(): boolean {
  return import.meta.env.DEV || import.meta.env.MODE === 'test'
}

/** Every code a language preference may name. */
export function localeCodes(): string[] {
  const codes = LOCALES.map((locale) => locale.code)
  return pseudoLocaleEnabled() ? [...codes, PSEUDO_LOCALE] : codes
}

export function localeInfo(code: string): LocaleInfo {
  if (code === PSEUDO_LOCALE) {
    return { code, name: 'Pseudo (en-XA)', intl: 'en-US', status: 'beta' }
  }
  return LOCALES.find((locale) => locale.code === code) ?? LOCALES[0]!
}

/** The registry language that best matches BCP 47 tags, in their order ("it-IT" gives it). */
export function matchLocale(tags: readonly string[]): string | null {
  for (const tag of tags) {
    const wanted = tag.trim().toLowerCase().replace('_', '-')
    const language = wanted.split('-')[0]
    const match =
      LOCALES.find((locale) => locale.code.toLowerCase() === wanted) ??
      LOCALES.find((locale) => locale.code.toLowerCase() === language)
    if (match) return match.code
  }
  return null
}
