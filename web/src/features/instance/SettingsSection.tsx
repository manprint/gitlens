import { displayTimeZone, fromZonedInput, toZonedInput } from '@/lib/timezone'
import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

import { useInstanceSettings } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { HelpLink } from '@/components/layout/HelpLink'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { EmptyState, ErrorState, Unknown } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import { usePreferences } from '@/hooks/usePreferences'
import type { MessageKey } from '@/i18n/messages/en'
import { useT, type Translate } from '@/i18n/react'
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

import { ColumnHint } from '@/components/layout/InfoHint'
import type { ColumnHelpId } from '@/lib/captions'

import { CaptionedBlock, InstancePanel } from './InstancePanel'

interface SettingsSectionProps {
  instanceId: string
}

/** The wall clock in the console's zone; the URL keeps UTC. */
function toLocalDateTime(value: string): string {
  const ms = Date.parse(value)
  return Number.isFinite(ms) ? toZonedInput(ms, displayTimeZone()) : ''
}

function DisplayValue({ setting }: { setting: Schemas['Setting'] }) {
  const t = useT()
  const displayValue = settingDisplayValue(setting)
  if (displayValue === null) return <Unknown />

  if (isRedactedArchiveCommand(setting)) {
    return <span title={t('instance.settings.argsWithheld')}>{displayValue}</span>
  }

  return displayValue
}

const DURABILITY_HELP: Record<(typeof DURABILITY_SETTING_NAMES)[number], ColumnHelpId> = {
  fsync: 'instance.durability-fsync',
  full_page_writes: 'instance.durability-full-page-writes',
  archive_mode: 'instance.durability-archive-mode',
  wal_level: 'instance.durability-wal-level',
  synchronous_commit: 'instance.durability-synchronous-commit',
}

const SEVERITY_KEYS: Record<'critical' | 'warning', MessageKey> = {
  critical: 'instance.settings.severity.critical',
  warning: 'instance.settings.severity.warning',
}

