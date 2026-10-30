import { useMemo } from 'react'

import { useRenameCluster, useRenameInstance } from '@/api/queries'
import type { Schemas } from '@/api/types'
import { DataTable, type DataTableToolbar } from '@/components/layout/DataTable'
import { Section } from '@/components/layout/Section'
import { EmptyState } from '@/components/state'
import { clusterLabel } from '@/lib/names'

import { nameRows, type NameRow } from './nameRows'
import { RenameDialog } from './RenameDialog'

type Cluster = Schemas['Cluster']
type InstanceSummary = Schemas['InstanceSummary']

const NAMES_TOOLBAR: DataTableToolbar<NameRow> = {
  searchFields: [
    (row) => row.cluster_name,
    (row) => row.cluster_id,
    (row) => row.instance_name,
    (row) => row.address,
    (row) => row.role,
  ],
  filters: [
    {
      id: 'kind',
      label: 'Row',
      kind: 'select',
      options: [
        { value: 'cluster', label: 'Clusters' },
        { value: 'instance', label: 'Instances' },
      ],
      accessor: (row) => row.kind,
    },
  ],
}

/** A name, or the default label the console shows in its place, marked as such. */
function NameCell({ name, fallback }: { name: string | null; fallback: string }) {
  if (name !== null) return <span title={name}>{name}</span>
  return (
    <span className="text-muted-foreground" title={`No name: shown as ${fallback}`}>
      {fallback}
      <span className="sr-only"> (default)</span>
    </span>
  )
}

function ClusterRename({ row }: { row: NameRow & { cluster_id: string } }) {
  const rename = useRenameCluster()
  return (
    <RenameDialog
      current={row.cluster_name}
      fallback={clusterLabel({ cluster_id: row.cluster_id, name: null })}
      kind="cluster"
      onSave={(name) => rename.mutateAsync({ clusterId: row.cluster_id, name })}
      subject={row.cluster_label}
    />
  )
}

function InstanceRename({ row }: { row: NameRow & { instance_id: string; address: string } }) {
  const rename = useRenameInstance()
  return (
    <RenameDialog
      current={row.instance_name}
      fallback={row.address}
      kind="instance"
      onSave={(displayName) => rename.mutateAsync({ instanceId: row.instance_id, displayName })}
      subject={row.instance_label ?? row.address}
    />
  )
}

function RenameCell({ row }: { row: NameRow }) {
  if (row.kind === 'cluster' && row.cluster_id !== null) {
    return <ClusterRename row={{ ...row, cluster_id: row.cluster_id }} />
  }
  if (row.kind === 'instance' && row.instance_id !== null && row.address !== null) {
    return <InstanceRename row={{ ...row, instance_id: row.instance_id, address: row.address }} />
  }
  return null
}

/** A typed cell title for `meta.title`, which sees the row as unknown. */
function rowTitle(title: (row: NameRow) => string): (row: unknown) => string {
  return (row) => title(row as NameRow)
}

function nameColumns() {
  return [
    {
      accessorKey: 'cluster_label',
      header: 'Cluster name',
      meta: {
        width: '16rem',
        truncate: true,
        title: rowTitle((row) => (row.kind === 'cluster' ? row.cluster_label : '')),
      },
      cell: ({ row }: { row: { original: NameRow } }) =>
        row.original.kind === 'cluster' ? (
          <NameCell fallback={row.original.cluster_label} name={row.original.cluster_name} />
        ) : null,
    },
    {
      accessorKey: 'cluster_id',
      header: 'Cluster id',
      meta: {
        width: '13rem',
        truncate: true,
        title: rowTitle((row) => (row.kind === 'cluster' ? (row.cluster_id ?? '') : '')),
      },
      cell: ({ row }: { row: { original: NameRow } }) =>
        row.original.kind === 'cluster' ? (
          <code className="font-mono text-xs">{row.original.cluster_id}</code>
        ) : null,
    },
    {
      accessorKey: 'instance_label',
      header: 'Instance name',
      meta: { width: '16rem', truncate: true, title: rowTitle((row) => row.instance_label ?? '') },
      cell: ({ row }: { row: { original: NameRow } }) =>
        row.original.kind === 'instance' && row.original.address !== null ? (
          <NameCell fallback={row.original.address} name={row.original.instance_name} />
        ) : null,
    },
    {
      accessorKey: 'address',
      header: 'Address',
      meta: { width: '16rem', truncate: true },
      cell: ({ row }: { row: { original: NameRow } }) => row.original.address ?? '',
    },
    {
      accessorKey: 'role',
      header: 'Role',
      meta: { width: '7rem' },
      cell: ({ row }: { row: { original: NameRow } }) => row.original.role ?? '',
    },
    {
      id: 'rename',
      header: 'Rename',
      enableSorting: false,
      enableHiding: false,
      meta: { width: '7rem' },
      cell: ({ row }: { row: { original: NameRow } }) => <RenameCell row={row.original} />,
    },
  ]
}

export interface NamesPanelProps {
  clusters: readonly Cluster[]
  instances: readonly InstanceSummary[]
}

/**
 * Names (D14, D15): rename a cluster or an instance. Mounted in Settings until
 * 11.1 moves it into the `names` tab.
 */
export function NamesPanel({ clusters, instances }: NamesPanelProps) {
  const rows = useMemo(() => nameRows(clusters, instances), [clusters, instances])
  const columns = useMemo(() => nameColumns(), [])

  return (
    <Section captionId="settings.names" title="Names">
      <DataTable
        ariaLabel="Cluster and instance names"
        captionId="settings.names-table"
        columns={columns}
        data={rows}
        // Keyed by cluster or instance, so focus goes back to the right Rename button after a sort.
        getRowId={(row) => row.key}
        emptyState={
          <EmptyState
            description="Clusters and instances appear here once an agent reports them."
            title="Nothing to name yet"
          />
        }
        scope="names"
        tableId="names"
        toolbar={NAMES_TOOLBAR}
        total={rows.length}
      />
    </Section>
  )
}
