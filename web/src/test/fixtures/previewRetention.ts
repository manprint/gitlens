import { withOverrides } from '../fixture-helpers'
import type { DeepPartial } from '../fixture-helpers'

export const responseStatus = 200

/** A shortening of metrics_ash to 14 days: 64 chunks, 40 MiB. */
export const base = {
  family: 'metrics_ash' as const,
  days: 14,
  chunks_to_drop: 64,
  bytes_to_free: 41_943_040,
  oldest_chunk_at: '2026-10-01T00:00:00Z',
  newest_dropped_chunk_end: '2026-10-16T06:00:00Z',
}

/** A lengthening drops nothing. */
export const lengthening = {
  ...base,
  days: 60,
  chunks_to_drop: 0,
  bytes_to_free: 0,
  newest_dropped_chunk_end: null,
}

export function makePreviewRetention(overrides: DeepPartial<typeof base> = {}) {
  return withOverrides(base, overrides)
}
