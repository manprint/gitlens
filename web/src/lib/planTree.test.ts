import { afterEach, describe, expect, it } from 'vitest'

import { refreshLocale, setServerLanguage } from '@/i18n/store'

import {
  bufferBytes,
  changeRatio,
  diffPlans,
  findExplainRoot,
  formatFactor,
  nodeTitle,
  parsePlan,
  placeholderCount,
  planText,
  type ParsedPlan,
} from './planTree'

const ANALYZED = [
  {
    'Planning Time': 0.5,
    'Execution Time': 101,
    Plan: {
      'Node Type': 'Hash Join',
      'Join Type': 'Inner',
      'Total Cost': 200,
      'Plan Rows': 10,
      'Actual Rows': 5000,
      'Actual Loops': 1,
      'Actual Total Time': 100,
      'Hash Cond': '(o.customer_id = c.id)',
      Plans: [
        {
          'Node Type': 'Seq Scan',
          'Relation Name': 'orders',
          Alias: 'o',
          'Total Cost': 150,
          'Plan Rows': 50000,
          'Actual Rows': 1000,
          'Actual Loops': 1,
          'Actual Total Time': 70,
          Filter: '(status = 1)',
          'Rows Removed by Filter': 99000,
        },
        {
          'Node Type': 'Hash',
          'Total Cost': 30,
          'Plan Rows': 100,
          'Actual Rows': 100,
          'Actual Loops': 1,
          'Actual Total Time': 5,
          'Hash Buckets': 1024,
          'Hash Batches': 4,
          'Peak Memory Usage': 64,
          Plans: [
            {
              'Node Type': 'Index Scan',
              'Relation Name': 'customers',
              Alias: 'c',
              'Index Name': 'customers_pkey',
              'Total Cost': 25,
              'Plan Rows': 100,
              'Actual Rows': 100,
              'Actual Loops': 1,
              'Actual Total Time': 4,
              'Index Cond': '(id > 0)',
            },
          ],
        },
      ],
    },
  },
]

function parsed(value: unknown, valuesFrom: Parameters<typeof parsePlan>[1] = null): ParsedPlan {
  const plan = parsePlan(value, valuesFrom)
  if (!plan) throw new Error('no plan')
  return plan
}

describe('findExplainRoot', () => {
  it('finds the plan in a command result, a stored array and a bare node', () => {
    const node = { 'Node Type': 'Result' }
    expect(findExplainRoot([{ Plan: node }])).toEqual({ Plan: node })
    expect(findExplainRoot({ plan: [{ Plan: node }], values_from: 'generic' })).toEqual({
      Plan: node,
    })
    expect(findExplainRoot({ result: { plan: [{ Plan: node }] } })).toEqual({ Plan: node })
    expect(findExplainRoot(node)).toEqual({ Plan: node })
  })

  it('returns null for anything without a plan', () => {
    expect(findExplainRoot(null)).toBeNull()
    expect(findExplainRoot('EXPLAIN')).toBeNull()
    expect(findExplainRoot({ rows: [] })).toBeNull()
    expect(parsePlan({ rows: [] })).toBeNull()
  })
})

describe('nodeTitle', () => {
  it.each([
    [{ 'Node Type': 'Hash Join', 'Join Type': 'Left' }, 'Hash Left Join'],
    [{ 'Node Type': 'Hash Join', 'Join Type': 'Inner' }, 'Hash Join'],
    [{ 'Node Type': 'Nested Loop', 'Join Type': 'Anti' }, 'Nested Loop Anti Join'],
    [{ 'Node Type': 'Aggregate', Strategy: 'Hashed' }, 'HashAggregate'],
    [
      { 'Node Type': 'Aggregate', Strategy: 'Sorted', 'Partial Mode': 'Partial' },
      'Partial GroupAggregate',
    ],
    [{ 'Node Type': 'Seq Scan', 'Parallel Aware': true }, 'Parallel Seq Scan'],
    [{ 'Node Type': 'Index Scan', 'Scan Direction': 'Backward' }, 'Index Scan Backward'],
    [{ 'Node Type': 'ModifyTable', Operation: 'Insert' }, 'Insert'],
    [{}, 'Plan node'],
  ])('names %o as psql does', (node, title) => {
    expect(nodeTitle(node)).toBe(title)
  })
})

