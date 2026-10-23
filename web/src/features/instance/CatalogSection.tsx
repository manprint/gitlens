import { useMemo, useState, type ReactNode } from 'react'

import {
  useInstanceTriggers,
  useInstanceUnindexedForeignKeys,
  type ApiQueryResult,
} from '@/api/queries'
import type { Schemas } from '@/api/types'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatBytes, formatTimestamp } from '@/lib/format'

interface CatalogSectionProps {
  instanceId: string
}

type Trigger = Schemas['Trigger']

const selectClassName = 'border-input bg-background h-9 w-full rounded-md border px-3'

function databaseOptions(datnames: readonly string[]): string[] {
  return [...new Set(datnames)].sort((left, right) => left.localeCompare(right))
}

/** A disabled internal trigger means its constraint is no longer enforced. */
function triggerStateBadge(trigger: Trigger) {
  if (trigger.state === 'disabled') {
    return (
      <Badge variant={trigger.kind === 'internal' ? 'destructive' : 'secondary'}>disabled</Badge>
    )
  }
  return <Badge variant="outline">{trigger.state}</Badge>
}

function triggerColumns(): DataTableProps<Trigger>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database' },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (trigger) => `${trigger.schemaname}.${trigger.relname}`,
    },
    { accessorKey: 'name', header: 'Trigger' },
    {
      accessorKey: 'state',
      header: 'State',
      cell: ({ row }) => triggerStateBadge(row.original),
    },
    {
      id: 'enforces',
      header: 'Enforces',
      accessorFn: (trigger) =>
        trigger.kind === 'internal'
          ? `${trigger.constraint_type || 'constraint'} ${trigger.constraint}`.trim()
          : '',
    },
    {
      id: 'fires',
      header: 'Fires',
      accessorFn: (trigger) => `${trigger.timing} ${trigger.events} FOR EACH ${trigger.level}`,
    },
    { accessorKey: 'function', header: 'Function' },
    {
      accessorKey: 'changed_at',
      header: 'State since',
      cell: ({ row }) => formatTimestamp(row.original.changed_at, 'UTC'),
    },
  ]
}

function TriggersTab({ response }: { response: Schemas['TriggersResponse'] }) {
  const [database, setDatabase] = useState('all')
  const [disabledOnly, setDisabledOnly] = useState(false)
  const databases = useMemo(
    () => databaseOptions(response.triggers.map((trigger) => trigger.datname)),
    [response.triggers],
  )
  const rows = useMemo(
    () =>
      response.triggers.filter(
        (trigger) =>
          (database === 'all' || trigger.datname === database) &&
          (!disabledOnly || trigger.state === 'disabled'),
      ),
    [database, disabledOnly, response.triggers],
  )
  const columns = useMemo(() => triggerColumns(), [])
  const truncated = response.truncated_databases.filter(
    (name) => database === 'all' || name === database,
  )
  const emptyState =
    response.triggers.length === 0 ? (
      <EmptyState
        title="No triggers reported"
        description="The trigger inventory found no user triggers and no disabled constraint triggers."
      />
    ) : (
      <EmptyState
        title="No triggers match these filters"
        description="Try another database or show enabled triggers too."
      />
    )

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-3">
        <label className="space-y-1 text-sm" htmlFor="catalog-trigger-database">
          <span className="font-medium">Database</span>
          <select
            className={selectClassName}
            id="catalog-trigger-database"
            onChange={(event) => setDatabase(event.target.value)}
            value={database}
          >
            <option value="all">All databases</option>
            {databases.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center gap-2 self-end text-sm">
          <input
            checked={disabledOnly}
            onChange={(event) => setDisabledOnly(event.target.checked)}
            type="checkbox"
          />
          <span>Disabled only</span>
        </label>
      </div>
      {truncated.length > 0 ? (
        <p role="status" className="text-muted-foreground text-sm">
          {truncated.join(', ')} {truncated.length === 1 ? 'has' : 'have'} more triggers than the
          agent reports: disabled triggers are listed first, enabled ones may be missing.
        </p>
      ) : null}
      <DataTable
        ariaLabel="Triggers"
        columns={columns}
        data={rows}
        emptyState={emptyState}
        scope="triggers"
        total={response.triggers.length}
      />
    </div>
  )
}

