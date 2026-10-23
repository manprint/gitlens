export type LockPID = number | string

export interface LockNode {
  pid: LockPID
  blocked_by?: readonly LockPID[] | null
  sampled_at?: string | null
  /** Start time of the session, sent back unchanged with a cancel or terminate. When it is absent, the signal names the PID alone. */
  backend_start?: string | null
  /** Identity across PID reuse: the start time in microseconds, hex, a dot and the PID. Absent when backend_start is. */
  session_id?: string | null
  /** When the session began waiting for the lock it waits on, from the server. Absent for a session that waits on none. */
  wait_started_at?: string | null
  /** Seconds the session had waited at the sample, measured on the server. Absent for a session that waits on none. */
  wait_duration?: number | null
  xact_age?: number | null
  state_age?: number | null
  [key: string]: unknown
}

export interface BlockingTreeNode {
  node: LockNode
  children: BlockingTreeNode[]
  cycle?: boolean
  /** Set on a childless entry for a session already shown elsewhere in the forest. */
  reference?: BlockingTreeNode
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
 * Build a blocking forest in which each session is expanded once.
 *
 * pg_blocking_pids() names every session queued ahead of a waiter on a
 * conflicting lock, so a queue lists each earlier waiter in every later
 * waiter's blockers; attaching a waiter under every blocker would repeat whole
 * subtrees and grow as 2^N. Each session is placed once, breadth first from all
 * the roots together, under the blocker closest to a root. Waiters are visited
 * in pid order, so the tree does not depend on the order the nodes arrive in.
 *
 * A blocker in another root's tree than the waiter's gets one childless
 * reference to it, so an independent holder still shows the session it holds
 * up and its root still counts it. Within one root's tree the root already
 * counts the waiter, so no reference is added there. A repeat along a session's
 * own path is a cycle marker.
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
  const rootOf = new Map<string, string>()
  // The waiters each root's tree already references, so a tree shows one
  // reference per waiter however many of its sessions block it.
  const referenced = new Set<string>()
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
      const startKey = keyOf(start.pid)
      if (placed.has(startKey)) continue
      const root: BlockingTreeNode = { node: start, children: [] }
      placed.set(startKey, root)
      rootOf.set(startKey, startKey)
      startTrees.push(root)
      queue.push(root)
    }
    for (const current of queue) {
      const currentKey = keyOf(current.node.pid)
      const currentRoot = rootOf.get(currentKey) ?? currentKey
      for (const child of children.get(currentKey) ?? []) {
        const childKey = keyOf(child.pid)
        const seen = placed.get(childKey)
        if (seen !== undefined) {
          if (isAncestorOrSelf(childKey, currentKey)) {
            current.children.push({ cycle: true, node: child, children: [] })
          } else if (rootOf.get(childKey) !== currentRoot) {
            const pair = `${currentRoot}>${childKey}`
            if (!referenced.has(pair)) {
              referenced.add(pair)
              current.children.push({ node: child, children: [], reference: seen })
            }
          }
          continue
        }
        const childTree: BlockingTreeNode = { node: child, children: [] }
        placed.set(childKey, childTree)
        parentOf.set(childKey, currentKey)
        rootOf.set(childKey, currentRoot)
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

/**
 * Count distinct sessions represented below the roots, excluding cycle markers.
 * A reference counts its session and then the sessions below that session's
 * first entry, so a root ranks the same whichever blocker expanded the waiter.
 */
export function blockedCount(tree: BlockingTree | BlockingTreeNode): number {
  const roots = Array.isArray(tree) ? tree : [tree]
  const blocked = new Set<string>()
  const visited = new Set<BlockingTreeNode>()
  const visit = (node: BlockingTreeNode): void => {
    if (visited.has(node)) return
    visited.add(node)
    for (const child of node.children) {
      if (child.cycle) continue
      blocked.add(keyOf(child.node.pid))
      visit(child.reference ?? child)
    }
  }
  roots.forEach(visit)
  return blocked.size
}

/** A session waits on a lock when the snapshot names a blocker for it. */
export function isWaiting(node: LockNode): boolean {
  return (node.blocked_by ?? []).length > 0
}

/**
 * A root blocker blocks at least one session and is not itself blocked, so it
 * is the top of its blocking tree. A cycle marker or a reference is never one.
 */
export function isRootBlocker(tree: BlockingTreeNode): boolean {
  return (
    !tree.cycle && tree.reference === undefined && tree.children.length > 0 && !isWaiting(tree.node)
  )
}

/**
 * The wait in seconds at the sample. The server measured wait_duration on its
 * own clock at the sample, so it is used as sent. Without it, the wait is
 * derived from the start time and the sample timestamp, never the current clock.
 */
export function waitDuration(node: LockNode, sampledAt: string | Date | null): number {
  const measured = finiteDuration(node.wait_duration)
  if (measured !== null) return measured

  const sampleMillis = timestampMillis(sampledAt)
  const startedMillis = timestampMillis(node.wait_started_at)
  if (sampleMillis !== null && startedMillis !== null) {
    return Math.max(0, (sampleMillis - startedMillis) / 1000)
  }

  return fallbackWaitDuration(node)
}

/** The identity a session list entry and its tree node share. */
export function sessionKey(node: LockNode): string {
  return typeof node.session_id === 'string' && node.session_id !== ''
    ? node.session_id
    : keyOf(node.pid)
}

/**
 * Give each session list entry the query text of its tree node, by session
 * identity. The server ships text for the sessions of a blocking tree only, so
 * a session outside every tree gets none, even if an older agent sent one.
 */
export function sessionsWithTreeQuery(
  sessions: readonly LockNode[],
  nodes: readonly LockNode[],
): LockNode[] {
  const textByKey = new Map<string, string>()
  for (const node of nodes) {
    if (typeof node.query === 'string') textByKey.set(sessionKey(node), node.query)
  }
  return sessions.map((session) => ({ ...session, query: textByKey.get(sessionKey(session)) }))
}

/** Rank roots by the number of blocked sessions, then by snapshot wait duration. */
export function rankRoots(
  forest: readonly BlockingTreeNode[],
  sampledAt?: string | Date,
): BlockingTree {
  // blockedCount walks a whole subtree, so count each root once instead of on every comparison.
  const blocked = new Map(forest.map((root) => [root, blockedCount(root)] as const))
  return [...forest].sort((left, right) => {
    const blockedDifference = (blocked.get(right) ?? 0) - (blocked.get(left) ?? 0)
    if (blockedDifference !== 0) return blockedDifference

    const leftWait = waitDuration(left.node, sampledAt ?? left.node.sampled_at ?? null)
    const rightWait = waitDuration(right.node, sampledAt ?? right.node.sampled_at ?? null)
    return rightWait - leftWait
  })
}