describe('parsePlan', () => {
  it('builds the tree with totals over loops and the time each operation adds', () => {
    const plan = parsed(ANALYZED)

    expect(plan.analyzed).toBe(true)
    expect(plan.executionMs).toBe(101)
    expect(plan.planningMs).toBe(0.5)
    expect(plan.rows).toBe(5000)
    expect(plan.nodes.map((node) => [node.id, node.depth, node.title, node.object])).toEqual([
      ['0', 0, 'Hash Join', null],
      ['0.0', 1, 'Seq Scan', 'orders o'],
      ['0.1', 1, 'Hash', null],
      ['0.1.0', 2, 'Index Scan', 'customers c using customers_pkey'],
    ])
    const [join, scan, hash, index] = plan.nodes
    expect(join!.exclusiveMs).toBe(25)
    expect(scan!.exclusiveMs).toBe(70)
    expect(hash!.exclusiveMs).toBe(1)
    expect(index!.exclusiveMs).toBe(4)
    expect(scan!.timeShare).toBeCloseTo(70 / 101)
    expect(join!.exclusiveCost).toBe(20)
    expect(hash!.exclusiveCost).toBe(5)
    expect(join!.estimateFactor).toBe(500)
    expect(scan!.estimateFactor).toBeCloseTo(0.02)
    expect(scan!.rowsRemoved).toBe(99000)
    expect(join!.conditions).toEqual([{ label: 'Hash Cond', text: '(o.customer_id = c.id)' }])
    expect(hash!.details.find((detail) => detail.label === 'Hash')?.value).toContain('4 batches')
  })

  it('points at the slowest operation, the worst estimates, the discarded rows and the hash batches', () => {
    const titles = parsed(ANALYZED).insights.map((insight) => [
      insight.severity,
      insight.nodeId,
      insight.title,
    ])

    expect(titles).toEqual([
      ['high', '0.0', 'Seq Scan on orders o takes 69% of the execution time'],
      ['high', '0', 'Row estimate off ×500 at Hash Join'],
      ['medium', '0.0', 'Row estimate off ×50 at Seq Scan on orders o'],
      ['medium', '0.0', 'Seq Scan on orders o discards 99% of the rows it reads'],
      ['medium', '0.1', 'Hash split into 4 batches'],
    ])
  })

  it('keeps wall time under Gather by dividing by the processes that ran in parallel', () => {
    const plan = parsed({
      Plan: {
        'Node Type': 'Gather',
        'Workers Planned': 2,
        'Workers Launched': 2,
        'Total Cost': 10,
        'Plan Rows': 3,
        'Actual Rows': 3,
        'Actual Loops': 1,
        'Actual Total Time': 12,
        Plans: [
          {
            'Node Type': 'Seq Scan',
            'Parallel Aware': true,
            'Relation Name': 't',
            'Total Cost': 8,
            'Plan Rows': 1,
            'Actual Rows': 1,
            'Actual Loops': 3,
            'Actual Total Time': 10,
          },
        ],
      },
    })
    const [gather, scan] = plan.nodes

    expect(scan!.title).toBe('Parallel Seq Scan')
    expect(scan!.inclusiveMs).toBe(10)
    expect(scan!.actualRows).toBe(3)
    expect(gather!.exclusiveMs).toBe(2)
    expect(gather!.details).toContainEqual({
      key: 'workers',
      label: 'Workers',
      value: '2 launched of 2 planned',
    })
  })

  it('reports operations never executed and leaves their factor empty', () => {
    const plan = parsed({
      Plan: {
        'Node Type': 'Nested Loop',
        'Total Cost': 5,
        'Plan Rows': 1,
        'Actual Rows': 0,
        'Actual Loops': 1,
        'Actual Total Time': 0.1,
        Plans: [
          {
            'Node Type': 'Seq Scan',
            'Relation Name': 'a',
            'Total Cost': 1,
            'Plan Rows': 1,
            'Actual Rows': 0,
            'Actual Loops': 1,
            'Actual Total Time': 0.05,
          },
          {
            'Node Type': 'Seq Scan',
            'Relation Name': 'b',
            'Total Cost': 1,
            'Plan Rows': 1,
            'Actual Rows': 0,
            'Actual Loops': 0,
            'Actual Total Time': 0,
          },
        ],
      },
    })

    expect(plan.nodes[2]!.neverExecuted).toBe(true)
    expect(plan.nodes[2]!.estimateFactor).toBeNull()
    expect(plan.insights.map((insight) => insight.title)).toContain(
      '1 operation was never executed',
    )
  })

  it('says a plan-only result holds estimates and a generic plan has no values', () => {
    const plan = parsed(
      {
        Plan: { 'Node Type': 'Seq Scan', 'Relation Name': 't', 'Total Cost': 10, 'Plan Rows': 100 },
      },
      'generic',
    )

    expect(plan.analyzed).toBe(false)
    expect(plan.rows).toBe(100)
    expect(plan.nodes[0]!.costShare).toBe(1)
    expect(plan.insights.map((insight) => insight.title)).toEqual([
      'Plan only: costs are estimates, not times',
      'Generic plan, for any value of the parameters',
    ])
  })
})

