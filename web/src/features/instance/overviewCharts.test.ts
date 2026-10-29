import { describe, expect, it } from 'vitest'

import { metricKind } from '@/lib/chartStep'
import { isWorkloadSeries, WORKLOAD_SERIES } from '@/lib/workloadSeries'

import {
  cardStepSeconds,
  OVERVIEW_GROUPS,
  OVERVIEW_NAV_LINKS,
  overviewStepSeconds,
  workloadCardKind,
  type ChartCardSpec,
  type WorkloadCardSpec,
} from './overviewCharts'

/**
 * The `pg_*` metrics the agent stores (internal/check: database_stats.go,
 * activity.go, wal.go, checkpointer.go), with the kind the agent gives them.
 */
const AGENT_METRICS: Record<string, 'rate' | 'gauge'> = {
  pg_xact_commit_total: 'rate',
  pg_xact_rollback_total: 'rate',
  pg_connections_used: 'gauge',
  pg_connections_limit: 'gauge',
  pg_deadlocks_total: 'rate',
  pg_conflicts_total: 'rate',
  pg_blks_hit_total: 'rate',
  pg_blks_read_total: 'rate',
  pg_temp_files_total: 'rate',
  pg_temp_bytes_total: 'rate',
  pg_wal_wal_bytes: 'rate',
  pg_wal_wal_records: 'rate',
  pg_wal_wal_fpi: 'rate',
  pg_checkpoints_timed_total: 'rate',
  pg_checkpoints_requested_total: 'rate',
  pg_checkpoint_write_time_ms_total: 'rate',
  pg_checkpoint_sync_time_ms_total: 'rate',
}

/** Stored without a database: requesting them with one filters every row out. */
const INSTANCE_WIDE = new Set([
  'pg_connections_used',
  'pg_connections_limit',
  'pg_wal_wal_bytes',
  'pg_wal_wal_records',
  'pg_wal_wal_fpi',
  'pg_checkpoints_timed_total',
  'pg_checkpoints_requested_total',
  'pg_checkpoint_write_time_ms_total',
  'pg_checkpoint_sync_time_ms_total',
])

const HOUR = 3_600_000
const TO = new Date('2026-08-27T12:00:00Z')
const RANGES = [0.25, 1, 6, 24, 24 * 7].map((hours) => new Date(TO.getTime() - hours * HOUR))

const allCards: ChartCardSpec[] = OVERVIEW_GROUPS.flatMap((group) => [...group.cards])
const workloadCards = allCards.filter(
  (card): card is WorkloadCardSpec => card.source === 'workload',
)

