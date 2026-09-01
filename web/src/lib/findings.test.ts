import { describe, expect, it } from 'vitest'

import type { AdvisorRule, FindingRecord } from './findings'
import {
  groupByScope,
  joinCatalogue,
  muteExpiry,
  rankFindings,
  summariseFindings,
} from './findings'

const baseFinding: FindingRecord = {
  finding_id: 'finding-001',
  rule_id: 'rule-001',
  severity: 'info',
  state: 'open',
  scope: 'cluster',
  title: 'Example finding',
}

function finding(overrides: Partial<FindingRecord> = {}): FindingRecord {
  return { ...baseFinding, ...overrides }
}

function rule(overrides: Partial<AdvisorRule> = {}): AdvisorRule {
  return {
    id: 'rule-001',
    severity: 'warning',
    scope: 'cluster',
    needs: ['replication_lag'],
    min_tier: 'T0',
    ...overrides,
  }
}

describe('finding helpers', () => {
  it('UI-FIND-001 ranks by severity then state then scope then rule id', () => {
    const ranked = rankFindings([
      finding({ finding_id: 'resolved', severity: 'critical', state: 'resolved' }),
      finding({ finding_id: 'warning-muted', severity: 'warning', state: 'muted' }),
      finding({
        finding_id: 'warning-instance-z',
        severity: 'warning',
        scope: 'instance',
        rule_id: 'z',
      }),
      finding({
        finding_id: 'warning-instance-a',
        severity: 'warning',
        scope: 'instance',
        rule_id: 'a',
      }),
      finding({ finding_id: 'warning-cluster', severity: 'warning', scope: 'cluster' }),
      finding({ finding_id: 'info', severity: 'info' }),
      finding({ finding_id: 'critical-open', severity: 'critical', state: 'open' }),
    ])

    expect(ranked.map((item) => item.finding_id)).toEqual([
      'critical-open',
      'resolved',
      'warning-cluster',
      'warning-instance-a',
      'warning-instance-z',
      'warning-muted',
      'info',
    ])
  })

  it('UI-FIND-002 ranking is stable for identical inputs', () => {
    const input = [finding({ finding_id: 'second' }), finding({ finding_id: 'first' })]
    const ranked = rankFindings(input)

    expect(ranked).not.toBe(input)
    expect(ranked.map((item) => item.finding_id)).toEqual(['second', 'first'])
    expect(input.map((item) => item.finding_id)).toEqual(['second', 'first'])
  })

  it('UI-FIND-003 summarise counts open only and reports degraded separately', () => {
    expect(
      summariseFindings([
        finding({ severity: 'critical' }),
        finding({ severity: 'warning', finding_id: 'warning' }),
        finding({ severity: 'info', state: 'resolved', finding_id: 'resolved' }),
        finding({ severity: 'warning', state: 'degraded', finding_id: 'degraded' }),
        finding({ severity: 'info', state: 'muted', finding_id: 'muted' }),
      ]),
    ).toEqual({ critical: 1, warning: 1, info: 0, degraded: 1 })
  })

  it('UI-FIND-004 joinCatalogue attaches needs and min_tier', () => {
    const catalogueRule = rule({ needs: ['history_7d'], min_tier: 'T1' })
    const [joined] = joinCatalogue([finding()], [catalogueRule])

    expect(joined).toMatchObject({
      catalogue: catalogueRule,
      catalogueMissing: false,
      needs: ['history_7d'],
      min_tier: 'T1',
    })
  })

  it('UI-FIND-005 a finding whose rule is missing from the catalogue is flagged, not dropped', () => {
    const [joined] = joinCatalogue([finding({ rule_id: 'missing-rule' })], [])

    expect(joined).toMatchObject({
      catalogue: null,
      catalogueMissing: true,
      finding_id: 'finding-001',
      rule_id: 'missing-rule',
      needs: [],
      min_tier: null,
    })
  })

  it('UI-FIND-006 muteExpiry returns null for an unmuted finding', () => {
    expect(
      muteExpiry(
        finding({ state: 'open', muted_until: '2030-01-01T01:00:00Z' }),
        new Date('2030-01-01T00:00:00Z'),
      ),
    ).toBeNull()
  })

  it('UI-FIND-007 muteExpiry uses the frozen clock', () => {
    const muted = finding({ state: 'muted', muted_until: '2030-01-01T01:00:00Z' })

    expect(muteExpiry(muted, new Date('2030-01-01T00:00:00Z'))).toBe(3600)
    expect(muteExpiry(muted, new Date('2030-01-01T00:30:00Z'))).toBe(1800)
  })

  it('groups cluster and instance findings while retaining unknown scopes', () => {
    const cluster = finding({ finding_id: 'cluster' })
    const instance = finding({ finding_id: 'instance', scope: 'instance' })
    const unknown = finding({ finding_id: 'unknown', scope: 'global' })

    expect(groupByScope([cluster, instance, unknown])).toEqual({
      cluster: [cluster],
      instance: [instance],
      global: [unknown],
    })
  })
})
