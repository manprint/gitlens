import { describe, expect, it } from 'vitest'

import {
  blockedCount,
  buildBlockingTree,
  rankRoots,
  treeDepth,
  waitDuration,
  type LockNode,
} from './locks'

function lock(pid: number, blocked_by: number[] = [], overrides: Partial<LockNode> = {}): LockNode {
  return { pid, blocked_by, ...overrides }
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

  it('UI-LOCK-004 derives waitDuration from sampled_at, not the current clock', () => {
    const node = lock(42, [], {
      wait_started_at: '2026-09-01T00:00:00Z',
      wait_duration: 1,
    })

    expect(waitDuration(node, '2026-09-01T00:00:17Z')).toBe(17)
  })

  it('UI-LOCK-005 returns an empty forest for an empty node list', () => {
    expect(buildBlockingTree([])).toEqual([])
    expect(treeDepth([])).toBe(0)
    expect(blockedCount([])).toBe(0)
  })
})
