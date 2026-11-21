import { useCallback, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import { Background, Controls, ReactFlow, type Edge, type Node, type OnInit } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useNavigate } from 'react-router-dom'

import { HelpLink } from '@/components/layout/HelpLink'
import { Section } from '@/components/layout/Section'
import { SimpleTable, type SimpleColumn } from '@/components/layout/SimpleTable'
import { Button } from '@/components/ui/button'
import type { TableToolbarSpec } from '@/hooks/useFilteredRows'
import type { ReplicationGraphEdge, ReplicationInstance, TopologyEdge } from '@/lib/replication'
import { useT, type Translate } from '@/i18n/react'
import { buildGraph, detectAnomalies, layoutGraph } from '@/lib/replication'

import {
  TopologyNode,
  type TopologyFlowNode,
  type TopologyNodeData,
} from '@/components/charts/topology.nodes'

// Declared once, outside the component, so React Flow keeps the same node types between renders.
const topologyNodeTypes = { topology: TopologyNode }

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

function syncLabel(syncState: string | null, t: Translate): string {
  return syncState === null || syncState.trim() === '' ? t('cluster.common.unknown') : syncState
}

function getReplayLag(
  source: TopologyGraphProps['replayLagByInstance'],
  instanceID: string,
): number | null | undefined {
  if (!source) return undefined
  if ('get' in source && typeof source.get === 'function') return source.get(instanceID)
  return (source as Readonly<Record<string, number | null>>)[instanceID]
}

function nodeLabel(node: TopologyNodeData | undefined, fallback: string, t: Translate): string {
  if (!node) return fallback
  if (node.unresolved) {
    return t('cluster.topology.unresolved', { id: node.id.replace(/^unresolved:/, '') })
  }
  if (node.name) return node.name
  return `${node.address ?? node.id}:${node.port ?? t('cluster.common.unknown')}`
}

