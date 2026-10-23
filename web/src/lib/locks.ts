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

function compareByPID(left: LockNode, right: LockNode): number {
  return keyOf(left.pid).localeCompare(keyOf(right.pid), 'en', { numeric: true })
}

/**
 * Build a blocking forest while representing a repeated node as a cycle marker.
 *
 * pg_blocking_pids() names every session queued ahead of a waiter on a
 * conflicting lock, so a queue lists each earlier waiter in every later
 * waiter's blockers. Each session is placed once, breadth first from all the
 * roots together, under the blocker closest to a root. Waiters are visited in
 * pid order, so the tree does not depend on the order the nodes arrive in.
 */
export function buildBlockingTree(nodes: readonly LockNode[]): BlockingTree {
  const byPID = new Map<string, LockNode>()
  for (const node of nodes) {
    if (!byPID.has(keyOf(node.pid))) byPID.set(keyOf(node.pid), node)
  }

  const children = new Map<string, LockNode[]>()
  for (const node of nodes) {
    for (const blockerKey of new Set((node.blocked_by ?? []).map(keyOf))) {
      if (!byPID.has(blockerKey)) continue
      const siblings = children.get(blockerKey) ?? []
      siblings.push(node)
      children.set(blockerKey, siblings)
    }
  }

  for (const siblings of children.values()) siblings.sort(compareByPID)

  const placed = new Map<string, BlockingTreeNode>()
  const parentOf = new Map<string, string>()
  const isAncestorOrSelf = (candidateKey: string, key: string): boolean => {
    let current: string | undefined = key
    while (current !== undefined) {
      if (current === candidateKey) return true
      current = parentOf.get(current)
    }
    return false
  }

  // Every start is queued before the first step, so each session is placed
  // under the nearest start that reaches it, whichever order the starts came in.
  const place = (starts: readonly LockNode[]): BlockingTreeNode[] => {
    const startTrees: BlockingTreeNode[] = []
    const queue: BlockingTreeNode[] = []
    for (const start of starts) {
      if (placed.has(keyOf(start.pid))) continue
      const root: BlockingTreeNode = { node: start, children: [] }
      placed.set(keyOf(start.pid), root)
      startTrees.push(root)
      queue.push(root)
    }
    for (const current of queue) {
      const currentKey = keyOf(current.node.pid)
      for (const child of children.get(currentKey) ?? []) {
        const childKey = keyOf(child.pid)
        if (placed.has(childKey)) {
          // A session already placed elsewhere is only a cycle when it is one
          // of this node's ancestors; otherwise the edge is shown from its
          // placed position and dropped here.
          if (isAncestorOrSelf(childKey, currentKey)) {
            current.children.push({ cycle: true, node: child, children: [] })
          }
          continue
        }
        const childTree: BlockingTreeNode = { node: child, children: [] }
        placed.set(childKey, childTree)
        parentOf.set(childKey, currentKey)
        current.children.push(childTree)
        queue.push(childTree)
      }
    }
    return startTrees
  }

  const hasKnownBlocker = (node: LockNode): boolean =>
    (node.blocked_by ?? []).some((blocker) => byPID.has(keyOf(blocker)))
  const rootNodes = nodes.filter((node) => !hasKnownBlocker(node)).sort(compareByPID)
  const forest: BlockingTree = place(rootNodes)

  // A cycle has no unblocked root. Add one representative so the operator can
  // see the complete component without allowing recursion to run forever.
  const unplaced = nodes.filter((node) => !placed.has(keyOf(node.pid))).sort(compareByPID)
  for (const node of unplaced) {
    if (!placed.has(keyOf(node.pid))) forest.push(...place([node]))
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

function blockedCountOf(tree: BlockingTreeNode): number {
  return tree.children.reduce(
    (count, child) => count + (child.cycle ? 0 : 1 + blockedCountOf(child)),
    0,
  )
}

/** Count distinct descendants represented by a tree, excluding cycle markers. */
export function blockedCount(tree: BlockingTree | BlockingTreeNode): number {
  const roots = Array.isArray(tree) ? tree : [tree]
  return roots.reduce((count, root) => count + blockedCountOf(root), 0)
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
