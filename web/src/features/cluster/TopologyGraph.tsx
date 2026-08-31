import { useCallback, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import { Background, Controls, ReactFlow, type Edge, type Node, type OnInit } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import type { ReplicationGraphEdge, ReplicationInstance, TopologyEdge } from '@/lib/replication'
import { buildGraph, detectAnomalies, layoutGraph } from '@/lib/replication'

import {
  topologyNodeTypes,
  type TopologyFlowNode,
  type TopologyNodeData,
} from '@/components/charts/topology.nodes'

export interface TopologyGraphProps {
  instances: readonly ReplicationInstance[]
  topology: readonly TopologyEdge[]
  /** Replay lag is supplied by the replication metrics section when available. */
  replayLagByInstance?: ReadonlyMap<string, number | null> | Readonly<Record<string, number | null>>
  /** Override the target id used when an edge is selected. */
  lagChartId?: (edge: ReplicationGraphEdge) => string
  onEdgeSelect?: (edge: ReplicationGraphEdge) => void
}

type TopologyGraphEdgeData = ReplicationGraphEdge & Record<string, unknown>
type TopologyFlowEdge = Edge<TopologyGraphEdgeData, 'default'>

const NODE_X_SPACING = 280
const NODE_Y_SPACING = 190

function syncLabel(syncState: string | null): string {
  return syncState === null || syncState.trim() === '' ? 'Unknown' : syncState
}

function getReplayLag(
  source: TopologyGraphProps['replayLagByInstance'],
  instanceID: string,
): number | null | undefined {
  if (!source) return undefined
  if ('get' in source && typeof source.get === 'function') return source.get(instanceID)
  return (source as Readonly<Record<string, number | null>>)[instanceID]
}

function nodeLabel(node: TopologyNodeData | undefined, fallback: string): string {
  if (!node) return fallback
  if (node.unresolved) return `Unresolved endpoint ${node.id.replace(/^unresolved:/, '')}`
  return `${node.address ?? node.id}:${node.port ?? 'Unknown'}`
}

function edgeDescription(
  edge: ReplicationGraphEdge,
  nodes: ReadonlyMap<string, TopologyNodeData>,
): string {
  const from = nodeLabel(nodes.get(edge.from), edge.from)
  const to = nodeLabel(nodes.get(edge.to), edge.to)
  const note = edge.note ? ` Note: ${edge.note}` : ''
  return `${from} to ${to}, ${edge.type}, ${syncLabel(edge.sync_state)}, ${edge.confidence}.${note}`
}

function EdgeTable({
  edges,
  nodes,
  visible,
}: {
  edges: readonly ReplicationGraphEdge[]
  nodes: ReadonlyMap<string, TopologyNodeData>
  visible: boolean
}) {
  return (
    <div className={visible ? 'overflow-x-auto' : 'sr-only'} data-topology-edge-table>
      <table aria-label="Replication topology edges" className="w-full text-left text-sm">
        <caption className="sr-only">Every replication topology edge and its confidence.</caption>
        <thead>
          <tr className="border-b text-xs uppercase">
            <th className="px-3 py-2" scope="col">
              From
            </th>
            <th className="px-3 py-2" scope="col">
              To
            </th>
            <th className="px-3 py-2" scope="col">
              Type
            </th>
            <th className="px-3 py-2" scope="col">
              Sync state
            </th>
            <th className="px-3 py-2" scope="col">
              Confidence
            </th>
            <th className="px-3 py-2" scope="col">
              Note
            </th>
          </tr>
        </thead>
        <tbody>
          {edges.map((edge) => (
            <tr className="border-b" data-edge-row={edge.id} key={edge.id}>
              <td className="px-3 py-2">{nodeLabel(nodes.get(edge.from), edge.from)}</td>
              <td className="px-3 py-2">{nodeLabel(nodes.get(edge.to), edge.to)}</td>
              <td className="px-3 py-2">{edge.type}</td>
              <td className="px-3 py-2">{syncLabel(edge.sync_state)}</td>
              <td className="px-3 py-2">{edge.confidence}</td>
              <td className="px-3 py-2">{edge.note ?? '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function TopologyGraph({
  instances,
  topology,
  replayLagByInstance,
  lagChartId = (edge) => `lag-chart-${edge.id}`,
  onEdgeSelect,
}: TopologyGraphProps) {
  const navigate = useNavigate()
  const [showTable, setShowTable] = useState(false)
  const [selectedEdgeID, setSelectedEdgeID] = useState<string | null>(null)
  const fitted = useRef(false)
  const highlightedChart = useRef<HTMLElement | null>(null)

  const graph = useMemo(() => buildGraph(instances, topology), [instances, topology])
  const positionedGraph = useMemo(() => layoutGraph(graph), [graph])
  const anomalies = useMemo(() => detectAnomalies(graph), [graph])

  const nodeData = useMemo(
    () =>
      new Map(
        positionedGraph.nodes.map((node) => [
          node.id,
          (() => {
            const replayLagSec = getReplayLag(replayLagByInstance, node.id)
            return (
              replayLagSec === undefined ? { ...node } : { ...node, replayLagSec }
            ) satisfies TopologyNodeData
          })(),
        ]),
      ),
    [positionedGraph.nodes, replayLagByInstance],
  )

  const flowNodes = useMemo<Node<TopologyNodeData, 'topology'>[]>(
    () =>
      positionedGraph.nodes.map((node) => ({
        data: nodeData.get(node.id)!,
        draggable: false,
        id: node.id,
        position: {
          x: node.position.x * NODE_X_SPACING,
          y: node.position.y * NODE_Y_SPACING,
        },
        type: 'topology',
      })),
    [nodeData, positionedGraph.nodes],
  )

  const flowEdges = useMemo<TopologyFlowEdge[]>(
    () =>
      positionedGraph.edges.map((edge) => ({
        ariaLabel: edgeDescription(edge, nodeData),
        data: edge as TopologyGraphEdgeData,
        id: edge.id,
        label: syncLabel(edge.sync_state),
        labelShowBg: true,
        source: edge.from,
        style: {
          strokeDasharray: edge.confidence === 'low' ? '6 4' : undefined,
          strokeWidth: edge.confidence === 'low' ? 2 : 1,
        },
        target: edge.to,
      })),
    [nodeData, positionedGraph.edges],
  )

  const handleInit = useCallback<OnInit<TopologyFlowNode, TopologyFlowEdge>>((instance) => {
    // React Flow owns the viewport; fitView is intentionally a one-time action
    // so a 15-second poll cannot reset a user's pan or zoom.
    if (!fitted.current) {
      fitted.current = true
      void instance.fitView({ padding: 0.2 })
    }
  }, [])

  const handleNodeClick = useCallback(
    (_event: ReactMouseEvent, node: TopologyFlowNode) => {
      if (!node.data.unresolved) void navigate(`/instances/${encodeURIComponent(node.id)}`)
    },
    [navigate],
  )

  const handleEdgeClick = useCallback(
    (_event: ReactMouseEvent, edge: TopologyFlowEdge) => {
      const graphEdge = graph.edges.find((candidate) => candidate.id === edge.id)
      if (!graphEdge) return
      setSelectedEdgeID(graphEdge.id)
      onEdgeSelect?.(graphEdge)

      highlightedChart.current?.removeAttribute('data-highlighted')
      const chart = document.getElementById(lagChartId(graphEdge))
      if (chart) {
        chart.setAttribute('data-highlighted', 'true')
        chart.scrollIntoView({ behavior: 'smooth', block: 'center' })
        highlightedChart.current = chart
      } else {
        highlightedChart.current = null
      }
    },
    [graph.edges, lagChartId, onEdgeSelect],
  )

  const hasCritical = anomalies.noPrimary.length > 0 || anomalies.multiplePrimary.length > 0
  const hasWarning = anomalies.orphanStandbys.length > 0 || anomalies.lowConfidenceEdges.length > 0
  const nodeCount = positionedGraph.nodes.length
  const edgeCount = positionedGraph.edges.length

  return (
    <section aria-labelledby="topology-graph-title" className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-medium" id="topology-graph-title">
            Replication topology
          </h2>
          <p className="text-muted-foreground text-sm">
            {nodeCount} instance{nodeCount === 1 ? '' : 's'}, {edgeCount} replication edge
            {edgeCount === 1 ? '' : 's'}.
          </p>
        </div>
        <Button
          aria-controls="topology-edge-table"
          aria-expanded={showTable}
          onClick={() => setShowTable((current) => !current)}
          type="button"
          variant="outline"
        >
          {showTable ? 'Hide accessible edge table' : 'Show accessible edge table'}
        </Button>
      </div>

      {hasCritical ? (
        <div
          aria-live="assertive"
          className="border-destructive bg-destructive/10 rounded-lg border-2 p-3"
          role="alert"
        >
          <strong>Critical replication anomaly.</strong>{' '}
          {anomalies.multiplePrimary.length > 0
            ? 'Split brain: more than one primary is reported.'
            : 'No primary is reported.'}
        </div>
      ) : null}
      {hasWarning ? (
        <div
          aria-live="polite"
          className="border-warning bg-warning/10 rounded-lg border p-3"
          role="status"
        >
          <strong>Replication topology warning.</strong>{' '}
          {anomalies.orphanStandbys.length > 0 ? 'One or more standbys have no upstream.' : null}
          {anomalies.orphanStandbys.length > 0 && anomalies.lowConfidenceEdges.length > 0
            ? ' '
            : null}
          {anomalies.lowConfidenceEdges.length > 0
            ? 'One or more edges have low confidence.'
            : null}
        </div>
      ) : null}

      <div
        aria-label="Replication topology graph"
        className="h-[32rem] rounded-lg border"
        data-topology-graph
        role="region"
      >
        <ReactFlow<TopologyFlowNode, TopologyFlowEdge>
          edges={flowEdges}
          fitView={false}
          nodes={flowNodes}
          nodeTypes={topologyNodeTypes}
          nodesConnectable={false}
          nodesDraggable={false}
          onEdgeClick={handleEdgeClick}
          onInit={handleInit}
          onNodeClick={handleNodeClick}
          panOnDrag
          zoomOnScroll
        >
          <Background />
          <Controls />
        </ReactFlow>
      </div>

      <div className="sr-only" id="topology-edge-table">
        <p id="topology-edge-table-description">Equivalent text table for the topology graph.</p>
        <EdgeTable edges={positionedGraph.edges} nodes={nodeData} visible={showTable} />
      </div>
      {showTable ? (
        <div aria-describedby="topology-edge-table-description" id="topology-edge-table-visible">
          <EdgeTable edges={positionedGraph.edges} nodes={nodeData} visible />
        </div>
      ) : null}
      {selectedEdgeID ? <p className="sr-only">Selected edge {selectedEdgeID}.</p> : null}
    </section>
  )
}
