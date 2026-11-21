import { useId, type ReactNode } from 'react'

import { localeInfo } from '@/i18n/locales'
import type { MessageKey } from '@/i18n/messages/en'
import { useServerLocale, useT, type Translate } from '@/i18n/react'
import { resolveLocale } from '@/i18n/store'

import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { usePreferences } from '@/hooks/usePreferences'
import { useDisplayTimeZone } from '@/hooks/useDisplayTimeZone'
import { useTheme } from '@/hooks/useTheme'
import {
  DEFAULT_PREFERENCES,
  DEFAULT_TIME_RANGES,
  languagePreferences,
  REFRESH_INTERVALS,
  resetPreferences,
  TABLE_PAGE_SIZES,
  TIME_ZONE_PREFERENCES,
  updatePreferences,
  type DefaultTimeRange,
  type LanguagePreference,
  type RefreshIntervalPreference,
  type ThemePreference,
  type TimeZonePreference,
} from '@/lib/preferences'
import { browserTimeZone, serverTimeZone } from '@/lib/timezone'
import { rangeLabelKey } from '@/lib/timerange'

const THEME_OPTIONS: readonly ThemePreference[] = ['light', 'dark', 'system']

function refreshLabel(t: Translate, interval: RefreshIntervalPreference): string {
  if (interval === 'auto' || interval === 'off') {
    return t(`settings.preferences.refreshInterval.${interval}`)
  }
  return t('settings.preferences.refreshInterval.every', { seconds: Number.parseInt(interval, 10) })
}

function timeZoneLabel(t: Translate, choice: TimeZonePreference): string {
  if (choice === 'server')
    return t('settings.preferences.timeZone.server', { zone: serverTimeZone() })
  if (choice === 'browser') {
    return t('settings.preferences.timeZone.browser', { zone: browserTimeZone() })
  }
  return 'UTC'
}

/** Each language in its own name; server and browser name the language they give now. */
function languageLabel(t: Translate, choice: LanguagePreference): string {
  const named = (key: 'server' | 'browser') =>
    t(`settings.preferences.language.${key}`, { language: localeInfo(resolveLocale(key)).name })
  if (choice === 'server' || choice === 'browser') return named(choice)
  return localeInfo(choice).name
}

/** What each control changes (I-16), under the key of its label. */
type PreferenceKey = Extract<
  MessageKey,
  | 'settings.preferences.theme'
  | 'settings.preferences.defaultTimeRange'
  | 'settings.preferences.refreshInterval'
  | 'settings.preferences.tablePageSize'
  | 'settings.preferences.timeZone'
  | 'settings.preferences.language'
>

interface PreferenceFieldProps {
  name: PreferenceKey
  children: (ids: { control: string; help: string }) => ReactNode
}

/** One labelled control with its helper text, read by screen readers as its description. */
function PreferenceField({ name, children }: PreferenceFieldProps) {
  const t = useT()
  const id = useId().replaceAll(':', '')
  const ids = { control: `${id}-control`, help: `${id}-help` }

  return (
    <div className="grid gap-1.5 min-[1600px]:grid-cols-[14rem_18rem_1fr] min-[1600px]:items-start min-[1600px]:gap-4">
      <label className="text-foreground pt-2 text-sm font-medium" htmlFor={ids.control}>
        {t(name)}
      </label>
      {children(ids)}
      <p className="text-muted-foreground text-sm leading-relaxed min-[1600px]:pt-2" id={ids.help}>
        {t(`${name}.help`)}
      </p>
    </div>
  )
}

/**
 * The Preferences tab (11.2): console preferences of this browser. A change
 * applies at once in every open page and is stored best effort; nothing is
 * sent to the server.
 */