describe('planText', () => {
  it('prints the plan as psql does, with conditions and timings', () => {
    const text = planText(parsed(ANALYZED)).split('\n')

    expect(text[0]).toMatch(
      /^Hash Join {2}\(cost=\?\.\.200\.00 rows=10 width=\?\) \(actual time=\?\.\.100\.000 rows=5000 loops=1\)$/,
    )
    expect(text[1]).toBe('  Hash Cond: (o.customer_id = c.id)')
    expect(text[2]).toMatch(/^ {2}-> {2}Seq Scan on orders o {2}\(cost=/)
    expect(text).toContain('        Filter: (status = 1)')
    expect(text).toContain('        Rows removed by Filter: 99,000')
    expect(text.find((line) => line.includes('Index Scan'))).toMatch(
      /^ {8}-> {2}Index Scan on customers c using customers_pkey/,
    )
    expect(text.at(-2)).toBe('Planning Time: 0.500 ms')
    expect(text.at(-1)).toBe('Execution Time: 101.000 ms')
  })
})

describe('diffPlans', () => {
  const left = parsed({
    Plan: {
      'Node Type': 'Sort',
      'Total Cost': 20,
      'Sort Key': ['id'],
      Plans: [
        { 'Node Type': 'Seq Scan', 'Relation Name': 't', 'Total Cost': 10, Filter: '(a = $1)' },
      ],
    },
  })

  it('marks every operation the same when the shape is the same', () => {
    expect(diffPlans(left, left).map((row) => row.change)).toEqual(['same', 'same'])
  })

  it('aligns by place in the tree and marks changed, added and removed operations', () => {
    const right = parsed({
      Plan: {
        'Node Type': 'Sort',
        'Total Cost': 30,
        'Sort Key': ['id'],
        Plans: [
          {
            'Node Type': 'Index Scan',
            'Relation Name': 't',
            'Index Name': 't_a',
            'Total Cost': 5,
            'Index Cond': '(a = $1)',
          },
          { 'Node Type': 'Seq Scan', 'Relation Name': 'u', 'Total Cost': 1 },
        ],
      },
    })

    expect(diffPlans(left, right).map((row) => [row.id, row.depth, row.change])).toEqual([
      ['0', 0, 'same'],
      ['0.0', 1, 'changed'],
      ['0.1', 1, 'added'],
    ])
    expect(diffPlans(right, left).at(-1)!.change).toBe('removed')
  })
})

describe('small helpers', () => {
  it('counts placeholders as the highest $n', () => {
    expect(placeholderCount('SELECT * FROM t WHERE a = $1 AND b = $12 AND c = $3')).toBe(12)
    expect(placeholderCount('SELECT 1')).toBe(0)
  })

  it('formats factors, ratios and buffer sizes', () => {
    expect(formatFactor(500)).toBe('×500')
    expect(formatFactor(25)).toBe('×25')
    expect(formatFactor(0.5)).toBe('×2.0')
    expect(changeRatio(10, 12)).toBeCloseTo(1.2)
    expect(changeRatio(0, 12)).toBeNull()
    expect(changeRatio(null, 12)).toBeNull()
    expect(bufferBytes(null)).toBe('—')
    expect(bufferBytes(128)).toMatch(/^1(\.0)? MiB$|^1(\.0)? MB$/)
  })
})

function analyzedPlan(node: Record<string, unknown>, top: Record<string, unknown> = {}) {
  return [
    {
      'Planning Time': 0.2,
      'Execution Time': 200,
      ...top,
      Plan: {
        'Node Type': 'Result',
        'Total Cost': 10,
        'Plan Rows': 1,
        'Actual Total Time': 200,
        'Actual Rows': 1,
        'Actual Loops': 1,
        Plans: [
          {
            'Parent Relationship': 'Outer',
            'Total Cost': 5,
            'Plan Rows': 10,
            'Actual Total Time': 1,
            'Actual Rows': 10,
            'Actual Loops': 1,
            ...node,
          },
        ],
      },
    },
  ]
}

function ofKind(value: unknown, kind: string) {
  return parsed(value).insights.filter((insight) => insight.kind === kind)
}

describe('plan insights added by plan 007', () => {
  afterEach(async () => {
    await setServerLanguage(undefined)
    await refreshLocale()
  })

  it('UI-PLI-001 flags a lossy bitmap heap scan', () => {
    const hits = ofKind(
      analyzedPlan({
        'Node Type': 'Bitmap Heap Scan',
        'Relation Name': 't',
        'Lossy Heap Blocks': 300,
        'Exact Heap Blocks': 700,
      }),
      'lossyBitmap',
    )
    expect(hits).toHaveLength(1)
    expect(hits[0]).toMatchObject({ nodeId: '0.0', args: { lossy: 300, exact: 700 } })
    expect(hits[0]?.title).toBe(
      'Bitmap scan went lossy on 300 of 1,000 heap blocks; work_mem is too small for the bitmap',
    )
    expect(
      ofKind(
        analyzedPlan({
          'Node Type': 'Bitmap Heap Scan',
          'Lossy Heap Blocks': 0,
          'Exact Heap Blocks': 9,
        }),
        'lossyBitmap',
      ),
    ).toEqual([])
  })

  it('UI-PLI-002 flags a Gather that started fewer workers than planned', () => {
    const hits = ofKind(
      analyzedPlan({ 'Node Type': 'Gather Merge', 'Workers Planned': 4, 'Workers Launched': 1 }),
      'workersShort',
    )
    expect(hits).toHaveLength(1)
    expect(hits[0]).toMatchObject({ nodeId: '0.0', args: { launched: 1, planned: 4 } })
    expect(hits[0]?.title).toBe('Only 1 of 4 parallel workers started')
    expect(
      ofKind(
        analyzedPlan({ 'Node Type': 'Gather', 'Workers Planned': 2, 'Workers Launched': 2 }),
        'workersShort',
      ),
    ).toEqual([])
  })

  it('UI-PLI-003 flags a sort that spilled to disk with its size', () => {
    const hits = ofKind(
      analyzedPlan({
        'Node Type': 'Incremental Sort',
        'Sort Method': 'external merge',
        'Sort Space Used': 2048,
        'Sort Space Type': 'Disk',
      }),
      'sortSpill',
    )
    expect(hits).toHaveLength(1)
    expect(hits[0]).toMatchObject({ nodeId: '0.0', args: { kb: 2048 } })
    expect(hits[0]?.title).toBe('Incremental Sort spilled to disk (2.0 MiB)')
    expect(
      ofKind(
        analyzedPlan({
          'Node Type': 'Sort',
          'Sort Method': 'quicksort',
          'Sort Space Used': 25,
          'Sort Space Type': 'Memory',
        }),
        'sortSpill',
      ),
    ).toEqual([])
  })

  it('UI-PLI-004 flags JIT taking a large share of a slow execution', () => {
    const hits = ofKind(
      analyzedPlan({ 'Node Type': 'Seq Scan' }, { JIT: { Functions: 4, Timing: { Total: 60 } } }),
      'jitShare',
    )
    expect(hits).toHaveLength(1)
    expect(hits[0]).toMatchObject({ nodeId: null, args: { ms: 60, pct: 0.3 } })
    expect(hits[0]?.title).toBe('JIT compilation took 60 ms (30% of execution)')
    expect(
      ofKind(
        analyzedPlan({ 'Node Type': 'Seq Scan' }, { JIT: { Timing: { Total: 30 } } }),
        'jitShare',
      ),
    ).toEqual([])
    const planOnly = parsed([
      {
        JIT: { Timing: { Total: 900 } },
        Plan: { 'Node Type': 'Seq Scan', 'Total Cost': 1, 'Plan Rows': 1 },
      },
    ])
    expect(planOnly.insights.map((insight) => insight.kind)).toEqual(['planOnly'])
  })

  it('UI-PLI-005 flags triggers taking a large share of the execution', () => {
    const triggers = [
      { 'Trigger Name': 'audit_row', Time: 50, Calls: 10 },
      { 'Trigger Name': 'RI_ConstraintTrigger_c_1', Time: 10, Calls: 10 },
    ]
    const hits = ofKind(
      analyzedPlan({ 'Node Type': 'Seq Scan' }, { Triggers: triggers }),
      'triggerShare',
    )
    expect(hits).toHaveLength(1)
    expect(hits[0]).toMatchObject({ nodeId: null, args: { ms: 60, pct: 0.3, name: 'audit_row' } })
    expect(hits[0]?.title).toBe('Triggers took 60 ms (30% of execution); slowest: audit_row')
    expect(
      ofKind(
        analyzedPlan({ 'Node Type': 'Seq Scan' }, { Triggers: [{ 'Trigger Name': 'x', Time: 5 }] }),
        'triggerShare',
      ),
    ).toEqual([])
  })

  it('UI-PLI-006 flags a join filter that throws most pairs away', () => {
    const hits = ofKind(
      analyzedPlan({
        'Node Type': 'Nested Loop',
        'Actual Rows': 5,
        'Actual Loops': 2,
        'Rows Removed by Join Filter': 6000,
      }),
      'joinFilterDiscards',
    )
    expect(hits).toHaveLength(1)
    expect(hits[0]).toMatchObject({ nodeId: '0.0', args: { removed: 12000, kept: 10 } })
    expect(hits[0]?.title).toBe('Join filter discarded 12,000 rows to keep 10')
    expect(
      ofKind(
        analyzedPlan({
          'Node Type': 'Hash Join',
          'Actual Rows': 2000,
          'Rows Removed by Join Filter': 15000,
        }),
        'joinFilterDiscards',
      ),
    ).toEqual([])
  })

  it('UI-PLI-007 leaves the insights of an existing plan unchanged', () => {
    expect(parsed(ANALYZED).insights.map((insight) => insight.kind)).toEqual([
      'slowest',
      'estimate',
      'estimate',
      'discards',
      'hashBatches',
    ])
  })

  it('UI-PLI-008 writes the new titles in Italian with the numbers', async () => {
    await setServerLanguage('it')
    await refreshLocale()
    const plan = parsed(
      analyzedPlan(
        { 'Node Type': 'Gather', 'Workers Planned': 4, 'Workers Launched': 2 },
        { JIT: { Timing: { Total: 1500.5 } }, 'Execution Time': 2000 },
      ),
    )
    const titles = plan.insights.map((insight) => insight.title)
    expect(titles).toContain('Solo 2 worker paralleli su 4 sono partiti')
    expect(titles.find((title) => title.startsWith('La compilazione JIT'))).toBe(
      'La compilazione JIT ha richiesto 1,5 s (75% dell’esecuzione)',
    )
  })
})
