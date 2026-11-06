import { describe, expect, it } from 'vitest'

import { ADVISOR_RULE_IDS, ALERT_RULE_IDS, needText, needsText, ruleText } from './ruleText'

const FIXTURE_RULE_IDS = [
  'config.checkpoints_too_frequent',
  'index.fk_unindexed',
  'index.invalid',
  'index.unused',
  'query.slow_mean',
  'table.dead_tuples_high',
  'txn.long_running',
  'disk.free_low',
  'archive.failing',
  'txn.wraparound_risk',
]

describe('ruleText', () => {
  it('knows every advisor rule and every alert rule', () => {
    expect(ADVISOR_RULE_IDS).toHaveLength(72)
    expect(ALERT_RULE_IDS).toHaveLength(19)
    expect(ALERT_RULE_IDS).toContain('replica.redundancy_reduced')
  })

  it.each([...ADVISOR_RULE_IDS, ...ALERT_RULE_IDS, ...FIXTURE_RULE_IDS])(
    'has one plain sentence for %s',
    (id) => {
      const text = ruleText(id)
      expect(text).toBeDefined()
      expect(text).toMatch(/^[A-Za-z].*\.$/)
      expect(text).not.toContain(id)
    },
  )

  it('returns undefined for an unknown id', () => {
    expect(ruleText('no.such_rule')).toBeUndefined()
  })

  it('maps needs to plain words', () => {
    expect(needText('Statements')).toBe('query statistics')
    expect(needText('pg_checkpoints_requested_ratio')).toBe('pg checkpoints requested ratio')
    expect(needsText(['settings', 'Siblings'])).toBe(
      'server settings, other instances of the cluster',
    )
    expect(needsText([])).toBe('None')
  })
})
