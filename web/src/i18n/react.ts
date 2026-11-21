import { useMemo, useSyncExternalStore } from 'react'

import type { MessageArgs } from './format'
import { localeInfo } from './locales'
import type { MessageKey } from './messages/en'
import { activeLocale, serverLocale, subscribeLocale, translate } from './store'

export type Translate = (key: MessageKey, args?: MessageArgs) => string

/** The console language; the component re-renders when it changes. */
export function useLocale(): string {
  return useSyncExternalStore(subscribeLocale, activeLocale, activeLocale)
}

/** The server default language (PGLENS_LANG), null until the session names one. */
export function useServerLocale(): string | null {
  return useSyncExternalStore(subscribeLocale, serverLocale, serverLocale)
}

/** `t(key, args)` in the console language (plan 006 D4). */
export function useT(): Translate {
  const locale = useLocale()
  return useMemo(() => (key, args) => translate(key, args, locale), [locale])
}

export interface LocaleFormat {
  locale: string
  /** The Intl locale of the console language. */
  intl: string
  number: (value: number, options?: Intl.NumberFormatOptions) => string
}

/** Numbers in the console language; lib/format moves onto it area by area (plan 006 F2). */
export function useFormat(): LocaleFormat {
  const locale = useLocale()
  return useMemo(() => {
    const intl = localeInfo(locale).intl
    return {
      locale,
      intl,
      number: (value, options) => new Intl.NumberFormat(intl, options).format(value),
    }
  }, [locale])
}
