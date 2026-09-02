import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { useInstanceSettings } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState, Unknown } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { formatTimestamp } from '@/lib/format'
import { parseRange } from '@/lib/timerange'

import {
  DURABILITY_SETTING_NAMES,
  durabilitySeverity,
  isPendingRestart,
  isRedactedArchiveCommand,
  settingDisplayValue,
} from '@/lib/settings'

interface SettingsSectionProps {
  instanceId: string
}

function toLocalDateTime(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return date.toISOString().slice(0, 16)
}

function settingMatches(setting: Schemas['Setting'], search: string): boolean {
  if (!search.trim()) return true
  const needle = search.trim().toLowerCase()
  return [
    setting.name,
    setting.value ?? '',
    setting.unit,
    setting.source,
    setting.context,
    setting.pending_restart,
  ].some((field) => field.toLowerCase().includes(needle))
}

function DisplayValue({ setting }: { setting: Schemas['Setting'] }) {
  const displayValue = settingDisplayValue(setting)
  if (displayValue === null) return <Unknown />

  if (isRedactedArchiveCommand(setting)) {
    return (
      <span title="Arguments are withheld because they may contain credentials.">
        {displayValue}
      </span>
    )
  }

  return displayValue
}

function DurabilitySummary({ settings }: { settings: Schemas['Setting'][] }) {
  const byName = useMemo(
    () => new Map(settings.map((setting) => [setting.name, setting])),
    [settings],
  )

  return (
    <div>
      <h3 className="font-medium">Durability summary</h3>
      <dl className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        {DURABILITY_SETTING_NAMES.map((name) => {
          const setting = byName.get(name)
          const severity = setting ? durabilitySeverity(setting) : null
          return (
            <div key={name} className="border-muted bg-muted/20 p-3">
              <dt className="text-muted-foreground text-xs">{name}</dt>
              <dd className="mt-1 flex items-center gap-2 font-medium">
                {setting ? <DisplayValue setting={setting} /> : <Unknown />}
                {severity ? (
                  <Badge
                    variant={severity === 'critical' ? 'destructive' : 'secondary'}
                    aria-label={`Durability severity: ${severity}`}
                  >
                    {severity}
                  </Badge>
                ) : null}
              </dd>
            </div>
          )
        })}
      </dl>
    </div>
  )
}

function SettingTable({ settings }: { settings: Schemas['Setting'][] }) {
  return (
    <div
      className="overflow-x-auto"
      role="region"
      aria-label="Observed PostgreSQL settings"
      tabIndex={0}
    >
      <table>
        <caption className="sr-only">Observed PostgreSQL settings</caption>
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">Value</th>
            <th scope="col">Unit</th>
            <th scope="col">Source</th>
            <th scope="col">Context</th>
            <th scope="col">Changed at</th>
            <th scope="col">State</th>
          </tr>
        </thead>
        <tbody>
          {settings.map((setting) => {
            const pending = isPendingRestart(setting)
            return (
              <tr key={`${setting.name}-${setting.changed_at}`}>
                <th scope="row">{setting.name}</th>
                <td>
                  <DisplayValue setting={setting} />
                </td>
                <td>{setting.unit || <Unknown reason="Unit was not reported." />}</td>
                <td>{setting.source || <Unknown reason="Source was not reported." />}</td>
                <td>{setting.context || <Unknown reason="Context was not reported." />}</td>
                <td>{formatTimestamp(setting.changed_at, 'UTC')}</td>
                <td>
                  <Badge variant={pending ? 'destructive' : 'secondary'}>
                    {pending ? 'Pending restart' : 'In force'}
                  </Badge>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

export function SettingsSection({ instanceId }: SettingsSectionProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [search, setSearch] = useState('')
  const range = useMemo(() => parseRange(searchParams, new Date()), [searchParams])
  const changedSince = searchParams.get('changed_since') ?? range.from.toISOString()
  const query = useInstanceSettings(instanceId, { changed_since: changedSince })
  const settings = useMemo(
    () => (query.data?.settings ?? []).filter((setting) => settingMatches(setting, search)),
    [query.data?.settings, search],
  )

  function updateChangedSince(value: string) {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      if (!value) {
        next.delete('changed_since')
      } else {
        next.set('changed_since', new Date(`${value}:00Z`).toISOString())
      }
      return next
    })
  }

  function resetChangedSince() {
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.delete('changed_since')
      return next
    })
  }

  return (
    <Section
      id="settings"
      title="Settings and durability"
      description="Observed PostgreSQL settings, restart state, and the durability posture derived from the selected time range."
    >
      {query.error && !query.data ? (
        <ErrorState
          endpoint="instance settings"
          failure={query.error}
          onRetry={() => void query.refetch()}
        />
      ) : query.isPending || query.data === undefined ? (
        <section aria-busy="true" aria-label="Loading settings" role="status">
          Loading settings…
        </section>
      ) : (
        <div className="space-y-5">
          <div className="flex flex-wrap items-end gap-4">
            <label className="grid gap-1 text-sm">
              <span>Search settings</span>
              <input
                type="search"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder="name, source, or value"
              />
            </label>
            <label className="grid gap-1 text-sm">
              <span>Changed since</span>
              <input
                aria-label="Changed since"
                type="datetime-local"
                value={toLocalDateTime(changedSince)}
                onChange={(event) => updateChangedSince(event.target.value)}
              />
            </label>
            <button type="button" onClick={resetChangedSince}>
              Use selected range
            </button>
          </div>
          {settings.length === 0 ? (
            <EmptyState
              title="No setting changes"
              description="No observed setting changes match the selected time range and search."
            />
          ) : (
            <>
              <DurabilitySummary settings={settings} />
              <SettingTable settings={settings} />
            </>
          )}
        </div>
      )}
    </Section>
  )
}
