import { useSearchParams } from 'react-router-dom'

import { useTimeRange } from '@/hooks/useTimeRange'
import {
  type ComparePreset,
  parseInstant,
  type Period,
  periodB,
  presetPeriods,
  readComparePreset,
} from '@/lib/compare'

export interface ComparePeriods {
  preset: ComparePreset
  /** Null while a custom period A is incomplete. */
  a: Period | null
  b: Period
}

/** Period B is the page range; period A comes from `compare`, `a_from`, `a_to` in the URL. */
export function useComparePeriods(): ComparePeriods {
  const [searchParams] = useSearchParams()
  const { range } = useTimeRange()
  const preset = readComparePreset(searchParams.get('compare'))
  const b = periodB(range.from, range.to)
  const custom = {
    ...withDate('from', parseInstant(searchParams.get('a_from'))),
    ...withDate('to', parseInstant(searchParams.get('a_to'))),
  }
  return { preset, a: presetPeriods(preset, b.from, b.to, custom), b }
}

function withDate<K extends 'from' | 'to'>(key: K, value: Date | undefined) {
  return value === undefined ? {} : ({ [key]: value } as Record<K, Date>)
}
