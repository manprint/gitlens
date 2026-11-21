import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

import { useT } from '@/i18n/react'
import type { Schemas } from '@/api/types'
import { Section } from '@/components/layout/Section'
import { EmptyState } from '@/components/state'
import { partitionDatabases, defaultDatabase, type DatabaseEntry } from '@/lib/databases'
import { NativeSelect } from '@/components/ui/native-select'
import { Callout } from '@/components/ui/callout'

interface DatabaseSelectorProps {
  databases: readonly Schemas['Database'][]
  notMonitoredCount: number
}

export function DatabaseSelector({ databases, notMonitoredCount }: DatabaseSelectorProps) {
  const t = useT()
  const [searchParams, setSearchParams] = useSearchParams()
  const partition = useMemo(() => partitionDatabases(databases), [databases])
  const fallbackDatabase = defaultDatabase(partition.monitored)
  const requestedDatabase = searchParams.get('db')
  const selectedDatabase = partition.monitored.some(
    (database) => database.datname === requestedDatabase,
  )
    ? (requestedDatabase ?? '')
    : (fallbackDatabase?.datname ?? '')
  const skippedCount = [...partition.skipped.values()].reduce(
    (total, names) => total + names.length,
    0,
  )
  const displayedNotMonitoredCount = Math.max(notMonitoredCount, skippedCount)

  function updateDatabase(datname: string) {
    const next = new URLSearchParams(searchParams)
    if (datname) next.set('db', datname)
    else next.delete('db')
    setSearchParams(next)
  }

  return (
    <Section title={t('instance.dbsel.section')} captionId="instance.database-scope">
      {partition.monitored.length > 0 ? (
        <div className="mb-4 flex max-w-xl flex-col gap-1.5">
          <label className="text-sm font-medium" htmlFor="instance-database-selector">
            {t('instance.dbsel.label')}
          </label>
          <NativeSelect
            aria-describedby="instance-database-help"
            className="w-64 max-w-full"
            id="instance-database-selector"
            onChange={(event) => updateDatabase(event.target.value)}
            value={selectedDatabase}
          >
            {partition.monitored.map((database: DatabaseEntry) => (
              <option key={database.datname} value={database.datname}>
                {database.datname}
              </option>
            ))}
          </NativeSelect>
          <p className="text-muted-foreground text-xs" id="instance-database-help">
            {t('instance.dbsel.default')}
          </p>
        </div>
      ) : (
        <EmptyState
          title={t('instance.dbsel.noMonitored')}
          description={t('instance.dbsel.noMonitoredDescription')}
        />
      )}

      {displayedNotMonitoredCount > 0 ? (
        <Callout role="status" variant="warning">
          <strong>
            {t('instance.dbsel.countNotMonitored', { count: displayedNotMonitoredCount })}
          </strong>{' '}
          {t('instance.dbsel.blindSpot')}
          {[...partition.skipped.entries()].length > 0 ? (
            <ul className="mt-2 list-disc pl-5">
              {[...partition.skipped.entries()].map(([reason, names]) => (
                <li key={reason}>
                  <code>{reason}</code>: {names.join(', ')}
                </li>
              ))}
            </ul>
          ) : null}
          <p className="mt-2">
            {/* The README is a static file: a router link would render the not-found page. */}
            {t('instance.dbsel.reviewBefore')}{' '}
            <a href="/README.md#agent-configuration">databases.max</a>{' '}
            {t('instance.dbsel.reviewMiddle')}
            <a className="ml-1" href="/README.md#agent-configuration">
              {t('instance.dbsel.includeLink')}
            </a>{' '}
            {t('instance.dbsel.reviewAfter')}
          </p>
        </Callout>
      ) : null}
    </Section>
  )
}