describe('OVERVIEW_GROUPS', () => {
  it('OVC-001 lists the groups workload, memory-io, wal-checkpoints, queries, tables-indexes in order', () => {
    expect(OVERVIEW_GROUPS.map((group) => group.id)).toEqual([
      'workload',
      'memory-io',
      'wal-checkpoints',
      'queries',
      'tables-indexes',
    ])
    for (const group of OVERVIEW_GROUPS) {
      expect(group.title).not.toBe('')
      expect(group.description).not.toBe('')
      expect(group.cards.length).toBeGreaterThan(0)
    }
  })

  it('OVC-001 nav lists the five groups then the Host section', () => {
    expect(OVERVIEW_NAV_LINKS.map((link) => link.id)).toEqual([
      'workload',
      'memory-io',
      'wal-checkpoints',
      'queries',
      'tables-indexes',
      'host-metrics',
    ])
    expect(OVERVIEW_NAV_LINKS.at(-1)?.label).toBe('Host')
  })

  it('OVC-002 names only metrics the agent stores, with their kind and scope', () => {
    for (const card of allCards) {
      if (card.source !== 'metrics') continue
      for (const line of card.metrics) {
        expect(AGENT_METRICS, `${card.id}: ${line.metric}`).toHaveProperty([line.metric])
        expect(metricKind(line.metric), line.metric).toBe(AGENT_METRICS[line.metric])
        expect(line.scope, line.metric).toBe(
          INSTANCE_WIDE.has(line.metric) ? 'instance' : 'database',
        )
      }
    }
  })

  it('OVC-002 names only whitelisted workload series of the card source', () => {
    for (const card of workloadCards) {
      for (const line of card.series) {
        expect(isWorkloadSeries(card.workloadSource, line.series), line.series).toBe(true)
      }
      // The server answers at most 8 series per request.
      expect(card.series.length).toBeLessThanOrEqual(8)
    }
    expect(isWorkloadSeries('tables', 'calls')).toBe(false)
    expect(WORKLOAD_SERIES.indexes).toContain('index_bytes')
  })

  it('OVC-002 card ids are unique, line names unique per card, at most two units per card', () => {
    const ids = allCards.map((card) => card.id)
    expect(new Set(ids).size).toBe(ids.length)
    for (const card of allCards) {
      const lines = card.source === 'metrics' ? card.metrics : card.series
      const labels = lines.map((line) => line.label)
      expect(new Set(labels).size, card.id).toBe(labels.length)
      const units = new Set(lines.map((line) => line.unit ?? card.unit))
      expect(units.size, card.id).toBeLessThanOrEqual(2)
    }
  })

  it('OVC-002 every group fills whole grid rows (wide cards count twice)', () => {
    for (const group of OVERVIEW_GROUPS) {
      const cells = group.cards.reduce((sum, card) => sum + (card.wide ? 2 : 1), 0)
      expect(cells % 2, group.id).toBe(0)
    }
  })

  it('OVC-002 the cache hit ratio card derives from the hit and read counters', () => {
    const card = allCards.find((candidate) => candidate.id === 'cache-hit-ratio')
    expect(card?.source === 'metrics' && card.derive).toBe('cache-hit-ratio')
    expect(card?.source === 'metrics' && card.metrics.map((line) => line.metric)).toEqual([
      'pg_blks_hit_total',
      'pg_blks_read_total',
    ])
  })
})

describe('card steps', () => {
  it('OVC-007 workload cards never step below the server minimum of two samples', () => {
    for (const from of RANGES) {
      for (const card of workloadCards) {
        const sample = card.workloadSource === 'statements' ? 60 : 300
        expect(cardStepSeconds(card, from, TO), card.id).toBeGreaterThanOrEqual(2 * sample)
      }
    }
  })

  it('OVC-007 statements cards never step below 60 s', () => {
    for (const from of RANGES) {
      for (const card of workloadCards.filter((item) => item.workloadSource === 'statements')) {
        expect(cardStepSeconds(card, from, TO), card.id).toBeGreaterThanOrEqual(60)
      }
    }
  })

  it('OVC-008 tables and indexes cards never step below 300 s', () => {
    for (const from of RANGES) {
      for (const card of workloadCards.filter((item) => item.workloadSource !== 'statements')) {
        expect(cardStepSeconds(card, from, TO), card.id).toBeGreaterThanOrEqual(300)
      }
    }
  })

  it('OVC-008 a card of gauges uses the gauge step, one rate makes it a rate', () => {
    const dead = workloadCards.find((card) => card.id === 'dead-tuples')
    const scans = workloadCards.find((card) => card.id === 'scans')
    expect(dead && workloadCardKind(dead)).toBe('gauge')
    expect(scans && workloadCardKind(scans)).toBe('rate')
    const hourAgo = new Date(TO.getTime() - HOUR)
    // Two samples of 5 min: the server minimum, so the label never flips.
    expect(dead && cardStepSeconds(dead, hourAgo, TO)).toBe(600)
    // A rate needs two samples: 600 s, the next ladder step is 15 minutes.
    expect(scans && cardStepSeconds(scans, hourAgo, TO)).toBe(900)
  })

  it('OVC-007 metric cards step at the sampling of their slowest line', () => {
    const hourAgo = new Date(TO.getTime() - HOUR)
    expect(overviewStepSeconds('pg_connections_used', hourAgo, TO)).toBe(15)
    expect(overviewStepSeconds('pg_xact_commit_total', hourAgo, TO)).toBe(60)
    expect(overviewStepSeconds('pg_wal_wal_bytes', hourAgo, TO)).toBe(60)
    const connections = allCards.find((card) => card.id === 'connections')
    expect(connections && cardStepSeconds(connections, hourAgo, TO)).toBe(15)
  })
})
