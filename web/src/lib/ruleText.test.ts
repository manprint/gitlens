import { describe, expect, it } from 'vitest'

import { rules as itRules } from '@/i18n/domain/it/rules'

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
    expect(ADVISOR_RULE_IDS).toHaveLength(73)
    expect(ALERT_RULE_IDS).toHaveLength(23)
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

  it('UI-MXW-001 has its own multixact wraparound sentence in en and it', () => {
    const id = 'txn.multixact_wraparound_risk'
    expect(ruleText(id)).toMatch(/multixact/)
    expect(itRules[id]).toMatch(/multixact/)
    expect(ruleText(id)).not.toBe(ruleText('txn.wraparound_risk'))
    expect(itRules[id]).not.toBe(itRules['txn.wraparound_risk'])
  })

  it('UI-PGL-005 has en and it sentences for the four log rules', () => {
    for (const id of ['log.panic', 'log.corruption', 'log.disk_full', 'log.out_of_memory']) {
      expect(ALERT_RULE_IDS).toContain(id)
      expect(ruleText(id)).toMatch(/PostgreSQL log/)
      expect(itRules[id as keyof typeof itRules]).toMatch(/log di PostgreSQL/)
    }
  })

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
