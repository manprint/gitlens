import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'

import { Badge } from '@/components/ui/badge'
import { formatPostgresVersion } from '@/lib/format'
import type { ReplicationNode } from '@/lib/replication'

export type TopologyNodeData = ReplicationNode &
  Record<string, unknown> & {
    replayLagSec?: number | null
  }

export type TopologyFlowNode = Node<TopologyNodeData, 'topology'>

function formatReplayLag(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined) return 'Unknown'
  return `${seconds.toFixed(3)} s`
}

/** The deliberately information-dense node used by the replication topology. */
export function TopologyNode({ data }: NodeProps<TopologyFlowNode>) {
  const role = data.unresolved ? 'unresolved' : (data.role ?? 'unknown')
  const state = data.up === false ? 'Down' : data.up === true ? 'Up' : 'Unknown'
  const address = data.address ?? `Unresolved endpoint ${data.id.replace(/^unresolved:/, '')}`
  // A named instance shows its name in place of the address (D15); the port stays below.
  const label = data.name ?? address
  const isDown = data.up === false

  return (
    <div
      aria-label={`${role} node ${data.name ? `${data.name} (${address})` : address}, ${state}`}
      className={`bg-background min-w-56 rounded-lg border-2 p-3 text-left shadow-sm ${
        isDown ? 'border-destructive bg-destructive/10' : 'border-border'
      }`}
      data-status={state.toLowerCase()}
      data-topology-node={data.id}
    >
      <Handle aria-hidden="true" position={Position.Top} type="target" />
      <div className="flex items-center justify-between gap-2">
        <Badge variant={role === 'primary' ? 'default' : 'secondary'}>{role}</Badge>
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
        <dt>Port</dt>
        <dd>{data.port ?? 'Unknown'}</dd>
        <dt>PG version</dt>
        <dd>{data.version == null ? 'Unknown' : formatPostgresVersion(data.version)}</dd>
        <dt>Permission tier</dt>
        <dd>{data.tier ?? 'Unknown'}</dd>
        {role === 'standby' ? (
          <>
            <dt>Replay lag</dt>
            <dd>{formatReplayLag(data.replayLagSec)}</dd>
          </>
        ) : null}
      </dl>
      <Handle aria-hidden="true" position={Position.Bottom} type="source" />
    </div>
  )
}
