import { useId, type ReactNode } from 'react'

import { Section } from '@/components/layout/Section'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/native-select'
import { usePreferences } from '@/hooks/usePreferences'
import { useTheme } from '@/hooks/useTheme'
import {
  DEFAULT_PREFERENCES,
  DEFAULT_TIME_RANGES,
  REFRESH_INTERVALS,
  resetPreferences,
  TABLE_PAGE_SIZES,
  updatePreferences,
  type DefaultTimeRange,
  type RefreshIntervalPreference,
  type ThemePreference,
} from '@/lib/preferences'
import { RANGE_PRESETS } from '@/lib/timerange'

const THEME_OPTIONS: readonly { value: ThemePreference; label: string }[] = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'system', label: 'System' },
]

const REFRESH_LABELS: Record<RefreshIntervalPreference, string> = {
  auto: 'Automatic (each page’s own interval)',
  off: 'Off',
  '15s': 'Every 15 seconds',
  '30s': 'Every 30 seconds',
  '60s': 'Every 60 seconds',
}

/** What each control changes (I-16). */
const PREFERENCE_HELP = {
  theme:
    'Colours of the console in this browser. System follows the operating system. The theme button in the header changes the same preference.',
  defaultTimeRange:
    'The time range pages open with when the address names none. The range picker in the header and a range in a link still choose another one.',
  refreshInterval:
    'How often pages reload their data. Automatic keeps each page’s own interval. A chosen interval, or Off, applies to the pages that reload every 15 seconds or slower and moves their stale-data warning with it; Off stops them and hides the warning. Locks, metric charts, events and running commands keep their faster reload.',
  tablePageSize:
    'Rows per page of the tables when the address sets no page size. Rows per page under a table still changes that table only.',
} as const

interface PreferenceFieldProps {
  label: string
  help: string
  children: (ids: { control: string; help: string }) => ReactNode
}

/** One labelled control with its helper text, read by screen readers as its description. */
function PreferenceField({ label, help, children }: PreferenceFieldProps) {
  const id = useId().replaceAll(':', '')
  const ids = { control: `${id}-control`, help: `${id}-help` }

  return (
    <div className="grid gap-1.5 min-[1600px]:grid-cols-[14rem_18rem_1fr] min-[1600px]:items-start min-[1600px]:gap-4">
      <label className="text-foreground pt-2 text-sm font-medium" htmlFor={ids.control}>
        {label}
      </label>
      {children(ids)}
      <p className="text-muted-foreground text-sm leading-relaxed min-[1600px]:pt-2" id={ids.help}>
        {help}
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
  const isDefault = (
    Object.keys(DEFAULT_PREFERENCES) as (keyof typeof DEFAULT_PREFERENCES)[]
  ).every((key) => preferences[key] === DEFAULT_PREFERENCES[key])

  return (
    <Section
      actions={
        <Button
          disabled={isDefault}
          // The theme follows through `useTheme`, which applies the stored theme on change.
          onClick={() => resetPreferences()}
          size="sm"
          type="button"
          variant="outline"
        >
          Reset to defaults
        </Button>
      }
      captionId="settings.preferences"
      id="settings-preferences"
      title="Preferences"
    >
      <div className="space-y-5 px-5 py-4 min-[1600px]:space-y-3">
        <PreferenceField help={PREFERENCE_HELP.theme} label="Theme">
          {(ids) => (
            <NativeSelect
              aria-describedby={ids.help}
              className="w-full"
              id={ids.control}
              onChange={(event) => setTheme(event.target.value as ThemePreference)}
              value={preferences.theme}
            >
              {THEME_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <PreferenceField help={PREFERENCE_HELP.defaultTimeRange} label="Default time range">
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
                  {RANGE_PRESETS[range].label}
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <PreferenceField help={PREFERENCE_HELP.refreshInterval} label="Refresh interval">
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
                  {REFRESH_LABELS[interval]}
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <PreferenceField help={PREFERENCE_HELP.tablePageSize} label="Table page size">
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
                  {size} rows
                </option>
              ))}
            </NativeSelect>
          )}
        </PreferenceField>
        <p className="text-muted-foreground text-xs">
          Kept in this browser only; other browsers and other users keep their own preferences.
        </p>
      </div>
    </Section>
  )
}
