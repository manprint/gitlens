export type LockPID = number | string

export interface LockNode {
  pid: LockPID
  blocked_by?: readonly LockPID[] | null
  sampled_at?: string | null
  wait_started_at?: string | null
  wait_duration?: number | null
  xact_age?: number | null
  state_age?: number | null
  [key: string]: unknown
}

export interface BlockingTreeNode {
  node: LockNode
  children: BlockingTreeNode[]
  cycle?: boolean
}

export type BlockingTree = BlockingTreeNode[]

function keyOf(pid: LockPID): string {
  return String(pid)
}

function finiteDuration(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? Math.max(0, value) : null
}

function timestampMillis(value: unknown): number | null {
  if (typeof value !== 'string' && !(value instanceof Date)) return null
  const millis = new Date(value).getTime()
  return Number.isFinite(millis) ? millis : null
}

function fallbackWaitDuration(node: LockNode): number {
  return (
    finiteDuration(node.wait_duration) ??
    finiteDuration(node.state_age) ??
    finiteDuration(node.xact_age) ??
    0
  )
}

/**
 * Build a blocking forest in which each session appears once. A queued waiter
 * is blocked by everyone ahead of it, so attaching it under every blocker would
 * repeat whole subtrees and grow as 2^N. It is attached under the first blocker
 * that reaches it instead, and a repeat along its own path is a cycle marker.
 */
export function buildBlockingTree(nodes: readonly LockNode[]): BlockingTree {
  const byPID = new Map<string, LockNode>()
  for (const node of nodes) {
    if (!byPID.has(keyOf(node.pid))) byPID.set(keyOf(node.pid), node)
  }

  const children = new Map<string, LockNode[]>()
  for (const node of nodes) {
    for (const blocker of node.blocked_by ?? []) {
      const blockerKey = keyOf(blocker)
      if (!byPID.has(blockerKey)) continue
      const siblings = children.get(blockerKey) ?? []
      siblings.push(node)
      children.set(blockerKey, siblings)
    }
  }

  const placed = new Set<string>()
  const build = (node: LockNode, path: ReadonlySet<string>): BlockingTreeNode => {
    const nodeKey = keyOf(node.pid)
    if (path.has(nodeKey)) return { cycle: true, node, children: [] }

    placed.add(nodeKey)
    const nextPath = new Set(path)
    nextPath.add(nodeKey)
    const childNodes: BlockingTreeNode[] = []
    for (const child of children.get(nodeKey) ?? []) {
      const childKey = keyOf(child.pid)
      if (!nextPath.has(childKey) && placed.has(childKey)) continue
      childNodes.push(build(child, nextPath))
    }
    return { node, children: childNodes }
  }

  const hasKnownBlocker = (node: LockNode): boolean =>
    (node.blocked_by ?? []).some((blocker) => byPID.has(keyOf(blocker)))
  const roots = nodes.filter((node) => !hasKnownBlocker(node))
  const forest = roots.map((root) => build(root, new Set<string>()))

  // A cycle has no unblocked root. Add one representative so the operator can
  // see the complete component without allowing recursion to run forever.
  for (const node of nodes) {
    if (!placed.has(keyOf(node.pid))) {
      forest.push(build(node, new Set<string>()))
    }
  }

  return forest
}

function depthOf(tree: BlockingTreeNode): number {
  return 1 + Math.max(0, ...tree.children.map(depthOf))
}

/** Return the deepest level in a tree or forest; a root is level one. */
export function treeDepth(tree: BlockingTree | BlockingTreeNode): number {
  const roots = Array.isArray(tree) ? tree : [tree]
  return Math.max(0, ...roots.map(depthOf))
}

/** Count distinct sessions represented below the roots, excluding cycle markers. */
export function blockedCount(tree: BlockingTree | BlockingTreeNode): number {
  const roots = Array.isArray(tree) ? tree : [tree]
  const blocked = new Set<string>()
  const visit = (node: BlockingTreeNode): void => {
    for (const child of node.children) {
      if (child.cycle) continue
      blocked.add(keyOf(child.node.pid))
      visit(child)
    }
  }
  roots.forEach(visit)
  return blocked.size
}

/** Derive wait seconds from the sample timestamp, never from the current clock. */
export function waitDuration(node: LockNode, sampledAt: string | Date | null): number {
  const sampleMillis = timestampMillis(sampledAt)
  const startedMillis = timestampMillis(node.wait_started_at)
  if (sampleMillis !== null && startedMillis !== null) {
    return Math.max(0, (sampleMillis - startedMillis) / 1000)
  }

  return fallbackWaitDuration(node)
}

/** Rank roots by the number of blocked sessions, then by snapshot wait duration. */
export function rankRoots(
  forest: readonly BlockingTreeNode[],
  sampledAt?: string | Date,
): BlockingTree {
  return [...forest].sort((left, right) => {
    const blockedDifference = blockedCount(right) - blockedCount(left)
    if (blockedDifference !== 0) return blockedDifference

    const leftWait = waitDuration(left.node, sampledAt ?? left.node.sampled_at ?? null)
    const rightWait = waitDuration(right.node, sampledAt ?? right.node.sampled_at ?? null)
    return rightWait - leftWait
  })
}
