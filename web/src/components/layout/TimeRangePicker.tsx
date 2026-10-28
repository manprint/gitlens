import { useState, type FormEvent } from 'react'
import { CalendarClock, Clock, ZoomOut } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Degraded } from '@/components/state/Degraded'
import { Input } from '@/components/ui/input'
import {
  MAX_RANGE_MS,
  RANGE_PRESET_ORDER,
  RANGE_PRESETS,
  chooseStep,
  stepMilliseconds,
  type RangePreset,
} from '@/lib/timerange'
import { useTimeRange } from '@/hooks/useTimeRange'
import { NativeSelect } from '@/components/ui/native-select'

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
  const [formError, setFormError] = useState<string | null>(null)

  // The URL parser rejects these ranges and falls back to the default, so they
  // are refused here, where the operator can still correct them.
  function applyAbsolute(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const from = parseDateTimeLocal(formString(form, 'from'))
    const to = parseDateTimeLocal(formString(form, 'to'))
    if (!from || !to) {
      setFormError('Enter both a start and an end time.')
      return
    }
    if (from.getTime() > to.getTime()) {
      setFormError('The start must not be later than the end.')
      return
    }
    if (to.getTime() - from.getTime() > MAX_RANGE_MS) {
      setFormError('The range cannot be longer than 30 days.')
      return
    }
    setFormError(null)
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
    // An absolute range widens to the shortest preset longer than it, so the
    // button never narrows the view.
    const duration = range.to.getTime() - range.from.getTime()
    const wider = RANGE_PRESET_ORDER.find((preset) => RANGE_PRESETS[preset].durationMs > duration)
    setPreset(wider ?? RANGE_PRESET_ORDER.at(-1) ?? '7d')
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5" data-testid="time-range-picker">
      <div className="relative">
        <Clock
          aria-hidden="true"
          className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2"
        />
        <label className="sr-only" htmlFor="time-range-preset">
          Time range preset
        </label>
        <NativeSelect
          aria-label="Time range preset"
          className="hover:bg-muted h-8 pr-3 pl-8 font-medium"
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
        </NativeSelect>
      </div>
      <details className="group relative">
        <summary className="border-input bg-card hover:bg-muted flex h-8 list-none items-center gap-1.5 rounded-md border px-2.5 text-sm font-medium shadow-xs transition-colors [&::-webkit-details-marker]:hidden">
          <CalendarClock aria-hidden="true" className="text-muted-foreground size-4" />
          <span className="sr-only xl:not-sr-only">Custom</span>
        </summary>
        <div className="bg-popover text-popover-foreground shadow-raised absolute right-0 z-40 mt-2 w-80 space-y-3 rounded-lg border p-4">
          <p className="text-sm font-semibold">Custom time range (UTC)</p>
          <form aria-label="Absolute time range" className="space-y-2" onSubmit={applyAbsolute}>
            <label
              className="text-muted-foreground block text-xs font-medium"
              htmlFor="time-range-from"
            >
              From
            </label>
            <Input
              aria-label="From"
              defaultValue={dateTimeLocalValue(range.from)}
              aria-describedby={formError ? 'time-range-error' : undefined}
              aria-invalid={formError ? true : undefined}
              id="time-range-from"
              name="from"
              required
              type="datetime-local"
            />
            <label
              className="text-muted-foreground block text-xs font-medium"
              htmlFor="time-range-to"
            >
              To
            </label>
            <Input
              aria-label="To"
              defaultValue={dateTimeLocalValue(range.to)}
              aria-describedby={formError ? 'time-range-error' : undefined}
              aria-invalid={formError ? true : undefined}
              id="time-range-to"
              name="to"
              required
              type="datetime-local"
            />
            {formError ? (
              <p className="text-destructive text-xs" id="time-range-error" role="alert">
                {formError}
              </p>
            ) : null}
            <Button className="w-full" size="sm" type="submit">
              Apply range
            </Button>
          </form>
          <p className="text-muted-foreground text-xs" data-testid="time-range-step">
            Step {step} ({stepMilliseconds(step) / 1_000}s)
          </p>
        </div>
      </details>
      <Button
        aria-label="Zoom out time range"
        onClick={zoomOut}
        size="icon-sm"
        title="Zoom out"
        variant="outline"
      >
        <ZoomOut aria-hidden="true" />
      </Button>
      {!range.valid ? (
        <Degraded
          reason={`Invalid time range parameter "${range.parameter}": ${range.reason}. Using the default 1h range.`}
        />
      ) : null}
    </div>
  )
}
