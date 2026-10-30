import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

import { useInstanceSettings } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState, Unknown } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { formatTimestamp } from '@/lib/format'
import { parseRange } from '@/lib/timerange'

import {
  DURABILITY_SETTING_NAMES,
  durabilitySeverity,
  isPendingRestart,
  isRedactedArchiveCommand,
  settingDisplayValue,
} from '@/lib/settings'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

interface SettingsSectionProps {
  instanceId: string
}

function toLocalDateTime(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return date.toISOString().slice(0, 16)
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

type Setting = Schemas['Setting']

const SETTING_TOOLBAR: TableToolbarSpec<Setting> = {
  searchLabel: 'Search settings',
  searchFields: [
    (setting) => setting.name,
    (setting) => setting.value,
    (setting) => setting.unit,
    (setting) => setting.source,
    (setting) => setting.context,
  ],
  filters: [
    {
      id: 'source',
      label: 'Source',
      kind: 'select',
      accessor: (setting) => setting.source || null,
    },
    {
      id: 'state',
      label: 'State',
      kind: 'select',
      options: [
        { value: 'pending', label: 'Pending restart' },
        { value: 'in-force', label: 'In force' },
      ],
      accessor: (setting) => (isPendingRestart(setting) ? 'pending' : 'in-force'),
    },
  ],
}

const SETTING_COLUMNS: readonly SimpleColumn<Setting>[] = [
  {
    id: 'name',
    header: 'Name',
    rowHeader: true,
    sortValue: (setting) => setting.name,
    cell: (setting) => setting.name,
  },
  { id: 'value', header: 'Value', cell: (setting) => <DisplayValue setting={setting} /> },
  {
    id: 'unit',
    header: 'Unit',
    cell: (setting) => setting.unit || <Unknown reason="Unit was not reported." />,
  },
  {
    id: 'source',
    header: 'Source',
    sortValue: (setting) => setting.source || null,
    cell: (setting) => setting.source || <Unknown reason="Source was not reported." />,
  },
  {
    id: 'context',
    header: 'Context',
    sortValue: (setting) => setting.context || null,
    cell: (setting) => setting.context || <Unknown reason="Context was not reported." />,
  },
  {
    id: 'changed',
    header: 'Changed at',
    sortValue: (setting) => Date.parse(setting.changed_at),
    cell: (setting) => formatTimestamp(setting.changed_at, 'UTC'),
  },
  {
    id: 'state',
    header: 'State',
    cell: (setting) => {
      const pending = isPendingRestart(setting)
      return (
        <Badge variant={pending ? 'destructive' : 'secondary'}>
          {pending ? 'Pending restart' : 'In force'}
        </Badge>
      )
    },
  },
]

function SettingTable({ settings }: { settings: Setting[] }) {
  return (
    <SimpleTable
      ariaLabel="Observed PostgreSQL settings"
      columns={SETTING_COLUMNS}
      emptyState={null}
      rowKey={(setting) => `${setting.name}-${setting.changed_at}`}
      rows={settings}
      scope="settings"
      tableId="pg-settings"
      toolbar={SETTING_TOOLBAR}
    />
  )
}

export function SettingsSection({ instanceId }: SettingsSectionProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const range = useMemo(() => parseRange(searchParams, new Date()), [searchParams])
  const changedSince = searchParams.get('changed_since') ?? range.from.toISOString()
  // The latest scrape is fetched once, unfiltered. The change table is narrowed
  // here, so the durability posture is never hidden by the selected range.
  const query = useInstanceSettings(instanceId)
  const changedSettings = useMemo(() => {
    const since = Date.parse(changedSince)
    return (query.data?.settings ?? []).filter((setting) => Date.parse(setting.changed_at) >= since)
  }, [changedSince, query.data?.settings])

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
      description="Observed PostgreSQL settings and restart state. The durability posture is the latest scrape; the change table is narrowed to the selected time range."
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
              <span>Changed since</span>
              <Input
                className="w-auto"
                aria-label="Changed since"
                type="datetime-local"
                value={toLocalDateTime(changedSince)}
                onChange={(event) => updateChangedSince(event.target.value)}
              />
            </label>
            <Button size="sm" variant="outline" type="button" onClick={resetChangedSince}>
              Use selected range
            </Button>
          </div>
          <DurabilitySummary settings={query.data.settings} />
          {changedSettings.length === 0 ? (
            <EmptyState
              title="No setting changes"
              description="No observed setting changes fall in the selected time range."
            />
          ) : (
            <SettingTable settings={changedSettings} />
          )}
        </div>
      )}
    </Section>
  )
}
