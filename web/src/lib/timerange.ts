export const DEFAULT_RANGE = '1h' as const
export const MAX_RANGE_MS = 30 * 24 * 60 * 60 * 1_000

export const RANGE_PRESETS = {
  '15m': { durationMs: 15 * 60 * 1_000, label: 'Last 15 minutes' },
  '1h': { durationMs: 60 * 60 * 1_000, label: 'Last hour' },
  '6h': { durationMs: 6 * 60 * 60 * 1_000, label: 'Last 6 hours' },
  '24h': { durationMs: 24 * 60 * 60 * 1_000, label: 'Last 24 hours' },
  '7d': { durationMs: 7 * 24 * 60 * 60 * 1_000, label: 'Last 7 days' },
} as const

export type RangePreset = keyof typeof RANGE_PRESETS
export const RANGE_PRESET_ORDER: readonly RangePreset[] = ['15m', '1h', '6h', '24h', '7d']

export const STEP_LADDER = [
  { milliseconds: 15_000, value: '15s' },
  { milliseconds: 30_000, value: '30s' },
  { milliseconds: 60_000, value: '1m' },
  { milliseconds: 5 * 60_000, value: '5m' },
  { milliseconds: 15 * 60_000, value: '15m' },
  { milliseconds: 60 * 60_000, value: '1h' },
  { milliseconds: 6 * 60 * 60_000, value: '6h' },
  { milliseconds: 24 * 60 * 60_000, value: '1d' },
] as const

export interface RelativeTimeRange {
  from: Date
  to: Date
  kind: 'relative'
  label: string
  preset: RangePreset
}

export interface AbsoluteTimeRange {
  from: Date
  to: Date
  kind: 'absolute'
  label: string
}

export type TimeRange = RelativeTimeRange | AbsoluteTimeRange

export type ValidRelativeTimeRange = RelativeTimeRange & {
  valid: true
  fallback: false
}

export type ValidAbsoluteTimeRange = AbsoluteTimeRange & {
  valid: true
  fallback: false
}

export type ValidTimeRange = ValidRelativeTimeRange | ValidAbsoluteTimeRange

export type InvalidTimeRange = RelativeTimeRange & {
  valid: false
  fallback: true
  parameter: string
  reason: string
}

export type TimeRangeResult = ValidTimeRange | InvalidTimeRange
export type RangeParams = URLSearchParams | Readonly<Record<string, string | undefined>>

const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/

function paramValue(params: RangeParams, key: string): string | null {
  if (params instanceof URLSearchParams) return params.get(key)
  return params[key] ?? null
}

function cloneDate(date: Date): Date {
  return new Date(date.getTime())
}

function checkedNow(now: Date): Date {
  if (!Number.isFinite(now.getTime())) throw new RangeError('now must be a valid Date')
  return cloneDate(now)
}

function relativeRange(preset: RangePreset, now: Date): ValidRelativeTimeRange {
  const end = checkedNow(now)
  const { durationMs, label } = RANGE_PRESETS[preset]
  return {
    fallback: false,
    from: new Date(end.getTime() - durationMs),
    kind: 'relative',
    label,
    preset,
    to: end,
    valid: true,
  }
}

function invalidRange(now: Date, parameter: string, reason: string): InvalidTimeRange {
  const fallback = relativeRange(DEFAULT_RANGE, now)
  return { ...fallback, fallback: true, parameter, reason, valid: false }
}

function parseAbsolute(value: string): Date | null {
  if (!RFC3339.test(value)) return null
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date : null
}

function absoluteRange(from: Date, to: Date): ValidAbsoluteTimeRange {
  return {
    fallback: false,
    from: cloneDate(from),
    kind: 'absolute',
    label: 'Custom range',
    to: cloneDate(to),
    valid: true,
  }
}

export function parseRange(params: RangeParams, now: Date): TimeRangeResult {
  const fromValue = paramValue(params, 'from')
  const toValue = paramValue(params, 'to')
  const rangeValue = paramValue(params, 'range')

  if (fromValue !== null || toValue !== null) {
    if (rangeValue !== null) {
      return invalidRange(now, 'range', 'range cannot be combined with from and to')
    }
    if (fromValue === null)
      return invalidRange(now, 'from', 'from is required for an absolute range')
    if (toValue === null) return invalidRange(now, 'to', 'to is required for an absolute range')

    const from = parseAbsolute(fromValue)
    if (!from) return invalidRange(now, 'from', 'from must be an RFC 3339 timestamp')
    const to = parseAbsolute(toValue)
    if (!to) return invalidRange(now, 'to', 'to must be an RFC 3339 timestamp')
    if (from.getTime() > to.getTime()) {
      return invalidRange(now, 'from/to', 'from must not be later than to')
    }
    if (to.getTime() - from.getTime() > MAX_RANGE_MS) {
      return invalidRange(now, 'from/to', 'the range cannot exceed 30 days')
    }
    return absoluteRange(from, to)
  }

  if (rangeValue !== null) {
    if (!Object.hasOwn(RANGE_PRESETS, rangeValue)) {
      return invalidRange(now, 'range', `unknown preset ${rangeValue}`)
    }
    return relativeRange(rangeValue as RangePreset, now)
  }

  return relativeRange(DEFAULT_RANGE, now)
}

export function toParams(range: TimeRangeResult | TimeRange): URLSearchParams {
  if (range.kind === 'relative') return new URLSearchParams({ range: range.preset })
  return new URLSearchParams({ from: range.from.toISOString(), to: range.to.toISOString() })
}

export function chooseStep(from: Date, to: Date, maxPoints = 1_000): string {
  const span = Math.max(0, to.getTime() - from.getTime())
  const pointLimit = Number.isFinite(maxPoints) && maxPoints > 0 ? maxPoints : 1
  const lastStep = STEP_LADDER[STEP_LADDER.length - 1] ?? { milliseconds: 86_400_000, value: '1d' }
  return (STEP_LADDER.find(({ milliseconds }) => span / milliseconds <= pointLimit) ?? lastStep)
    .value
}

export function stepMilliseconds(step: string): number {
  return STEP_LADDER.find((candidate) => candidate.value === step)?.milliseconds ?? 0
}
