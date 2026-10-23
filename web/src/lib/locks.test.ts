import { describe, expect, it } from 'vitest'

import {
  blockedCount,
  buildBlockingTree,
  isRootBlocker,
  rankRoots,
  sessionKey,
  sessionsWithTreeQuery,
  treeDepth,
  waitDuration,
  type BlockingTree,
  type BlockingTreeNode,
  type LockNode,
} from './locks'

function lock(pid: number, blocked_by: number[] = [], overrides: Partial<LockNode> = {}): LockNode {
  return { pid, blocked_by, ...overrides }
}

function countEntries(forest: readonly BlockingTreeNode[]): number {
  return forest.reduce((count, tree) => count + 1 + countEntries(tree.children), 0)
}

function must<T>(value: T | undefined): T {
  if (value === undefined) throw new Error('expected an entry')
  return value
}

function allEntries(forest: readonly BlockingTreeNode[]): BlockingTreeNode[] {
  return forest.flatMap((tree) => [tree, ...allEntries(tree.children)])
}

// Each placed parent-child pair as a string, so two trees compare as sets of edges
// whatever order their children were listed in. A cycle marker ends in * and a
// reference to a session placed in another root's tree ends in &.
function edgesOf(forest: BlockingTree): string[] {
  const edges: string[] = []
  const visit = (parent: string, tree: BlockingTreeNode) => {
    const child = `${parent}>${tree.node.pid}${tree.cycle ? '*' : ''}${tree.reference ? '&' : ''}`
    edges.push(child)
    for (const grandchild of tree.children) visit(String(tree.node.pid), grandchild)
  }
  for (const root of forest) visit('root', root)
  return edges.sort()
}

function permutations<T>(items: readonly T[]): T[][] {
  if (items.length <= 1) return [[...items]]
  return items.flatMap((item, index) =>
    permutations([...items.slice(0, index), ...items.slice(index + 1)]).map((rest) => [
      item,
      ...rest,
    ]),
  )
}

