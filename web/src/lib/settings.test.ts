import { describe, expect, it } from 'vitest'

import type { Schemas } from '@/api/types'
import {
  durabilitySeverity,
  isPendingRestart,
  isRedactedArchiveCommand,
  normalizeSettingValue,
} from './settings'

const setting = (overrides: Partial<Schemas['Setting']> = {}): Schemas['Setting'] => ({
  name: 'fsync',
  value: 'on',
  unit: '',
  source: 'postgresql.conf',
  context: 'postmaster',
  pending_restart: 'false',
  first_seen: '2026-08-28T12:00:00Z',
  last_seen: '2026-08-28T12:00:00Z',
  changed_at: '2026-08-28T12:00:00Z',
  ...overrides,
})

describe('settings helpers', () => {
  it('UI-INST-040 normalises byte and time units', () => {
    expect(normalizeSettingValue('64', 'MB')).toBe('64.0 MiB')
    expect(normalizeSettingValue('500', 'ms')).toBe('0.5s')
    expect(normalizeSettingValue('5', 'min')).toBe('5m')
    expect(normalizeSettingValue('on', '')).toBe('on')
  })

  it('UI-INST-041 flags pending restart settings', () => {
    expect(isPendingRestart(setting({ pending_restart: 'true' }))).toBe(true)
    expect(isPendingRestart(setting({ pending_restart: 'false' }))).toBe(false)
  })

  it('UI-INST-042 detects redacted archive commands', () => {
    expect(
      isRedactedArchiveCommand(
        setting({ name: 'archive_command', value: 'wal-g wal-push %p [redacted]' }),
      ),
    ).toBe(true)
    expect(isRedactedArchiveCommand(setting({ name: 'archive_command', value: 'copy %p' }))).toBe(
      false,
    )
  })

  it('UI-INST-043 marks fsync off as critical', () => {
    expect(durabilitySeverity(setting({ name: 'fsync', value: 'off' }))).toBe('critical')
    expect(durabilitySeverity(setting({ name: 'archive_mode', value: 'off' }))).toBe('warning')
  })
})
