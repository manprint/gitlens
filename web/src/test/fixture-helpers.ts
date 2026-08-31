export const NOW = '2026-08-27T02:00:00.000Z'
export const INSTANCE_ID = '00000000-0000-4000-8000-000000000001'
export const CLUSTER_ID = '9007199254740993'

export const INSTANCE_SUMMARY = {
  instance_id: INSTANCE_ID,
  addr: 'postgres.example.test',
  port: 5432,
  role: 'primary',
  pg_version: 16,
  perm_tier: 'T1',
  last_seen: NOW,
  up: true,
} as const

export const CLUSTER = {
  cluster_id: CLUSTER_ID,
  name: 'production',
  id_source: 'system_identifier',
  primary: INSTANCE_ID,
  instance_count: 1,
  health: 'ok',
  instances: [INSTANCE_SUMMARY],
  standby_count: 0,
  sync_standby_count: 0,
  max_replay_lag_seconds: null,
  topology: [],
} as const

export type DeepPartial<T> = T extends readonly (infer U)[]
  ? readonly DeepPartial<U>[]
  : T extends object
    ? { [K in keyof T]?: DeepPartial<T[K]> }
    : T

export function withOverrides<T>(base: T, overrides: DeepPartial<T>): T {
  if (Array.isArray(base) || Array.isArray(overrides)) {
    return overrides as T
  }

  if (
    typeof base !== 'object' ||
    base === null ||
    typeof overrides !== 'object' ||
    overrides === null
  ) {
    return overrides as T
  }

  const merged = { ...(base as Record<string, unknown>) }
  for (const [key, value] of Object.entries(overrides as Record<string, unknown>)) {
    const current = merged[key]
    merged[key] =
      typeof current === 'object' && current !== null && typeof value === 'object' && value !== null
        ? withOverrides(current, value)
        : value
  }
  return merged as T
}
