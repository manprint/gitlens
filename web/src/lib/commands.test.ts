import { describe, expect, it } from 'vitest'

import {
  commandQueueFullMessage,
  describeCommandState,
  isTerminalCommandState,
  type CommandState,
} from './commands'

describe('command lifecycle descriptions', () => {
  it('UI-CMD-012 names a full agent queue only for a 429, with the server count', () => {
    const detail = 'the agent already has 20 pending commands; retry once they run or expire'
    expect(
      commandQueueFullMessage({ kind: 'server', status: 429, error: 'command queue full', detail }),
    ).toBe(`Command queue full: ${detail}`)
    expect(
      commandQueueFullMessage({ kind: 'server', status: 503, error: 'unavailable', detail }),
    ).toBeNull()
    expect(commandQueueFullMessage({ kind: 'network', message: 'offline' })).toBeNull()
  })

  it('UI-CMD-003 surfaces the agent rejection reason verbatim', () => {
    expect(describeCommandState('rejected', 'capability denied by target policy')).toContain(
      'capability denied by target policy',
    )
  })

  it('UI-CMD-011 describes a server expiry without naming a TTL the client cannot know', () => {
    // PGLENS_COMMAND_TTL sets the TTL on the server, so the copy must not state five minutes.
    expect(describeCommandState('expired')).toBe(
      'Command expired: its TTL ended before a result was recorded.',
    )
  })

  it('WD-10 does not say no agent claimed a command when the server expired it', () => {
    expect(describeCommandState('expired')).not.toMatch(/no agent claimed/i)
    expect(describeCommandState('expired', 'the agent claimed it')).toBe(
      'Command expired: the agent claimed it',
    )
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
    expect(isTerminalCommandState(null)).toBe(false)
    for (const state of ['done', 'succeeded', 'failed', 'expired', 'rejected']) {
      expect(isTerminalCommandState(state)).toBe(true)
    }
  })

  it('explains failed and rejected commands without an agent reason', () => {
    expect(describeCommandState('failed')).toBe('Command failed without an agent result.')
    expect(describeCommandState('rejected', null)).toBe('Command rejected by the agent.')
  })
})
