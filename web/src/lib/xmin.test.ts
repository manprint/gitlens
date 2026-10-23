import { describe, expect, it } from 'vitest'

import { type ActivityMetrics } from './activity'
import {
  XMIN_NONE_TEXT,
  XMIN_PENDING_TEXT,
  xminHorizonState,
  xminIdentityText,
  type XminHorizon,
} from './xmin'

const holder: XminHorizon = {
  holder_kind: 'session',
  identity: '4242',
  age_xids: 910,
  age_seconds: 33.5,
  sampled_at: '2026-10-23T10:00:00Z',
}

const withGauges: ActivityMetrics = {
  pg_xmin_horizon_age_xids: [
    { ts: '2026-10-23T10:00:00Z', value: 0, labels: { holder_kind: 'slot' } },
  ],
}

describe('xminHorizonState', () => {
  it('UI-XMIN-001 names the holder the latest sample reported', () => {
    expect(xminHorizonState(withGauges, holder)).toEqual({ state: 'holder', holder })
    expect(xminHorizonState({}, holder)).toEqual({ state: 'holder', holder })
  })

  it('UI-XMIN-002 says no holder holds the horizon when the sample has the gauges but no holder', () => {
    expect(xminHorizonState(withGauges, null)).toEqual({ state: 'none' })
    expect(xminHorizonState(withGauges, undefined)).toEqual({ state: 'none' })
  })

  it('UI-XMIN-003 says the holder is unknown without an activity snapshot', () => {
    expect(xminHorizonState({}, null)).toEqual({ state: 'unknown' })
    expect(xminHorizonState({ pg_backends: [] }, undefined)).toEqual({ state: 'unknown' })
  })

  it('UI-XMIN-004 keeps the exact pending wording of the spec', () => {
    expect(XMIN_PENDING_TEXT).toBe('xmin holder unknown: no activity snapshot')
    expect(XMIN_NONE_TEXT).toContain('holds an xmin in the latest activity sample')
  })
})

describe('xminIdentityText', () => {
  it('UI-XMIN-005 shows an empty identity as (none), and any other as it is', () => {
    expect(xminIdentityText({ ...holder, identity: '' })).toBe('(none)')
    expect(xminIdentityText({ ...holder, holder_kind: 'standby', identity: 'replica_1' })).toBe(
      'replica_1',
    )
  })
})
