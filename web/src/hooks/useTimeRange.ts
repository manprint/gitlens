import { useSearchParams } from 'react-router-dom'

import {
  parseRange,
  snapToInterval,
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

// Pass the refresh interval of the queries that read this window, so the
// relative range moves once per poll. The default is only for callers that
// build no query from the window.
export function useTimeRange(refreshMs = 1_000): TimeRangeControls {
  const [searchParams, setSearchParams] = useSearchParams()
  const range = parseRange(searchParams, snapToInterval(new Date(), refreshMs))

  function setPreset(preset: RangePreset) {
    setSearchParams(new URLSearchParams({ range: preset }))
  }

  function setRange(nextRange: TimeRange) {
    setSearchParams(toParams(nextRange))
  }

  return { range, setPreset, setRange }
}
