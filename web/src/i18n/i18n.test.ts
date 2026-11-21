import { afterEach, describe, expect, it } from 'vitest'

import { resetPreferences, updatePreferences } from '@/lib/preferences'

import cases from './testdata/format-cases.json'
import { formatMessage, messageNumber, placeholders, pluralSelectors } from './format'
import { localeCodes, localeInfo, LOCALES, matchLocale, PSEUDO_LOCALE } from './locales'
import { messages as english, type MessageKey } from './messages/en'
import { PSEUDO_GROWTH, pseudoLocalize } from './pseudo'
import {
  activeLocale,
  refreshLocale,
  resolveLocale,
  serverLocale,
  setServerLanguage,
  translate,
} from './store'

const catalogs = import.meta.glob<{ messages: Record<string, string> }>('./messages/*/index.ts', {
  eager: true,
})
const catalogOf = (code: string) => catalogs[`./messages/${code}/index.ts`]?.messages

// The plural categories a language uses for whole and decimal numbers.
function requiredCategories(intl: string): Set<string> {
  const rules = new Intl.PluralRules(intl)
  return new Set([0, 1, 2, 3, 5, 11, 21, 100, 1000, 1.5].map((n) => rules.select(n)))
}

afterEach(async () => {
  resetPreferences()
  await setServerLanguage(undefined)
  await refreshLocale()
})

describe('locale registry', () => {
  it('I18N-010 lists unique languages, English first and stable, each with its catalog folder', () => {
    const codes = LOCALES.map((locale) => locale.code)
    expect(new Set(codes).size).toBe(codes.length)
    expect(LOCALES[0]).toMatchObject({ code: 'en', status: 'stable' })
    for (const locale of LOCALES) {
      expect(Intl.getCanonicalLocales(locale.intl), locale.code).toHaveLength(1)
      expect(catalogOf(locale.code), locale.code).toBeDefined()
    }
    const folders = Object.keys(catalogs).map((path) => path.split('/')[2])
    expect(folders.sort()).toEqual([...codes].sort())
  })

  it('I18N-011 matches browser tags to a registry language', () => {
    expect(matchLocale(['it-IT', 'en'])).toBe('it')
    expect(matchLocale(['IT_ch'])).toBe('it')
    expect(matchLocale(['fr-CA', 'en-GB'])).toBe('en')
    expect(matchLocale(['xx'])).toBeNull()
    expect(localeInfo('zz').code).toBe('en')
    expect(localeInfo(PSEUDO_LOCALE).intl).toBe('en-US')
    expect(localeCodes()).toContain(PSEUDO_LOCALE)
  })
})

describe('I18N-001 catalog parity', () => {
  it.each(LOCALES.filter((locale) => locale.code !== 'en'))(
    '$code has the reference keys, placeholders and plural categories',
    ({ code, intl, status }) => {
      const catalog = catalogOf(code)!
      const keys = Object.keys(english) as MessageKey[]
      expect(Object.keys(catalog).filter((key) => !(key in english))).toEqual([])
      if (status === 'stable') expect(keys.filter((key) => !(key in catalog))).toEqual([])
      const required = requiredCategories(intl)
      for (const key of keys) {
        const message = catalog[key]
        if (message === undefined) continue
        expect(placeholders(message), key).toEqual(placeholders(english[key]))
        for (const selectors of pluralSelectors(message)) {
          expect(selectors, key).toContain('other')
          for (const category of required)
            expect(selectors, `${key} ${category}`).toContain(category)
        }
      }
    },
  )

  it('English plurals carry the categories English needs', () => {
    for (const [key, message] of Object.entries(english)) {
      for (const selectors of pluralSelectors(message)) {
        for (const category of requiredCategories('en-US')) {
          expect(selectors, key).toContain(category)
        }
      }
    }
  })
})

describe('I18N-004 message format', () => {
  it.each(cases)('$locale: $message', ({ locale, message, args, want }) => {
    expect(formatMessage(localeInfo(locale).intl, message, args)).toBe(want)
  })

  it('writes numbers like internal/i18n and keeps unknown arguments', () => {
    expect(messageNumber('en-US', 100)).toBe('100')
    expect(messageNumber('it-IT', 0.1)).toBe('0,1')
    expect(formatMessage('en-US', '{a, number} {b}', { b: true })).toBe('{a} true')
    expect(formatMessage('en-US', 'open {brace', {})).toBe('open {brace')
    expect(formatMessage('en-US', '{n, plural, other {#}}', { n: 'x' })).toBe('0')
    expect(placeholders('{a} {n, plural, one {{b}} other {#}}')).toEqual(['a', 'b', 'n'])
  })
})

describe('I18N-003 pseudo-locale', () => {
  it('accents and lengthens the text and keeps its placeholders and plurals', () => {
    const message = english['settings.preferences.refreshInterval.every']
    const pseudo = pseudoLocalize(message)
    expect(pseudo.startsWith('[')).toBe(true)
    expect(pseudo.endsWith(']')).toBe(true)
    expect(placeholders(pseudo)).toEqual(placeholders(message))
    expect(pluralSelectors(pseudo)).toEqual(pluralSelectors(message))
    const plain = formatMessage('en-US', message, { seconds: 15 })
    const shown = translate(
      'settings.preferences.refreshInterval.every',
      { seconds: 15 },
      PSEUDO_LOCALE,
    )
    expect(shown).toContain('15')
    expect(shown).toMatch(/É/)
    expect(shown.length).toBeGreaterThanOrEqual(plain.length * (1 + PSEUDO_GROWTH))
    expect(pseudoLocalize('{zone}')).toBe('[{zone}]')
  })
})

describe('language resolution (D6)', () => {
  it('I18N-012 prefers the operator, then the server, then the browser, then English', () => {
    expect(resolveLocale('it', 'en', ['en'])).toBe('it')
    expect(resolveLocale('server', 'it', ['en'])).toBe('it')
    expect(resolveLocale('server', null, ['it-IT'])).toBe('it')
    expect(resolveLocale('browser', 'en', ['it'])).toBe('it')
    expect(resolveLocale('browser', 'it', ['de'])).toBe('en')
    expect(resolveLocale('fr', null, [])).toBe('en')
  })

  it('I18N-013 loads the server language, switches <html lang> and falls back to English per key', async () => {
    expect(activeLocale()).toBe('en')
    await setServerLanguage('it-IT')
    expect(serverLocale()).toBe('it')
    expect(activeLocale()).toBe('it')
    expect(document.documentElement.lang).toBe('it')
    expect(translate('settings.preferences.language')).toBe('Lingua')
    expect(translate('settings.preferences.refreshInterval.every', { seconds: 30 })).toBe(
      'Ogni 30 secondi',
    )
    expect(translate('settings.preferences.language', {}, 'fr')).toBe('Language')

    updatePreferences({ language: 'en' })
    await refreshLocale()
    expect(activeLocale()).toBe('en')
    expect(document.documentElement.lang).toBe('en')

    await setServerLanguage('xx')
    expect(serverLocale()).toBeNull()
  })
})
