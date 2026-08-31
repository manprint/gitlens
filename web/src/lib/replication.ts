import type { Schemas, SeriesPoint } from '@/api/types'

export type ReplicationInstance = Schemas['InstanceSummary']
export type TopologyEdge = Schemas['TopologyEdge']
export type ReplicationMetricEdge = Schemas['ReplicationEdge']

export interface ReplicationNode {
  id: string
  role: string | null
  address: string | null
  port: number | null
  version: number | null
  tier: Schemas['PermTier'] | null
  up: boolean | null
  unresolved?: boolean
}

export interface ReplicationGraphEdge {
  id: string
  from: string
  to: string
  type: string
  sync_state: string | null
  confidence: 'high' | 'low'
  note?: string | null
}

export interface ReplicationGraph {
  nodes: ReplicationNode[]
  edges: ReplicationGraphEdge[]
}

export interface GraphPosition {
  x: number
  y: number
}

export interface PositionedReplicationNode extends ReplicationNode {
  position: GraphPosition
}

export interface PositionedReplicationGraph {
  nodes: PositionedReplicationNode[]
  edges: ReplicationGraphEdge[]
}

export interface ReplicationAnomalies {
  noPrimary: string[]
  multiplePrimary: string[]
  orphanStandbys: string[]
  /** Edge ids; the other anomaly lists contain node ids. */
  lowConfidenceEdges: string[]
}

export interface AlignedReplicationSeries {
  timestamps: string[]
  write_lag_sec: (number | null)[]
  flush_lag_sec: (number | null)[]
  replay_lag_sec: (number | null)[]
}

export interface ReplicationSlot {
  name: string
  active: boolean
  retainedBytes: number
}

export interface ServerReplicationSlot {
  slot_name: string
  slot_active: boolean | number
  retained_bytes: number
}

export type ReplicationSlotInput = ReplicationSlot | ServerReplicationSlot

export interface SlotSummary {
  inactive: number
  retainedBytesTotal: number
  worst: string | null
}

function unresolvedNode(id: string): ReplicationNode {
  return {
    address: null,
    id: `unresolved:${id}`,
    port: null,
    role: 'unresolved',
    tier: null,
    up: null,
    unresolved: true,
    version: null,
  }
}

function edgeEndpoint(
  id: string,
  known: Map<string, ReplicationNode>,
  nodes: ReplicationNode[],
): string {
  if (known.has(id)) return id
  const unresolved = `unresolved:${id}`
  if (!known.has(unresolved)) {
    const node = unresolvedNode(id)
    nodes.push(node)
    known.set(unresolved, node)
  }
  return unresolved
}

/** Build the graph data without dropping topology edges with unknown endpoints. */
export function buildGraph(
  instances: readonly ReplicationInstance[],
  topology: readonly TopologyEdge[],
): ReplicationGraph {
  const nodes: ReplicationNode[] = instances.map((instance) => ({
    address: instance.addr,
    id: instance.instance_id,
    port: instance.port,
    role: instance.role,
    tier: instance.perm_tier,
    up: instance.up,
    version: instance.pg_version,
  }))
  const known = new Map(nodes.map((node) => [node.id, node]))
  const edges: ReplicationGraphEdge[] = []

  topology.forEach((topologyEdge, index) => {
    const from = edgeEndpoint(topologyEdge.from, known, nodes)
    const to = edgeEndpoint(topologyEdge.to, known, nodes)
    const edge: ReplicationGraphEdge = {
      confidence: topologyEdge.confidence,
      from,
      id: `edge-${index}-${from}-${to}`,
      sync_state: topologyEdge.sync_state ?? null,
      to,
      type: topologyEdge.type,
    }
    if (topologyEdge.note !== undefined) edge.note = topologyEdge.note
    edges.push(edge)
  })

  return { edges, nodes }
}

function nodeSort(a: ReplicationNode, b: ReplicationNode): number {
  const addressA = a.address ?? '\uffff'
  const addressB = b.address ?? '\uffff'
  return addressA.localeCompare(addressB) || a.id.localeCompare(b.id)
}

function edgeParentChild(
  edge: ReplicationGraphEdge,
  nodes: ReadonlyMap<string, ReplicationNode>,
): [parent: string, child: string] {
  const from = nodes.get(edge.from)
  const to = nodes.get(edge.to)
  if (from?.role === 'primary' && to?.role !== 'primary') return [edge.from, edge.to]
  if (to?.role === 'primary' && from?.role !== 'primary') return [edge.to, edge.from]
  if (from?.unresolved && !to?.unresolved) return [edge.to, edge.from]
  if (to?.unresolved && !from?.unresolved) return [edge.from, edge.to]

  // The API stores the reporting standby in `from` and its upstream in `to`.
  return [edge.to, edge.from]
}