function UnindexedForeignKeysTab({
  response,
}: {
  response: Schemas['UnindexedForeignKeysResponse']
}) {
  const [database, setDatabase] = useState('all')
  const databases = useMemo(
    () => databaseOptions(response.foreign_keys.map((fk) => fk.datname)),
    [response.foreign_keys],
  )
  const rows = useMemo(
    () => response.foreign_keys.filter((fk) => database === 'all' || fk.datname === database),
    [database, response.foreign_keys],
  )
  const columns = useMemo(() => foreignKeyColumns(), [])
  const truncated = response.truncated_databases.filter(
    (name) => database === 'all' || name === database,
  )

  return (
    <div className="space-y-4">
      <p className="text-muted-foreground text-sm">
        Deleting a referenced row, or updating its key, scans the whole referencing table when no
        index leads with the foreign key&apos;s columns.
      </p>
      <div className="grid gap-4 sm:grid-cols-3">
        <label className="space-y-1 text-sm" htmlFor="catalog-fk-database">
          <span className="font-medium">Database</span>
          <select
            className={selectClassName}
            id="catalog-fk-database"
            onChange={(event) => setDatabase(event.target.value)}
            value={database}
          >
            <option value="all">All databases</option>
            {databases.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
        </label>
      </div>
      {truncated.length > 0 ? (
        <p role="status" className="text-muted-foreground text-sm">
          {truncated.join(', ')} {truncated.length === 1 ? 'has' : 'have'} more unindexed foreign
          keys than the agent reports: the largest tables are listed.
        </p>
      ) : null}
      <DataTable
        ariaLabel="Unindexed foreign keys"
        columns={columns}
        data={rows}
        emptyState={
          <EmptyState
            title="Every foreign key has an index"
            description="No foreign key without an index leading with its columns was reported."
          />
        }
        scope="foreign keys"
        total={response.foreign_keys.length}
      />
    </div>
  )
}

function foreignKeyColumns(): DataTableProps<Schemas['UnindexedForeignKey']>['columns'] {
  return [
    { accessorKey: 'datname', header: 'Database' },
    {
      id: 'table',
      header: 'Table',
      accessorFn: (fk) => `${fk.schemaname}.${fk.relname}`,
    },
    { accessorKey: 'constraint', header: 'Constraint' },
    { accessorKey: 'columns', header: 'Columns' },
    { accessorKey: 'referenced', header: 'References' },
    {
      accessorKey: 'table_bytes',
      header: 'Table size',
      cell: ({ row }) => formatBytes(row.original.table_bytes),
    },
  ]
}

interface CatalogTabStateProps<T> {
  query: ApiQueryResult<T>
  label: string
  children: (data: T) => ReactNode
}

function CatalogTabState<T>({ query, label, children }: CatalogTabStateProps<T>) {
  if (query.error && !query.data) {
    return (
      <ErrorState
        endpoint={`instance ${label}`}
        failure={query.error}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (query.isPending || query.data === undefined) {
    return (
      <section aria-busy="true" aria-label={`Loading ${label}`} role="status">
        Loading {label}…
      </section>
    )
  }
  return children(query.data)
}

function TabCount({ count, label }: { count: number; label: string }) {
  if (count === 0) return null
  return (
    <Badge variant="secondary" aria-label={`${count} ${label}`}>
      {count}
    </Badge>
  )
}

export function CatalogSection({ instanceId }: CatalogSectionProps) {
  const triggersQuery = useInstanceTriggers(instanceId)
  const foreignKeysQuery = useInstanceUnindexedForeignKeys(instanceId)
  const disabledTriggers =
    triggersQuery.data?.triggers.filter((trigger) => trigger.state === 'disabled').length ?? 0

  return (
    <Section
      id="catalog"
      title="Schema objects"
      description="Objects the catalog checks report for each database, refreshed every few minutes."
    >
      <Tabs defaultValue="triggers">
        <TabsList aria-label="Schema object kinds">
          <TabsTrigger value="triggers">
            Triggers <TabCount count={disabledTriggers} label="disabled" />
          </TabsTrigger>
          <TabsTrigger value="foreign-keys">
            Unindexed foreign keys{' '}
            <TabCount count={foreignKeysQuery.data?.foreign_keys.length ?? 0} label="reported" />
          </TabsTrigger>
        </TabsList>
        <TabsContent value="triggers">
          <CatalogTabState query={triggersQuery} label="triggers">
            {(data) => <TriggersTab response={data} />}
          </CatalogTabState>
        </TabsContent>
        <TabsContent value="foreign-keys">
          <CatalogTabState query={foreignKeysQuery} label="unindexed foreign keys">
            {(data) => <UnindexedForeignKeysTab response={data} />}
          </CatalogTabState>
        </TabsContent>
      </Tabs>
    </Section>
  )
}
