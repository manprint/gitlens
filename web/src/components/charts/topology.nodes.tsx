import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'

import { Badge } from '@/components/ui/badge'
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
  const isDown = data.up === false

  return (
    <div
      aria-label={`${role} node ${address}, ${state}`}
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
      <p className="mt-2 font-medium break-all">{address}</p>
      <dl className="text-muted-foreground mt-2 grid grid-cols-[auto_1fr] gap-x-2 text-xs">
        <dt>Port</dt>
        <dd>{data.port ?? 'Unknown'}</dd>
        <dt>PG version</dt>
        <dd>{data.version ?? 'Unknown'}</dd>
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

export const topologyNodeTypes = { topology: TopologyNode }