function DurabilitySummary({ settings }: { settings: Schemas['Setting'][] }) {
  const t = useT()
  const byName = useMemo(
    () => new Map(settings.map((setting) => [setting.name, setting])),
    [settings],
  )

  // From 1600 px the summary is one line in the panel heading (D26).
  return (
    <div>
      <h3 className="font-medium min-[1600px]:sr-only">
        {t('instance.settings.durabilitySummary')}
      </h3>
      <dl className="mt-3 grid gap-3 min-[1600px]:mt-0 min-[1600px]:flex min-[1600px]:gap-x-3 sm:grid-cols-2 lg:grid-cols-5">
        {DURABILITY_SETTING_NAMES.map((name) => {
          const setting = byName.get(name)
          const severity = setting ? durabilitySeverity(setting) : null
          return (
            <div
              key={name}
              className="border-muted bg-muted/20 p-3 min-[1600px]:flex min-[1600px]:items-center min-[1600px]:gap-1.5 min-[1600px]:rounded-md min-[1600px]:px-2 min-[1600px]:py-1 min-[1600px]:whitespace-nowrap"
            >
              <dt className="text-muted-foreground flex items-center gap-0.5 text-xs">
                {name}
                <ColumnHint id={DURABILITY_HELP[name]} label={name} />
              </dt>
              <dd className="mt-1 flex items-center gap-2 font-medium min-[1600px]:mt-0 min-[1600px]:gap-1.5">
                {setting ? <DisplayValue setting={setting} /> : <Unknown />}
                {severity ? (
                  <Badge
                    variant={severity === 'critical' ? 'destructive' : 'secondary'}
                    aria-label={t('instance.settings.severityAria', {
                      severity: t(SEVERITY_KEYS[severity]),
                    })}
                  >
                    {t(SEVERITY_KEYS[severity])}
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

function settingToolbar(t: Translate): TableToolbarSpec<Setting> {
  return {
    searchLabel: t('instance.settings.search'),
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
        label: t('instance.settings.col.source'),
        kind: 'select',
        accessor: (setting) => setting.source || null,
      },
      {
        id: 'state',
        label: t('instance.settings.col.state'),
        kind: 'select',
        options: [
          { value: 'pending', label: t('instance.settings.state.pending') },
          { value: 'in-force', label: t('instance.settings.state.inForce') },
        ],
        accessor: (setting) => (isPendingRestart(setting) ? 'pending' : 'in-force'),
      },
    ],
  }
}

function settingColumns(t: Translate): readonly SimpleColumn<Setting>[] {
  return [
    {
      id: 'name',
      header: t('instance.settings.col.name'),
      rowHeader: true,
      sortValue: (setting) => setting.name,
      cell: (setting) => setting.name,
      width: '18rem',
      truncate: (setting) => setting.name,
    },
    {
      id: 'value',
      header: t('instance.settings.col.value'),
      cell: (setting) => <DisplayValue setting={setting} />,
      truncate: (setting) => settingDisplayValue(setting) ?? '',
    },
    {
      id: 'unit',
      header: t('instance.settings.col.unit'),
      width: '5rem',
      cell: (setting) =>
        setting.unit || <Unknown reason={t('instance.settings.unitNotReported')} />,
    },
    {
      id: 'source',
      header: t('instance.settings.col.source'),
      help: 'instance.setting-source',
      sortValue: (setting) => setting.source || null,
      cell: (setting) =>
        setting.source || <Unknown reason={t('instance.settings.sourceNotReported')} />,
      width: '9rem',
      truncate: (setting) => setting.source,
    },
    {
      id: 'context',
      header: t('instance.settings.col.context'),
      help: 'instance.setting-context',
      sortValue: (setting) => setting.context || null,
      cell: (setting) =>
        setting.context || <Unknown reason={t('instance.settings.contextNotReported')} />,
      width: '9rem',
      truncate: (setting) => setting.context,
    },
    {
      id: 'changed',
      header: t('instance.settings.col.changedAt'),
      help: 'instance.setting-changed',
      sortValue: (setting) => Date.parse(setting.changed_at),
      cell: (setting) => formatTimestamp(setting.changed_at),
      width: '13rem',
    },
    {
      id: 'state',
      header: t('instance.settings.col.state'),
      help: 'instance.setting-state',
      width: '9rem',
      cell: (setting) => {
        const pending = isPendingRestart(setting)
        return (
          <Badge variant={pending ? 'destructive' : 'secondary'}>
            {pending ? t('instance.settings.state.pending') : t('instance.settings.state.inForce')}
          </Badge>
        )
      },
    },
  ]
}

function SettingTable({ settings }: { settings: Setting[] }) {
  const t = useT()
  const columns = useMemo(() => settingColumns(t), [t])
  const toolbar = useMemo(() => settingToolbar(t), [t])
  return (
    <SimpleTable
      ariaLabel={t('instance.settings.observed')}
      captionId="instance.settings-changes"
      columns={columns}
      emptyState={null}
      rowKey={(setting) => `${setting.name}-${setting.changed_at}`}
      rows={settings}
      scope={t('instance.settings.noun.settings')}
      tableId="pg-settings"
      toolbar={toolbar}
    />
  )
}

export function SettingsSection({ instanceId }: SettingsSectionProps) {
  const t = useT()
  const [searchParams, setSearchParams] = useSearchParams()
  // The header's range picker and this section read the same default (11.2).
  const { defaultTimeRange } = usePreferences()
  const range = useMemo(
    () => parseRange(searchParams, new Date(), defaultTimeRange),
    [defaultTimeRange, searchParams],
  )
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
        const since = fromZonedInput(value, displayTimeZone())
        if (since) next.set('changed_since', since.toISOString())
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
    <InstancePanel
      id="settings"
      title={t('instance.settings.section')}
      captionId="instance.settings"
      actions={
        <>
          {query.data ? (
            <>
              <DurabilitySummary settings={query.data.settings} />
              {/* From 1600 px the controls share the heading line; the input keeps its name. */}
              <div className="flex flex-wrap items-end gap-4 min-[1600px]:flex-nowrap min-[1600px]:items-center min-[1600px]:gap-2">
                <label className="grid gap-1 text-sm min-[1600px]:flex min-[1600px]:items-center">
                  <span>{t('instance.settings.changedSince')}</span>
                  <Input
                    className="w-auto min-[1600px]:h-8"
                    aria-label={t('instance.settings.changedSince')}
                    type="datetime-local"
                    value={toLocalDateTime(changedSince)}
                    onChange={(event) => updateChangedSince(event.target.value)}
                  />
                </label>
                <Button size="sm" variant="outline" type="button" onClick={resetChangedSince}>
                  {t('instance.settings.useRange')}
                </Button>
              </div>
            </>
          ) : null}
          <HelpLink anchor="instance-overview#settings" label={t('instance.settings.section')} />
        </>
      }
    >
      {query.error && !query.data ? (
        <ErrorState
          endpoint="instance settings"
          failure={query.error}
          onRetry={() => void query.refetch()}
        />
      ) : query.isPending || query.data === undefined ? (
        <section aria-busy="true" aria-label={t('instance.settings.loadingAria')} role="status">
          {t('instance.settings.loading')}
        </section>
      ) : changedSettings.length === 0 ? (
        <CaptionedBlock
          ariaLabel={t('instance.settings.observed')}
          captionId="instance.settings-changes"
        >
          <EmptyState
            title={t('instance.settings.noChanges')}
            description={t('instance.settings.noChangesDescription')}
          />
        </CaptionedBlock>
      ) : (
        <SettingTable settings={changedSettings} />
      )}
    </InstancePanel>
  )
}
