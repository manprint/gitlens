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

export function useTimeRange(): TimeRangeControls {
  const [searchParams, setSearchParams] = useSearchParams()
  const range = parseRange(searchParams, new Date())

  function setPreset(preset: RangePreset) {
    setSearchParams(new URLSearchParams({ range: preset }))
  }

  function setRange(nextRange: TimeRange) {
    setSearchParams(toParams(nextRange))
  }

  return { range, setPreset, setRange }
}
