import { describe, expect, it } from 'vitest'

import type { Schemas } from '@/api/types'
import { matchSilence, rankAlerts, silenceWindow, summariseAlerts } from './alerts'

type Alert = Schemas['Alert']
type Silence = Schemas['Silence']

const instant = '2030-01-01T00:00:00Z'

function alert(overrides: Partial<Alert> = {}): Alert {
  return {
    alert_key: 'replication-lag',
    rule_id: 'replication-lag',
    severity: 'warning',
    state: 'firing',
    datname: 'app',
    labels: { team: 'database' },
    value: 12,
    summary: 'Replication lag is high',
    started_at: instant,
    last_eval_at: instant,
    resolved_at: null,
    suppressed: false,
    ...overrides,
  }
}

function silence(overrides: Partial<Silence> = {}): Silence {
  return {
    silence_id: '00000000-0000-4000-8000-000000000001',
    matchers: [{ name: 'rule_id', value: 'replication-lag' }],
    reason: 'maintenance',
    starts_at: instant,
    ends_at: '2030-01-01T02:00:00Z',
    ...overrides,
  }
}

describe('alerts derivation', () => {
  it('UI-ALERT-001 ranks firing before resolved and unsuppressed before suppressed', () => {
    const resolved = alert({ alert_key: 'a', state: 'resolved' })
    const suppressed = alert({ alert_key: 'b', suppressed: true })
    const unsuppressed = alert({ alert_key: 'c' })

    expect(rankAlerts([resolved, suppressed, unsuppressed]).map(({ alert_key }) => alert_key)).toEqual([
      'c',
      'b',
      'a',
    ])
  })

  it('UI-ALERT-002 summarise reports suppressed separately from firing', () => {
    expect(
      summariseAlerts([
        alert({ severity: 'critical' }),
        alert({ alert_key: 'muted', severity: 'warning', suppressed: true }),
        alert({ alert_key: 'resolved', state: 'resolved', severity: 'info' }),
      ]),
    ).toEqual({
      firing: { critical: 1, warning: 0, info: 0 },
      suppressed: 1,
      resolved: 1,
    })
  })

  it('UI-ALERT-003 matchSilence selects on an exact name and value', () => {
    expect(matchSilence(alert({ labels: { team: 'database' } }), silence({ matchers: [{ name: 'team', value: 'database' }] }))).toBe(true)
  })

  it('UI-ALERT-004 matchSilence does not select on a partial value', () => {
    expect(matchSilence(alert(), silence({ matchers: [{ name: 'rule_id', value: 'replication' }] }))).toBe(false)
  })

  it('UI-ALERT-005 silenceWindow classifies pending, active and expired against the frozen clock', () => {
    const current = new Date(instant)
    const window = silence({
      starts_at: '2029-12-31T23:30:00Z',
      ends_at: '2030-01-01T00:30:00Z',
    })

    expect(silenceWindow({ ...window, starts_at: '2030-01-01T01:00:00Z' }, current)).toEqual({
      state: 'pending',
      remainingSeconds: 3600,
    })
    expect(silenceWindow(window, current)).toEqual({ state: 'active', remainingSeconds: 1800 })
    expect(silenceWindow({ ...window, ends_at: '2029-12-31T23:59:59Z' }, current)).toEqual({
      state: 'expired',
      remainingSeconds: 0,
    })
  })

  it('UI-ALERT-006 cluster_id is compared as a string', () => {
    const high = alert({ alert_key: 'same', cluster_id: '10' })
    const low = alert({ alert_key: 'same', cluster_id: '2' })

    expect(rankAlerts([low, high]).map(({ cluster_id }) => cluster_id)).toEqual(['10', '2'])
  })
})
