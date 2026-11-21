import { useSearchParams } from 'react-router-dom'

import { Badge } from '@/components/ui/badge'
import { NativeSelect } from '@/components/ui/native-select'
import { useT } from '@/i18n/react'
import type { ComparePeriods } from '@/hooks/useComparePeriods'
import {
  COMPARE_PRESETS,
  type CompareStatus,
  formatRatio,
  isWorse,
  parseInstant,
  presetPeriods,
  readComparePreset,
} from '@/lib/compare'
import { formatTimestamp } from '@/lib/format'
import { displayTimeZone, fromZonedInput, toZonedInput } from '@/lib/timezone'

export type CompareArea = 'queries' | 'ash'

export function ComparePeriodPicker({
  area,
  periods,
}: {
  area: CompareArea
  periods: ComparePeriods
}) {
  const t = useT()
  const [searchParams, setSearchParams] = useSearchParams()
  const zone = displayTimeZone()

  function update(changes: Record<string, string | null>) {
    const next = new URLSearchParams(searchParams)
    for (const [key, value] of Object.entries(changes)) {
      if (value === null) next.delete(key)
      else next.set(key, value)
    }
    setSearchParams(next)
  }

  function choosePreset(value: string) {
    const preset = readComparePreset(value)
    if (preset !== 'custom') {
      update({ compare: preset === 'previous' ? null : preset, a_from: null, a_to: null })
      return
    }
    // Custom starts from the period A shown so far.
    const shown = periods.a ?? presetPeriods('previous', periods.b.from, periods.b.to)
    update({
      compare: 'custom',
      a_from: shown?.from.toISOString() ?? null,
      a_to: shown?.to.toISOString() ?? null,
    })
  }

  function chooseEnd(key: 'a_from' | 'a_to', value: string) {
    update({ [key]: fromZonedInput(value, zone)?.toISOString() ?? null })
  }

  const custom = {
    from: parseInstant(searchParams.get('a_from')),
    to: parseInstant(searchParams.get('a_to')),
  }
  const stamp = (d: Date) => formatTimestamp(d.toISOString())

  return (
    <div className="flex flex-wrap items-center gap-2 text-sm" data-testid="compare-periods">
      <label className="text-muted-foreground font-medium" htmlFor={`${area}-compare-preset`}>
        {t(`${area}.compare.presetLabel` as const)}
      </label>
      <NativeSelect
        id={`${area}-compare-preset`}
        onChange={(event) => choosePreset(event.target.value)}
        value={periods.preset}
      >
        {COMPARE_PRESETS.map((preset) => (
          <option key={preset} value={preset}>
            {t(`${area}.compare.preset.${preset}` as const)}
          </option>
        ))}
      </NativeSelect>
      {periods.preset === 'custom' ? (
        <>
          <label className="inline-flex items-center gap-1">
            <span className="text-muted-foreground">
              {t(`${area}.compare.customFrom` as const)}
            </span>
            <input
              className="border-input bg-background rounded-md border px-2 py-1"
              onChange={(event) => chooseEnd('a_from', event.target.value)}
              type="datetime-local"
              value={custom.from ? toZonedInput(custom.from.getTime(), zone) : ''}
            />
          </label>
          <label className="inline-flex items-center gap-1">
            <span className="text-muted-foreground">{t(`${area}.compare.customTo` as const)}</span>
            <input
              className="border-input bg-background rounded-md border px-2 py-1"
              onChange={(event) => chooseEnd('a_to', event.target.value)}
              type="datetime-local"
              value={custom.to ? toZonedInput(custom.to.getTime(), zone) : ''}
            />
          </label>
        </>
      ) : null}
      {periods.a ? (
        <span className="text-muted-foreground tabular-nums" data-testid="compare-period-text">
          {t(`${area}.compare.periods` as const, {
            aFrom: stamp(periods.a.from),
            aTo: stamp(periods.a.to),
            bFrom: stamp(periods.b.from),
            bTo: stamp(periods.b.to),
          })}
        </span>
      ) : (
        <span className="text-warning-foreground" role="status">
          {t(`${area}.compare.chooseCustom` as const)}
        </span>
      )}
    </div>
  )
}

/** The B / A cell: a badge for a row seen in one period, the warning style from ×2.0. */
export function CompareRatio({
  area,
  ratio,
  status,
}: {
  area: CompareArea
  ratio: number | null | undefined
  status: CompareStatus
}) {
  const t = useT()
  const text = formatRatio(ratio, status)
  if (text === 'new' || text === 'gone') {
    return <Badge variant="outline">{t(`${area}.compare.${text}` as const)}</Badge>
  }
  return isWorse(ratio, status) ? (
    <Badge title={t(`${area}.compare.worse` as const)} variant="warning">
      {text}
    </Badge>
  ) : (
    text
  )
}
