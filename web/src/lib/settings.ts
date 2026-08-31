import type { Schemas } from '@/api/types'
import { formatBytes, formatDuration } from '@/lib/format'

export type DurabilitySeverity = 'critical' | 'warning' | null

export const DURABILITY_SETTING_NAMES = [
  'fsync',
  'full_page_writes',
  'archive_mode',
  'wal_level',
  'synchronous_commit',
] as const

const BYTE_MULTIPLIERS: Record<string, number> = {
  '8kB': 8 * 1024,
  kB: 1024,
  MB: 1024 ** 2,
  GB: 1024 ** 3,
}

const DURATION_MULTIPLIERS: Record<string, number> = {
  ms: 1 / 1000,
  s: 1,
  min: 60,
}

function numericValue(value: string | null): number | null {
  if (value === null || value.trim() === '') return null
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : null
}

export function normalizeSettingValue(value: string | null, unit: string): string | null {
  if (value === null) return null

  const parsed = numericValue(value)
  if (parsed !== null && BYTE_MULTIPLIERS[unit]) {
    return formatBytes(parsed * BYTE_MULTIPLIERS[unit])
  }
  if (parsed !== null && DURATION_MULTIPLIERS[unit]) {
    return formatDuration(parsed * DURATION_MULTIPLIERS[unit])
  }

  return unit ? `${value} ${unit}` : value
}

export function isPendingRestart(setting: Schemas['Setting']): boolean {
  return setting.pending_restart.trim().toLowerCase() === 'true'
}

export function isRedactedArchiveCommand(setting: Schemas['Setting']): boolean {
  return setting.name === 'archive_command' && setting.value?.includes('[redacted]') === true
}

export function durabilitySeverity(setting: Schemas['Setting']): DurabilitySeverity {
  const value = setting.value?.trim().toLowerCase()
  if (value === undefined) return null

  if (
    (setting.name === 'fsync' && value === 'off') ||
    (setting.name === 'full_page_writes' && value === 'off')
  ) {
    return 'critical'
  }

  if (
    (setting.name === 'archive_mode' && value === 'off') ||
    (setting.name === 'wal_level' && value === 'minimal') ||
    (setting.name === 'synchronous_commit' && value === 'off')
  ) {
    return 'warning'
  }

  return null
}

export function settingDisplayValue(setting: Schemas['Setting']): string | null {
  return normalizeSettingValue(setting.value, setting.unit)
}
