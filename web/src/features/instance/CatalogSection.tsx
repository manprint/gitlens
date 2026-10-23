import { useMemo, useState } from 'react'

import { useInstanceTriggers } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { DataTable, type DataTableProps } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { EmptyState, ErrorState } from '@/components/state'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatTimestamp } from '@/lib/format'

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

export function CatalogSection({ instanceId }: CatalogSectionProps) {
  const triggersQuery = useInstanceTriggers(instanceId)
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
            Triggers{' '}
            {disabledTriggers > 0 ? (
              <Badge variant="secondary" aria-label={`${disabledTriggers} disabled`}>
                {disabledTriggers}
              </Badge>
            ) : null}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="triggers">
          {triggersQuery.error && !triggersQuery.data ? (
            <ErrorState
              endpoint="instance triggers"
              failure={triggersQuery.error}
              onRetry={() => void triggersQuery.refetch()}
            />
          ) : triggersQuery.isPending || triggersQuery.data === undefined ? (
            <section aria-busy="true" aria-label="Loading triggers" role="status">
              Loading triggers…
            </section>
          ) : (
            <TriggersTab response={triggersQuery.data} />
          )}
        </TabsContent>
      </Tabs>
    </Section>
  )
}
