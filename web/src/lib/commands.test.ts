import { describe, expect, it } from 'vitest'

import {
  COMMAND_CLIENT_TIMEOUT_MS,
  COMMAND_GRACE_MS,
  COMMAND_TTL_LABEL,
  describeCommandState,
  isTerminalCommandState,
  type CommandState,
} from './commands'

describe('command lifecycle descriptions', () => {
  it('UI-CMD-003 surfaces the agent rejection reason verbatim', () => {
    expect(describeCommandState('rejected', 'capability denied by target policy')).toContain(
      'capability denied by target policy',
    )
  })

  it('UI-CMD-004 names the TTL and grace period used by client expiry', () => {
    expect(describeCommandState('expired')).toContain(COMMAND_TTL_LABEL)
    expect(COMMAND_CLIENT_TIMEOUT_MS).toBeGreaterThan(COMMAND_TTL_LABEL === '5 minutes' ? 0 : 0)
    expect(COMMAND_CLIENT_TIMEOUT_MS).toBe(5 * 60_000 + COMMAND_GRACE_MS)
  })

  it('UI-CMD-005 describes every command state in the union', () => {
    const states: CommandState[] = [
      'pending',
      'claimed',
      'done',
      'succeeded',
      'failed',
      'expired',
      'rejected',
    ]
    for (const state of states) {
      expect(describeCommandState(state, 'agent reason')).toMatch(/Command/)
    }
  })

  it('recognises every terminal state, including the server done state', () => {
    expect(isTerminalCommandState('pending')).toBe(false)
    for (const state of ['done', 'succeeded', 'failed', 'expired', 'rejected']) {
      expect(isTerminalCommandState(state)).toBe(true)
    }
  })
})
