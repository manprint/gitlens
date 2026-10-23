import { useSearchParams } from 'react-router-dom'

import {
  parseRange,
  toParams,
  type RangePreset,
  type TimeRange,
  type TimeRangeResult,
} from '@/lib/timerange'

export interface TimeRangeControls {
  range: TimeRangeResult
  setPreset: (preset: RangePreset) => void
  setRange: (range: TimeRange) => void
}

const RANGE_KEYS = ['range', 'from', 'to'] as const

// A range change replaces only the range keys. The database filter and the
// ASH drill-down keys stay, so changing the window does not widen the view.
function withRangeParams(current: URLSearchParams, rangeParams: URLSearchParams): URLSearchParams {
  const next = new URLSearchParams(current)
  for (const key of RANGE_KEYS) next.delete(key)
  for (const [key, value] of rangeParams) next.set(key, value)
  return next
}

export function useTimeRange(): TimeRangeControls {
  const [searchParams, setSearchParams] = useSearchParams()
  const range = parseRange(searchParams, new Date())

  function setPreset(preset: RangePreset) {
    setSearchParams((current) => withRangeParams(current, new URLSearchParams({ range: preset })))
  }

  function setRange(nextRange: TimeRange) {
    setSearchParams((current) => withRangeParams(current, toParams(nextRange)))
  }

  return { range, setPreset, setRange }
}