function edgeDescription(
  edge: ReplicationGraphEdge,
  nodes: ReadonlyMap<string, TopologyNodeData>,
  t: Translate,
): string {
  const from = nodeLabel(nodes.get(edge.from), edge.from, t)
  const to = nodeLabel(nodes.get(edge.to), edge.to, t)
  const note = edge.note ? t('cluster.topology.note', { note: edge.note }) : ''
  return t('cluster.topology.edgeDescription', {
    from,
    to,
    type: edge.type,
    sync: syncLabel(edge.sync_state, t),
    confidence: edge.confidence,
    note,
  })
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
  const t = useT()
  return (
    <div className={visible ? 'overflow-x-auto' : 'sr-only'} data-topology-edge-table>
      <table aria-label={t('cluster.topology.tableLabel')} className="w-full text-left text-sm">
        <caption className="sr-only">{t('cluster.topology.tableCaption')}</caption>
        <thead>
          <tr className="border-b text-xs uppercase">
            <th className="px-3 py-2" scope="col">
              {t('cluster.topology.colFrom')}
            </th>
            <th className="px-3 py-2" scope="col">
              {t('cluster.topology.colTo')}
            </th>
            <th className="px-3 py-2" scope="col">
              {t('cluster.topology.colType')}
            </th>
            <th className="px-3 py-2" scope="col">
              {t('cluster.topology.colSync')}
            </th>
            <th className="px-3 py-2" scope="col">
              {t('cluster.topology.colConfidence')}
            </th>
            <th className="px-3 py-2" scope="col">
              {t('cluster.topology.colNote')}
            </th>
          </tr>
        </thead>
        <tbody>
          {edges.map((edge) => (
            <tr className="border-b" data-edge-row={edge.id} key={edge.id}>
              <td className="px-3 py-2">{nodeLabel(nodes.get(edge.from), edge.from, t)}</td>
              <td className="px-3 py-2">{nodeLabel(nodes.get(edge.to), edge.to, t)}</td>
              <td className="px-3 py-2">{edge.type}</td>
              <td className="px-3 py-2">{syncLabel(edge.sync_state, t)}</td>
              <td className="px-3 py-2">{edge.confidence}</td>
              <td className="px-3 py-2">{edge.note ?? '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

interface EdgeRow {
  edge: ReplicationGraphEdge
  from: string
  to: string
}

function edgeToolbar(t: Translate): TableToolbarSpec<EdgeRow> {
  return {
    searchFields: [(row) => row.from, (row) => row.to, (row) => row.edge.note],
    filters: [
      {
        id: 'type',
        label: t('cluster.topology.colType'),
        kind: 'select',
        accessor: (row) => row.edge.type,
      },
      {
        id: 'sync',
        label: t('cluster.topology.colSync'),
        kind: 'select',
        accessor: (row) => syncLabel(row.edge.sync_state, t),
      },
    ],
  }
}

function edgeColumns(t: Translate): readonly SimpleColumn<EdgeRow>[] {
  return [
    {
      id: 'from',
      header: t('cluster.topology.colFrom'),
      sortValue: (row) => row.from,
      cell: (row) => row.from,
    },
    {
      id: 'to',
      header: t('cluster.topology.colTo'),
      sortValue: (row) => row.to,
      cell: (row) => row.to,
    },
    {
      id: 'type',
      header: t('cluster.topology.colType'),
      sortValue: (row) => row.edge.type,
      cell: (row) => row.edge.type,
    },
    {
      id: 'sync',
      header: t('cluster.topology.colSync'),
      sortValue: (row) => syncLabel(row.edge.sync_state, t),
      cell: (row) => syncLabel(row.edge.sync_state, t),
    },
    {
      id: 'confidence',
      header: t('cluster.topology.colConfidence'),
      sortValue: (row) => row.edge.confidence,
      cell: (row) => row.edge.confidence,
    },
    { id: 'note', header: t('cluster.topology.colNote'), cell: (row) => row.edge.note ?? '—' },
  ]
}

/**
 * The edge table the toggle reveals: searchable, filterable, paged (D12, D23).
 * The sr-only copy above stays the complete text equivalent of the graph.
 */
function VisibleEdgeTable({
  edges,
  nodes,
}: {
  edges: readonly ReplicationGraphEdge[]
  nodes: ReadonlyMap<string, TopologyNodeData>
}) {
  const t = useT()
  const columns = useMemo(() => edgeColumns(t), [t])
  const toolbar = useMemo(() => edgeToolbar(t), [t])
  const rows = useMemo(
    () =>
      edges.map((edge) => ({
        edge,
        from: nodeLabel(nodes.get(edge.from), edge.from, t),
        to: nodeLabel(nodes.get(edge.to), edge.to, t),
      })),
    [edges, nodes, t],
  )
  return (
    <SimpleTable
      ariaLabel={t('cluster.topology.tableLabel')}
      caption={t('cluster.topology.tableCaption')}
      captionId="topology.edges"
      columns={columns}
      emptyState={<p className="text-muted-foreground text-sm">{t('cluster.topology.noEdges')}</p>}
      rowKey={(row) => row.edge.id}
      rows={rows}
      scope="edges"
      tableId="topology-edges"
      toolbar={toolbar}
    />
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
  const t = useT()
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
        ariaLabel: edgeDescription(edge, nodeData, t),
        data: edge as TopologyGraphEdgeData,
        id: edge.id,
        label: syncLabel(edge.sync_state, t),
        labelShowBg: true,
        source: edge.from,
        style: {
          strokeDasharray: edge.confidence === 'low' ? '6 4' : undefined,
          strokeWidth: edge.confidence === 'low' ? 2 : 1,
        },
        target: edge.to,
      })),
    [nodeData, positionedGraph.edges, t],
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
    <Section
      actions={
        <>
          <Button
            aria-controls="topology-edge-table"
            aria-expanded={showTable}
            onClick={() => setShowTable((current) => !current)}
            type="button"
            variant="outline"
          >
            {showTable ? t('cluster.topology.hideTable') : t('cluster.topology.showTable')}
          </Button>
          <HelpLink anchor="cluster#topology" label={t('cluster.topology.heading')} />
        </>
      }
      captionId="topology.graph"
      title={t('cluster.topology.heading')}
    >
      <div className="space-y-4">
        <p className="text-muted-foreground text-sm">
          {t('cluster.topology.summary', { nodes: nodeCount, edges: edgeCount })}
        </p>

        {hasCritical ? (
          <div
            aria-live="assertive"
            className="border-destructive bg-destructive/10 rounded-lg border-2 p-3"
            role="alert"
          >
            <strong>{t('cluster.topology.anomalyTitle')}</strong>{' '}
            {anomalies.multiplePrimary.length > 0
              ? t('cluster.topology.splitBrain')
              : t('cluster.topology.noPrimary')}
          </div>
        ) : null}
        {hasWarning ? (
          <div
            aria-live="polite"
            className="border-warning bg-warning/10 rounded-lg border p-3"
            role="status"
          >
            <strong>{t('cluster.topology.warningTitle')}</strong>{' '}
            {anomalies.orphanStandbys.length > 0 ? t('cluster.topology.orphanStandbys') : null}
            {anomalies.orphanStandbys.length > 0 && anomalies.lowConfidenceEdges.length > 0
              ? ' '
              : null}
            {anomalies.lowConfidenceEdges.length > 0 ? t('cluster.topology.lowConfidence') : null}
          </div>
        ) : null}

        <div
          aria-label={t('cluster.topology.graphLabel')}
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
          <p id="topology-edge-table-description">{t('cluster.topology.tableIntro')}</p>
          <EdgeTable edges={positionedGraph.edges} nodes={nodeData} visible={showTable} />
        </div>
        {showTable ? (
          <div aria-describedby="topology-edge-table-description" id="topology-edge-table-visible">
            <VisibleEdgeTable edges={positionedGraph.edges} nodes={nodeData} />
          </div>
        ) : null}
        {selectedEdgeID ? (
          <p className="sr-only">{t('cluster.topology.selectedEdge', { id: selectedEdgeID })}</p>
        ) : null}
      </div>
    </Section>
  )
}
