import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import type { Schemas } from '@/api/types'
import { Section } from '@/components/layout/Section'
import { EmptyState } from '@/components/state'
import { partitionDatabases, defaultDatabase, type DatabaseEntry } from '@/lib/databases'

interface DatabaseSelectorProps {
  databases: readonly Schemas['Database'][]
  notMonitoredCount: number
}

function formatDatabaseCount(count: number): string {
  return `${count} database${count === 1 ? '' : 's'}`
}

export function DatabaseSelector({ databases, notMonitoredCount }: DatabaseSelectorProps) {
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
    <Section
      title="Database scope"
      description="Per-database metrics are shown for one monitored database at a time."
    >
      {partition.monitored.length > 0 ? (
        <div className="max-w-xl">
          <label className="text-sm font-medium" htmlFor="instance-database-selector">
            Database
          </label>
          <select
            aria-describedby="instance-database-help"
            id="instance-database-selector"
            onChange={(event) => updateDatabase(event.target.value)}
            value={selectedDatabase}
          >
            {partition.monitored.map((database: DatabaseEntry) => (
              <option key={database.datname} value={database.datname}>
                {database.datname}
              </option>
            ))}
          </select>
          <p className="text-muted-foreground mt-1 text-xs" id="instance-database-help">
            The default is the most active monitored database when activity is available.
          </p>
        </div>
      ) : (
        <EmptyState
          title="No monitored databases"
          description="This instance has no database available for per-database metrics."
        />
      )}

      {displayedNotMonitoredCount > 0 ? (
        <aside className="border-warning/40 bg-warning/10 text-sm" role="status">
          <strong>{formatDatabaseCount(displayedNotMonitoredCount)} not monitored.</strong> Hidden
          databases are a blind spot in this instance’s metrics.
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
            Review the <Link to="/README.md#agent-configuration">databases.max</Link> budget and
            <Link className="ml-1" to="/README.md#agent-configuration">
              include
            </Link>{' '}
            filter configuration.
          </p>
        </aside>
      ) : null}
    </Section>
  )
}
