import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'

import { Badge } from '@/components/ui/badge'
import { useT, type Translate } from '@/i18n/react'
import { formatDecimal, formatPostgresVersion } from '@/lib/format'
import type { ReplicationNode } from '@/lib/replication'

export type TopologyNodeData = ReplicationNode &
  Record<string, unknown> & {
    replayLagSec?: number | null
  }

export type TopologyFlowNode = Node<TopologyNodeData, 'topology'>

function formatReplayLag(t: Translate, seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined) return t('shell.topology.unknown')
  return `${formatDecimal(seconds, 3)} s`
}

function roleLabel(t: Translate, role: string): string {
  switch (role) {
    case 'primary':
      return t('shell.topology.role.primary')
    case 'standby':
      return t('shell.topology.role.standby')
    case 'unresolved':
      return t('shell.topology.role.unresolved')
    case 'unknown':
      return t('shell.topology.role.unknown')
    default:
      return role
  }
}

/** The deliberately information-dense node used by the replication topology. */
export function TopologyNode({ data }: NodeProps<TopologyFlowNode>) {
  const t = useT()
  const role = data.unresolved ? 'unresolved' : (data.role ?? 'unknown')
  const status = data.up === false ? 'down' : data.up === true ? 'up' : 'unknown'
  const state = t(`shell.topology.state.${status}`)
  const unknown = t('shell.topology.unknown')
  const address =
    data.address ??
    t('shell.topology.unresolved', { endpoint: data.id.replace(/^unresolved:/, '') })
  // A named instance shows its name in place of the address (D15); the port stays below.
  const label = data.name ?? address
  const isDown = data.up === false

  return (
    <div
      aria-label={t('shell.topology.node', {
        role: roleLabel(t, role),
        name: data.name ? `${data.name} (${address})` : address,
        state,
      })}
      className={`bg-background min-w-56 rounded-lg border-2 p-3 text-left shadow-sm ${
        isDown ? 'border-destructive bg-destructive/10' : 'border-border'
      }`}
      data-status={status}
      data-topology-node={data.id}
    >
      <Handle aria-hidden="true" position={Position.Top} type="target" />
      <div className="flex items-center justify-between gap-2">
        <Badge variant={role === 'primary' ? 'default' : 'secondary'}>{roleLabel(t, role)}</Badge>
        <strong className={isDown ? 'text-destructive' : undefined}>{state}</strong>
      </div>
      {/* A name of up to 64 characters stays one line inside the node (NODE_X_SPACING 280). */}
      <p
        className={data.name ? 'mt-2 max-w-56 truncate font-medium' : 'mt-2 font-medium break-all'}
        {...(data.name ? { title: `${data.name} (${address})` } : {})}
      >
        {label}
      </p>
      <dl className="text-muted-foreground mt-2 grid grid-cols-[auto_1fr] gap-x-2 text-xs">
        <dt>{t('shell.topology.port')}</dt>
        <dd>{data.port ?? unknown}</dd>
        <dt>{t('shell.topology.version')}</dt>
        <dd>{data.version == null ? unknown : formatPostgresVersion(data.version)}</dd>
        <dt>{t('shell.topology.tier')}</dt>
        <dd>{data.tier ?? unknown}</dd>
        {role === 'standby' ? (
          <>
            <dt>{t('shell.topology.replayLag')}</dt>
            <dd>{formatReplayLag(t, data.replayLagSec)}</dd>
          </>
        ) : null}
      </dl>
      <Handle aria-hidden="true" position={Position.Bottom} type="source" />
    </div>
  )
}