/** Add deterministic positions: primary row first, then each replication hop. */
export function layoutGraph(graph: ReplicationGraph): PositionedReplicationGraph {
  const nodesByID = new Map(graph.nodes.map((node) => [node.id, node]))
  const children = new Map<string, string[]>()
  for (const edge of graph.edges) {
    const [parent, child] = edgeParentChild(edge, nodesByID)
    const existing = children.get(parent) ?? []
    existing.push(child)
    children.set(parent, existing)
  }
  for (const childIDs of children.values()) {
    childIDs.sort((a, b) => nodeSort(nodesByID.get(a)!, nodesByID.get(b)!))
  }

  const primaryIDs = graph.nodes.filter((node) => node.role === 'primary').map((node) => node.id)
  const depth = new Map<string, number>()
  const queue = [...primaryIDs.sort((a, b) => nodeSort(nodesByID.get(a)!, nodesByID.get(b)!))]
  for (const id of queue) depth.set(id, 0)

  while (queue.length > 0) {
    const parent = queue.shift()!
    const parentDepth = depth.get(parent) ?? 0
    for (const child of children.get(parent) ?? []) {
      const nextDepth = parentDepth + 1
      const currentDepth = depth.get(child)
      if (currentDepth === undefined || nextDepth < currentDepth) {
        depth.set(child, nextDepth)
        queue.push(child)
      }
    }
  }

  const unvisited = graph.nodes.filter((node) => !depth.has(node.id)).sort(nodeSort)
  for (const node of unvisited) depth.set(node.id, primaryIDs.length > 0 ? 1 : 0)

  const rows = new Map<number, string[]>()
  for (const node of graph.nodes) {
    const row = rows.get(depth.get(node.id) ?? 0) ?? []
    row.push(node.id)
    rows.set(depth.get(node.id) ?? 0, row)
  }
  for (const row of rows.values())
    row.sort((a, b) => nodeSort(nodesByID.get(a)!, nodesByID.get(b)!))

  const positions = new Map<string, GraphPosition>()
  for (const [y, row] of [...rows.entries()].sort(([a], [b]) => a - b)) {
    row.forEach((id, x) => positions.set(id, { x, y }))
  }

  return {
    edges: graph.edges.map((edge) => ({ ...edge })),
    nodes: graph.nodes.map((node) => ({
      ...node,
      position: positions.get(node.id) ?? { x: 0, y: 0 },
    })),
  }
}

function graphParentChildren(graph: ReplicationGraph): Map<string, Set<string>> {
  const nodesByID = new Map(graph.nodes.map((node) => [node.id, node]))
  const parents = new Map<string, Set<string>>()
  for (const edge of graph.edges) {
    const [parent, child] = edgeParentChild(edge, nodesByID)
    const parentSet = parents.get(child) ?? new Set<string>()
    parentSet.add(parent)
    parents.set(child, parentSet)
  }
  return parents
}

/** Find missing-primary, split-brain, orphan, and low-confidence anomalies. */
export function detectAnomalies(graph: ReplicationGraph): ReplicationAnomalies {
  const primaryIDs = graph.nodes
    .filter((node) => node.role === 'primary' && !node.unresolved)
    .map((node) => node.id)
  const parents = graphParentChildren(graph)

  return {
    lowConfidenceEdges: graph.edges
      .filter((edge) => edge.confidence === 'low')
      .map((edge) => edge.id),
    multiplePrimary: primaryIDs.length > 1 ? primaryIDs : [],
    noPrimary:
      primaryIDs.length === 0
        ? graph.nodes.filter((node) => !node.unresolved).map((node) => node.id)
        : [],
    orphanStandbys: graph.nodes
      .filter((node) => node.role === 'standby' && !parents.has(node.id))
      .map((node) => node.id),
  }
}

function compareTimestamp(a: string, b: string): number {
  const timeA = Date.parse(a)
  const timeB = Date.parse(b)
  if (Number.isFinite(timeA) && Number.isFinite(timeB) && timeA !== timeB) return timeA - timeB
  return a.localeCompare(b)
}

/** Align one edge's three metric responses on one timestamp axis. */
export function alignSeries(edges: readonly ReplicationMetricEdge[]): AlignedReplicationSeries {
  const timestamps = [
    ...new Set(edges.flatMap((edge) => edge.series.map((point) => point.ts))),
  ].sort(compareTimestamp)
  const metrics = {
    flush_lag_sec: 'flush_lag_sec',
    replay_lag_sec: 'replay_lag_sec',
    write_lag_sec: 'write_lag_sec',
  } as const

  const valuesFor = (metric: keyof typeof metrics): (number | null)[] => {
    const edge = edges.find((candidate) => candidate.metric === metrics[metric])
    const values = new Map(edge?.series.map((point) => [point.ts, point.value]) ?? [])
    return timestamps.map((timestamp) => values.get(timestamp) ?? null)
  }

  return {
    flush_lag_sec: valuesFor('flush_lag_sec'),
    replay_lag_sec: valuesFor('replay_lag_sec'),
    timestamps,
    write_lag_sec: valuesFor('write_lag_sec'),
  }
}

export function hasGaps(series: readonly (number | null | SeriesPoint)[]): boolean {
  return series.some((point) =>
    point !== null && typeof point === 'object' ? point.value === null : point === null,
  )
}

function slotName(slot: ReplicationSlotInput): string {
  return 'name' in slot ? slot.name : slot.slot_name
}

function slotActive(slot: ReplicationSlotInput): boolean | null {
  const active = 'active' in slot ? slot.active : slot.slot_active
  if (typeof active === 'boolean') return active
  if (typeof active === 'number' && Number.isFinite(active)) return active !== 0
  return null
}

function slotRetainedBytes(slot: ReplicationSlotInput): number | null {
  const retained = 'retainedBytes' in slot ? slot.retainedBytes : slot.retained_bytes
  return Number.isFinite(retained) && retained >= 0 ? retained : null
}

/** Summarise slot inactivity and retention without turning unknown values into zero. */
export function summariseSlots(slots: readonly ReplicationSlotInput[]): SlotSummary {
  let inactive = 0
  let retainedBytesTotal = 0
  let worst: { name: string; retainedBytes: number } | null = null

  for (const slot of slots) {
    if (slotActive(slot) === false) inactive += 1
    const retainedBytes = slotRetainedBytes(slot)
    if (retainedBytes === null) continue
    retainedBytesTotal += retainedBytes
    if (worst === null || retainedBytes > worst.retainedBytes) {
      worst = { name: slotName(slot), retainedBytes }
    }
  }

  return {
    inactive,
    retainedBytesTotal,
    worst: worst?.name ?? null,
  }
}