export function PreferencesPanel() {
  const preferences = usePreferences()
  const { setTheme } = useTheme()
  const t = useT()
  // Re-render when the server default language arrives, so its option names it.
  useServerLocale()
  // Re-render when the server default arrives, so its option names the zone.
  useDisplayTimeZone()
  const isDefault = (
    Object.keys(DEFAULT_PREFERENCES) as (keyof typeof DEFAULT_PREFERENCES)[]
  ).every((key) => preferences[key] === DEFAULT_PREFERENCES[key])

  return (
    <Section
      actions={
        <>
          <Button
            disabled={isDefault}
            // The theme follows through `useTheme`, which applies the stored theme on change.
            onClick={() => resetPreferences()}
            size="sm"
            type="button"
            variant="outline"
          >
            {t('settings.preferences.reset')}
          </Button>
          <HelpLink anchor="settings#preferences" label={t('settings.preferences.title')} />
        </>
      }
      captionId="settings.preferences"
      id="settings-preferences"
      title={t('settings.preferences.title')}
    >
      <div className="space-y-5 px-5 py-4 min-[1600px]:space-y-3">
        <PreferenceField name="settings.preferences.theme">
          {(ids) => (
            <NativeSelect
              aria-describedby={ids.help}
              className="w-full"
              id={ids.control}
              onChange={(event) => setTheme(event.target.value as ThemePreference)}
              value={preferences.theme}
            >
              {THEME_OPTIONS.map((theme) => (
                <option key={theme} value={theme}>
                  {t(`settings.preferences.theme.${theme}`)}
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <PreferenceField name="settings.preferences.defaultTimeRange">
          {(ids) => (
            <NativeSelect
              aria-describedby={ids.help}
              className="w-full"
              id={ids.control}
              onChange={(event) =>
                updatePreferences({ defaultTimeRange: event.target.value as DefaultTimeRange })
              }
              value={preferences.defaultTimeRange}
            >
              {DEFAULT_TIME_RANGES.map((range) => (
                <option key={range} value={range}>
                  {t(rangeLabelKey({ kind: 'relative', preset: range }))}
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <PreferenceField name="settings.preferences.refreshInterval">
          {(ids) => (
            <NativeSelect
              aria-describedby={ids.help}
              className="w-full"
              id={ids.control}
              onChange={(event) =>
                updatePreferences({
                  refreshInterval: event.target.value as RefreshIntervalPreference,
                })
              }
              value={preferences.refreshInterval}
            >
              {REFRESH_INTERVALS.map((interval) => (
                <option key={interval} value={interval}>
                  {refreshLabel(t, interval)}
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <PreferenceField name="settings.preferences.tablePageSize">
          {(ids) => (
            <NativeSelect
              aria-describedby={ids.help}
              className="w-full"
              id={ids.control}
              onChange={(event) => {
                const size = TABLE_PAGE_SIZES.find((entry) => String(entry) === event.target.value)
                if (size !== undefined) updatePreferences({ tablePageSize: size })
              }}
              value={preferences.tablePageSize}
            >
              {TABLE_PAGE_SIZES.map((size) => (
                <option key={size} value={size}>
                  {t('settings.preferences.tablePageSize.rows', { size })}
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <PreferenceField name="settings.preferences.timeZone">
          {(ids) => (
            <NativeSelect
              aria-describedby={ids.help}
              className="w-full"
              id={ids.control}
              onChange={(event) =>
                updatePreferences({ timeZone: event.target.value as TimeZonePreference })
              }
              value={preferences.timeZone}
            >
              {TIME_ZONE_PREFERENCES.map((choice) => (
                <option key={choice} value={choice}>
                  {timeZoneLabel(t, choice)}
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <PreferenceField name="settings.preferences.language">
          {(ids) => (
            <NativeSelect
              aria-describedby={ids.help}
              className="w-full"
              id={ids.control}
              onChange={(event) => updatePreferences({ language: event.target.value })}
              value={preferences.language}
            >
              {languagePreferences().map((choice) => (
                <option key={choice} value={choice}>
                  {languageLabel(t, choice)}
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <p className="text-muted-foreground text-xs">{t('settings.preferences.storedLocally')}</p>
      </div>
    </Section>
  )
}
