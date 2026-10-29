import { describe, expect, it } from 'vitest'

import { metricKind, SAMPLE_SECONDS, stepFor } from './chartStep'
import { chooseStep, stepMilliseconds } from './timerange'

const HOUR = 60 * 60 * 1_000
const to = new Date(Date.UTC(2026, 9, 29, 10, 41))
const before = (ms: number) => new Date(to.getTime() - ms)

describe('chart step', () => {
  it('CSTEP-001 queries a gauge at its sampling interval over 1 h', () => {
    expect(stepFor('gauge', before(HOUR), to, 30)).toBe(30)
  })

  it('CSTEP-002 queries a rate at one minute over 1 h when sampled every 30 s', () => {
    expect(stepFor('rate', before(HOUR), to, 30)).toBe(60)
  })

  it('CSTEP-003 keeps the base step of a 24 h range for a rate, never below one minute', () => {
    const from = before(24 * HOUR)
    const base = stepMilliseconds(chooseStep(from, to)) / 1_000
    const step = stepFor('rate', from, to, 30)
    expect(step).toBe(Math.max(base, 60))
    expect(step).toBe(300)
    expect(step).toBeGreaterThanOrEqual(60)
  })

  it('CSTEP-004 needs two samples per bucket for a rate sampled every 60 s', () => {
    expect(stepFor('rate', before(HOUR), to, 60)).toBe(300)
  })

  it('CSTEP-005 falls back to the largest ladder step for a huge range', () => {
    expect(stepFor('rate', before(10 * 365 * 24 * HOUR), to, 30)).toBe(86_400)
    expect(stepFor('gauge', before(HOUR), to, 7 * 86_400)).toBe(86_400)
  })

  it('CSTEP-006 classifies metrics as rate or gauge', () => {
    const table: Record<string, 'rate' | 'gauge'> = {
      pg_xact_commit_total: 'rate',
      pg_wal_wal_bytes: 'rate',
      pg_wal_wal_records: 'rate',
      pg_wal_wal_fpi: 'rate',
      pg_wal_wal_buffers_full: 'rate',
      pg_wal_wal_write: 'rate',
      pg_wal_wal_sync: 'rate',
      pg_wal_wal_write_time: 'rate',
      pg_wal_wal_sync_time: 'rate',
      pg_wal_lsn_bytes: 'rate',
      pg_wal_wal_fpi_bytes: 'rate',
      pg_wal_some_future_count: 'rate',
      pg_table_seq_scan: 'rate',
      pg_table_n_tup_hot_upd: 'rate',
      pg_table_autovacuum_count: 'rate',
      pg_index_idx_scan: 'rate',
      pg_index_idx_blks_hit: 'rate',
      pg_index_index_bytes: 'gauge',
      pg_index_is_unique: 'gauge',
      pg_blks_read_total: 'rate',
      pg_stat_statements_total_exec_time_ms: 'rate',
      pg_io_reads: 'rate',
      pg_io_timing_enabled: 'gauge',
      pg_connections_used: 'gauge',
      pg_blks_hit_ratio: 'gauge',
      host_cpu_used_ratio: 'gauge',
      host_mem_available_bytes: 'gauge',
      pg_wal_stats_columns_available: 'gauge',
      some_unknown_metric: 'gauge',
    }
    for (const [name, kind] of Object.entries(table)) expect(metricKind(name), name).toBe(kind)
  })

  it('CSTEP-007 every sampling interval is positive', () => {
    expect(Object.keys(SAMPLE_SECONDS).length).toBeGreaterThan(0)
    for (const [check, seconds] of Object.entries(SAMPLE_SECONDS)) {
      expect(seconds, check).toBeGreaterThan(0)
    }
  })
})
