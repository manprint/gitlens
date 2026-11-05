import { describe, expect, it } from 'vitest'

import type { AlertRule } from '@/api/alerts'

import { thresholdText } from './ruleThreshold'

function rule(id: string, threshold: number | undefined): AlertRule {
  return { id, threshold } as AlertRule
}

describe('thresholdText', () => {
  it.each([
    ['conn.near_max', 0.8, 'above 80% of max_connections'],
    ['disk.free_low', 0.15, 'below 15% free'],
    ['replica.lag_high', 30, 'above 30 s'],
    ['clock_skew', 30, 'above 30 s'],
    ['conn.idle_in_transaction', 300, 'above 300 s'],
    ['txn.long_running', 900, 'above 900 s'],
    ['txn.wraparound_risk', 1e9, 'above 1,000,000,000 transactions'],
    ['db.deadlock_rate', 0, 'any deadlock'],
    ['db.deadlock_rate', 0.1, 'above 0.1 deadlocks per second'],
    ['replica.no_sync_standby', 1, 'below 1 standbys'],
    ['failover_detected', 0, 'on the event'],
    ['some.custom_rule', 7, '7'],
  ])('%s at %s reads %s', (id, threshold, text) => {
    expect(thresholdText(rule(id, threshold))).toBe(text)
  })

  it('shows a dash without a threshold', () => {
    expect(thresholdText(rule('some.custom_rule', undefined))).toBe('—')
  })
})
