import type { FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { Degraded } from '@/components/state/Degraded'
import { Input } from '@/components/ui/input'
import {
  RANGE_PRESET_ORDER,
  RANGE_PRESETS,
  chooseStep,
  stepMilliseconds,
  type RangePreset,
} from '@/lib/timerange'
import { useTimeRange } from '@/hooks/useTimeRange'

function dateTimeLocalValue(date: Date): string {
  return date.toISOString().slice(0, 16)
}

function parseDateTimeLocal(value: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return null
  const date = new Date(`${value}:00Z`)
  return Number.isFinite(date.getTime()) ? date : null
}

function formString(form: FormData, name: string): string {
  const value = form.get(name)
  return typeof value === 'string' ? value : ''
}

export function TimeRangePicker() {
  const { range, setPreset, setRange } = useTimeRange()
  const step = chooseStep(range.from, range.to)

  function applyAbsolute(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const from = parseDateTimeLocal(formString(form, 'from'))
    const to = parseDateTimeLocal(formString(form, 'to'))
    if (!from || !to) return
    setRange({ from, kind: 'absolute', label: 'Custom range', to })
  }

  function zoomOut() {
    if (range.kind === 'relative') {
      const currentIndex = RANGE_PRESET_ORDER.indexOf(range.preset)
      const nextPreset: RangePreset =
        RANGE_PRESET_ORDER[Math.min(currentIndex + 1, RANGE_PRESET_ORDER.length - 1)] ??
        range.preset
      setPreset(nextPreset)
      return
    }
    setPreset('24h')
  }

  return (
    <div className="flex flex-wrap items-center gap-2" data-testid="time-range-picker">
      <label className="sr-only" htmlFor="time-range-preset">
        Time range preset
      </label>
      <select
        aria-label="Time range preset"
        className="border-input bg-background h-9 rounded-md border px-3 text-sm"
        id="time-range-preset"
        onChange={(event) => setPreset(event.target.value as RangePreset)}
        value={range.kind === 'relative' ? range.preset : ''}
      >
        <option disabled value="">
          Custom range
        </option>
        {RANGE_PRESET_ORDER.map((preset) => (
          <option key={preset} value={preset}>
            {RANGE_PRESETS[preset].label}
          </option>
        ))}
      </select>
      <form
        aria-label="Absolute time range"
        className="flex items-center gap-1"
        onSubmit={applyAbsolute}
      >
        <label className="sr-only" htmlFor="time-range-from">
          From
        </label>
        <Input
          aria-label="From"
          defaultValue={dateTimeLocalValue(range.from)}
          id="time-range-from"
          name="from"
          required
          type="datetime-local"
        />
        <span aria-hidden="true">–</span>
        <label className="sr-only" htmlFor="time-range-to">
          To
        </label>
        <Input
          aria-label="To"
          defaultValue={dateTimeLocalValue(range.to)}
          id="time-range-to"
          name="to"
          required
          type="datetime-local"
        />
        <Button size="sm" type="submit" variant="outline">
          Apply range
        </Button>
      </form>
      <Button aria-label="Zoom out time range" onClick={zoomOut} size="sm" variant="ghost">
        Zoom out
      </Button>
      <span className="text-text-secondary text-xs" data-testid="time-range-step">
        Step {step} ({stepMilliseconds(step) / 1_000}s)
      </span>
      {!range.valid ? (
        <Degraded
          reason={`Invalid time range parameter "${range.parameter}": ${range.reason}. Using the default 1h range.`}
        />
      ) : null}
    </div>
  )
}