describe('lock-tree derivation', () => {
  it('UI-LOCK-001 builds a forest from a flat blocking list', () => {
    const forest = buildBlockingTree([lock(101), lock(202, [101]), lock(303, [202])])

    expect(forest).toHaveLength(1)
    expect(forest[0]?.node.pid).toBe(101)
    expect(forest[0]?.children[0]?.node.pid).toBe(202)
    expect(forest[0]?.children[0]?.children[0]?.node.pid).toBe(303)
    expect(treeDepth(forest)).toBe(3)
    expect(blockedCount(forest)).toBe(2)
  })

  it('UI-LOCK-002 detects two-node and three-node cycles without recursing forever', () => {
    const twoNode = buildBlockingTree([lock(1, [2]), lock(2, [1])])
    const threeNode = buildBlockingTree([lock(3, [4]), lock(4, [5]), lock(5, [3])])

    expect(twoNode).toHaveLength(1)
    expect(twoNode[0]?.children[0]?.children[0]?.cycle).toBe(true)
    expect(threeNode).toHaveLength(1)
    expect(threeNode[0]?.children[0]?.children[0]?.children[0]?.cycle).toBe(true)
  })

  it('UI-LOCK-006 places each queued session once, so a pile-up stays linear', () => {
    // Waiter i queues behind the holder and every waiter ahead of it, which is
    // how pg_blocking_pids reports a queue on an advisory lock.
    const waiters = Array.from({ length: 20 }, (_, index) =>
      lock(101 + index, [100, ...Array.from({ length: index }, (_, ahead) => 101 + ahead)]),
    )
    const forest = buildBlockingTree([lock(100), ...waiters])

    expect(forest).toHaveLength(1)
    expect(countEntries(forest)).toBe(21)
    expect(blockedCount(forest)).toBe(20)
  })

  it('UI-LOCK-007 counts a session once when several waiters share a blocker', () => {
    const forest = buildBlockingTree([lock(1), lock(2, [1]), lock(3, [1, 2])])

    expect(countEntries(forest)).toBe(3)
    expect(blockedCount(forest)).toBe(2)
  })

  it('UI-LOCK-061 keeps a waiter visible under every independent holder that blocks it', () => {
    const forest = buildBlockingTree([lock(1), lock(2), lock(3, [1, 2])])

    expect(forest.map((tree) => tree.node.pid)).toEqual([1, 2])
    const secondRoot = forest[1]
    expect(secondRoot?.children[0]?.node.pid).toBe(3)
    expect(secondRoot?.children[0]?.reference).toBeDefined()
    expect(secondRoot?.children[0]?.children).toEqual([])
    expect(blockedCount(secondRoot ?? [])).toBe(1)
  })

  it('UI-LOCK-062 counts the sessions below a referenced waiter under the other holder', () => {
    const forest = buildBlockingTree([lock(1), lock(2), lock(3, [1, 2]), lock(4, [3])])

    expect(countEntries(forest)).toBe(5)
    expect(blockedCount(forest[1] ?? [])).toBe(2)
    expect(rankRoots(forest).map((tree) => tree.node.pid)).toEqual([1, 2])
  })

  it('UI-LOCK-003 ranks roots by blocked count then wait duration', () => {
    const forest = buildBlockingTree([
      lock(10, [], { wait_duration: 10 }),
      lock(11, [10]),
      lock(20, [], { wait_duration: 60 }),
      lock(21, [20]),
      lock(22, [20]),
      lock(30, [], { wait_duration: 90 }),
      lock(31, [30]),
    ])

    expect(rankRoots(forest).map((tree) => tree.node.pid)).toEqual([20, 30, 10])
  })

  it('UI-LOCK-004 uses the server-measured wait_duration, and derives from sampled_at only without it', () => {
    const measured = lock(42, [1], {
      wait_started_at: '2026-09-01T00:00:00Z',
      wait_duration: 1,
    })
    const derived = lock(43, [1], { wait_started_at: '2026-09-01T00:00:00Z' })

    expect(waitDuration(measured, '2026-09-01T00:00:17Z')).toBe(1)
    expect(waitDuration(derived, '2026-09-01T00:00:17Z')).toBe(17)
  })

  it('UI-LOCK-067 marks a blocker that is not itself blocked as the root of its tree', () => {
    const forest = buildBlockingTree([lock(1), lock(2, [1]), lock(3, [2])])

    expect(isRootBlocker(must(forest[0]))).toBe(true)
    expect(isRootBlocker(must(must(forest[0]).children[0]))).toBe(false)
    const waiter = buildBlockingTree([lock(5), lock(6, [5])])
    expect(isRootBlocker(must(must(waiter[0]).children[0]))).toBe(false)
    expect(isRootBlocker(must(waiter[0]))).toBe(true)
  })

  it('UI-LOCK-068 is never a root blocker for a cycle marker, a reference, or a session that waits', () => {
    const cycle = buildBlockingTree([lock(1, [2]), lock(2, [1])])
    const markers = allEntries(cycle).filter((entry) => entry.cycle)
    expect(markers.length).toBeGreaterThan(0)
    for (const marker of markers) expect(isRootBlocker(marker)).toBe(false)

    const reference: BlockingTreeNode = {
      node: lock(9),
      children: [],
      reference: { node: lock(8), children: [] },
    }
    expect(isRootBlocker(reference)).toBe(false)

    const waitingBlocker: BlockingTreeNode = {
      node: lock(7, [6]),
      children: [{ node: lock(8, [7]), children: [] }],
    }
    expect(isRootBlocker(waitingBlocker)).toBe(false)
  })

  it('UI-LOCK-069 gives a session its identity from session_id, else its pid', () => {
    expect(sessionKey(lock(10, [], { session_id: '65e7e3480a401.10' }))).toBe('65e7e3480a401.10')
    expect(sessionKey(lock(10, [], { session_id: null }))).toBe('10')
  })

  it('UI-LOCK-070 gives a session list entry the text of its tree node only, by session identity', () => {
    const nodes = [
      lock(10, [], { session_id: 'a.10', query: 'UPDATE t SET x = 1' }),
      lock(11, [10], { session_id: 'b.11', query: 'SELECT 2' }),
    ]
    const sessions = [
      lock(10, [], {
        session_id: 'a.10',
        state: 'idle in transaction',
        query: 'stale text from an older agent',
      }),
      // The PID was reused by a later session, so it is not the node's session.
      lock(11, [], { session_id: 'c.11', query: 'SELECT 3' }),
      lock(12, [], { session_id: 'd.12', query: 'SELECT 4' }),
    ]

    const view = sessionsWithTreeQuery(sessions, nodes)

    expect(view.map((session) => session.query)).toEqual([
      'UPDATE t SET x = 1',
      undefined,
      undefined,
    ])
    expect(view[0]?.state).toBe('idle in transaction')
  })

  it('UI-LOCK-005 returns an empty forest for an empty node list', () => {
    expect(buildBlockingTree([])).toEqual([])
    expect(treeDepth([])).toBe(0)
    expect(blockedCount([])).toBe(0)
  })

  it('UI-LOCK-057 places a queue that every waiter lists in full once, under the holder', () => {
    const waiters = Array.from({ length: 20 }, (_, index) =>
      lock(index + 2, [1, ...Array.from({ length: index }, (_, earlier) => earlier + 2)]),
    )

    const forest = buildBlockingTree([lock(1), ...waiters])
    const reversed = buildBlockingTree([...waiters].reverse().concat(lock(1)))

    expect(forest).toHaveLength(1)
    expect(forest[0]?.children).toHaveLength(20)
    expect(blockedCount(forest)).toBe(20)
    expect(treeDepth(forest)).toBe(2)
    expect(blockedCount(reversed)).toBe(20)
    expect(treeDepth(reversed)).toBe(2)
  })

  it('UI-LOCK-058 places a session under the blocker nearest a root, whatever order the nodes arrive in', () => {
    // 4 is reachable from root 9 in one step and from root 1 in two, so it belongs
    // under 9, and 3 in root 1's tree keeps a reference to it.
    const nearest = [lock(1), lock(3, [1]), lock(4, [3, 9]), lock(9)]
    // 4 is two steps from each of the roots 1 and 9, so the tie goes to the lower
    // pid, 3, and 5 in root 9's tree keeps a reference to it.
    const tied = [lock(1), lock(9), lock(3, [1]), lock(5, [9]), lock(4, [3, 5])]

    for (const order of permutations(nearest)) {
      expect(edgesOf(buildBlockingTree(order))).toEqual(
        ['root>1', 'root>9', '1>3', '9>4', '3>4&'].sort(),
      )
    }
    for (const order of permutations(tied)) {
      expect(edgesOf(buildBlockingTree(order))).toEqual(
        ['root>1', 'root>9', '1>3', '9>5', '3>4', '5>4&'].sort(),
      )
    }
  })
})
