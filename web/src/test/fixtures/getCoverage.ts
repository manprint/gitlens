import type { Schemas } from '@/api/types'

import { INSTANCE_ID } from '../fixture-helpers'

type Coverage = Schemas['CoverageResponse']

export const responseStatus = 200

/**
 * Three minutes after the agent started: two checks reported, one failed and
 * one still pending, expected by the start plus its 600 s first-run bound plus
 * 30 s.
 */
export const base: Coverage = {
  instance_id: INSTANCE_ID,
  agent_started_at: '2026-08-27T01:57:00Z',
  generated_at: '2026-08-27T02:00:00Z',
  checks: [
    {
      check: 'activity',
      interval_seconds: 15,
      state: 'reported',
      first_result_at: '2026-08-27T01:57:07Z',
      last_result_at: '2026-08-27T01:59:52Z',
      expected_by: null,
    },
    {
      check: 'database_stats',
      interval_seconds: 30,
      state: 'reported',
      first_result_at: '2026-08-27T01:57:12Z',
      last_result_at: '2026-08-27T01:59:42Z',
      expected_by: null,
    },
    {
      check: 'locks',
      interval_seconds: 30,
      state: 'failed',
      first_result_at: '2026-08-27T01:57:20Z',
      last_result_at: '2026-08-27T01:59:50Z',
      expected_by: null,
    },
    {
      check: 'settings',
      interval_seconds: 3600,
      state: 'pending',
      first_result_at: null,
      last_result_at: null,
      expected_by: '2026-08-27T02:07:30Z',
    },
  ],
  reported: 2,
  total: 4,
}

/** Every check has reported. */
export const complete: Coverage = {
  ...base,
  checks: base.checks.map((check) =>
    check.state === 'reported'
      ? check
      : {
          ...check,
          state: 'reported',
          first_result_at: '2026-08-27T01:59:00Z',
          last_result_at: '2026-08-27T01:59:00Z',
          expected_by: null,
        },
  ),
  reported: 4,
}

/** An agent that predates agent_started_at and check_intervals. */
export const oldAgent: Coverage = {
  instance_id: INSTANCE_ID,
  agent_started_at: null,
  generated_at: '2026-08-27T02:00:00Z',
  checks: [
    {
      check: 'activity',
      interval_seconds: null,
      state: 'reported',
      first_result_at: '2026-08-20T00:00:00Z',
      last_result_at: '2026-08-27T01:59:52Z',
      expected_by: null,
    },
  ],
  reported: 1,
  total: 1,
}
